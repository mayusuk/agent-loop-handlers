// Package run is the life of one stage run: it reads the handler
// environment (INTEGRATION-SPEC §3.1), checks the input and the contract
// version, and guarantees that the run ends with a result.json that has
// passed contract.ValidateResult, or a failed one (FR-18 AC3, plan D2).
package run

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/mayusuk/agent-loop/contract"
)

// The handler environment (INTEGRATION-SPEC §3.1).
const (
	EnvInput    = "AGENT_LOOP_INPUT"
	EnvResult   = "AGENT_LOOP_RESULT"
	EnvOut      = "AGENT_LOOP_OUT"
	EnvContract = "AGENT_LOOP_CONTRACT"
)

// Exit codes. The exit code is informational (§3.1): the result file
// decides the outcome. It is non-zero only when no result was written
// (plan D2).
const (
	ExitResultWritten = 0
	ExitNoResult      = 1
)

// UnknownRunID is the run_id of a failed result written when the input
// gives none, because the result schema requires a non-empty run_id.
const UnknownRunID = "unknown"

// Contracts lists the contract versions this release supports (plan D7).
var Contracts = []string{contract.Version}

// Run is one stage run.
type Run struct {
	// Stage is the subcommand's stage.
	Stage contract.Stage
	// Input is the parsed input. Before the input passes its checks it
	// holds only Stage and the best-known RunID.
	Input contract.Input
	// Out is AGENT_LOOP_OUT, the directory result files are read from.
	Out string
	// ResultPath is AGENT_LOOP_RESULT.
	ResultPath string
	// Args are the subcommand's arguments.
	Args []string

	stderr   io.Writer
	finished bool
	written  bool
	code     int
}

// Body runs a stage once the input has passed its checks, and returns its
// result. Main passes the result to Finish.
type Body func(r *Run) contract.Result

// Main runs one stage and returns the process exit code. It never returns
// without trying to write a result: a failed check, a result that
// ValidateResult rejects and a panic in body each end in a failed result.
func Main(stage contract.Stage, args []string, getenv func(string) string, stderr io.Writer, body Body) (code int) {
	r := &Run{
		Stage:      stage,
		Input:      contract.Input{Stage: stage, RunID: UnknownRunID},
		ResultPath: getenv(EnvResult),
		Args:       args,
		stderr:     &lockedWriter{w: stderr},
	}
	if r.ResultPath == "" {
		r.logf("%s is not set; no result can be written", EnvResult)
		return ExitNoResult
	}
	defer func() {
		if p := recover(); p != nil {
			r.safeLogf("panic: %v\n%s", p, debug.Stack())
			if r.finished {
				code = r.code
				return
			}
			code = r.Finish(Failed(fmt.Sprintf("internal error: %v", p)))
		}
	}()
	if res, ok := r.load(getenv); !ok {
		return r.Finish(res)
	}
	return r.Finish(body(r))
}

// load reads and checks the environment and the input. It returns false
// and a failed result when a check fails.
func (r *Run) load(getenv func(string) string) (contract.Result, bool) {
	inPath := getenv(EnvInput)
	if inPath == "" {
		return Failed(EnvInput + " is not set"), false
	}
	data, err := os.ReadFile(inPath)
	if err != nil {
		return Failed("cannot read the input: " + err.Error()), false
	}
	if id := lenientRunID(data); id != "" {
		r.Input.RunID = id
	}

	if res, ok := checkContract(getenv(EnvContract)); !ok {
		return res, false
	}

	in, err := contract.ParseInput(data)
	if err != nil {
		return Failed("invalid input: " + err.Error()), false
	}
	if in.RunID == "" {
		return Failed("invalid input: run_id is empty"), false
	}
	r.Input.RunID = in.RunID
	if in.Stage != r.Stage {
		return Failed(fmt.Sprintf("the input is for stage %q but this is the %s handler", in.Stage, r.Stage)), false
	}

	out := getenv(EnvOut)
	if out == "" {
		return Failed(EnvOut + " is not set"), false
	}
	if fi, err := os.Stat(out); err != nil {
		return Failed(EnvOut + " is not usable: " + err.Error()), false
	} else if !fi.IsDir() {
		return Failed(EnvOut + " is not a directory: " + out), false
	}

	r.Input = in
	r.Out = out
	return contract.Result{}, true
}

// checkContract checks AGENT_LOOP_CONTRACT: any 1.x is supported, because
// a 1.0 handler ignores the fields a later minor adds (INTEGRATION-SPEC §2).
func checkContract(v string) (contract.Result, bool) {
	if v == "" {
		return Failed(EnvContract + " is not set"), false
	}
	major, _, err := contract.ParseVersion(v)
	if err != nil {
		return Failed(EnvContract + ": " + err.Error()), false
	}
	if major != contract.Major {
		return Failed(fmt.Sprintf("contract %s is not supported: this release supports %s",
			v, strings.Join(Contracts, ", "))), false
	}
	return contract.Result{}, true
}

// lenientRunID returns the input's run_id if the input is a JSON object
// with a string run_id, so that a failed result for an input that fails
// its checks still names the run.
func lenientRunID(data []byte) string {
	var v struct {
		RunID string `json:"run_id"`
	}
	if json.Unmarshal(data, &v) != nil {
		return ""
	}
	return strings.TrimSpace(v.RunID)
}

// Finish validates res and writes it to AGENT_LOOP_RESULT, and returns
// the exit code. It sets contract_version and run_id, which the handler
// owns. A result that contract.ValidateResult rejects is replaced by the
// failed result it returns. Finish writes at most once per run; a later
// call is logged and returns the first call's exit code. A panic inside
// Finish keeps a result already written, and otherwise writes a minimal
// failed result (plan D2).
func (r *Run) Finish(res contract.Result) (code int) {
	if r.finished {
		r.logf("internal error: Finish called twice; the first result stands")
		return r.code
	}
	r.finished = true
	r.code = ExitNoResult // until the write succeeds, in case finish panics
	defer func() {
		if p := recover(); p != nil {
			stack := debug.Stack()
			if r.written {
				r.code = ExitResultWritten
			} else {
				r.code = r.writeFallback(p)
			}
			code = r.code
			r.safeLogf("panic in Finish: %v\n%s", p, stack)
		}
	}()
	r.code = r.finish(res)
	return r.code
}

// writeFallback writes a minimal failed result after a panic in Finish. It
// skips ValidateResult, which may be what panicked: the result is
// schema-valid by construction. It never panics.
func (r *Run) writeFallback(p any) (code int) {
	defer func() {
		if q := recover(); q != nil {
			r.safeLogf("cannot write the failed result: panic: %v", q)
			code = ExitNoResult
		}
	}()
	res := Failed(fmt.Sprintf("internal error: cannot finish the result: %v", p))
	res.ContractVersion, res.RunID = contract.Version, r.Input.RunID
	data, err := json.Marshal(res)
	if err == nil {
		err = writeAtomic(r.ResultPath, append(data, '\n'))
	}
	if err != nil {
		r.safeLogf("cannot write the failed result: %v", err)
		return ExitNoResult
	}
	r.written = true
	r.safeLogf("result: failed: %s", res.Failed.Reason)
	return ExitResultWritten
}

func (r *Run) finish(res contract.Result) int {
	res.ContractVersion = contract.Version
	res.RunID = r.Input.RunID
	data, err := json.Marshal(res)
	if err == nil {
		var valid contract.Result
		valid, err = contract.ValidateResult(r.Input, data, r.Out)
		if err != nil {
			r.logf("result rejected: %v", err)
			data, err = json.Marshal(valid)
			res = valid
		}
	}
	if err != nil {
		r.logf("cannot encode the result: %v", err)
		res = Failed("internal error: cannot encode the result")
		res.ContractVersion, res.RunID = contract.Version, r.Input.RunID
		if data, err = json.Marshal(res); err != nil {
			r.logf("cannot encode the failed result: %v", err)
			return ExitNoResult
		}
	}

	if err := writeAtomic(r.ResultPath, append(data, '\n')); err != nil {
		r.logf("cannot write the result: %v", err)
		return ExitNoResult
	}
	r.written = true
	if res.Failed != nil {
		r.logf("result: failed: %s", res.Failed.Reason)
	} else {
		r.logf("result: %s", res.Result)
	}
	return ExitResultWritten
}

// writeAtomic writes data to path through a temporary file in the same
// directory, so the worker never reads a partial result.
func writeAtomic(path string, data []byte) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".result-*.json")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			os.Remove(tmp)
		}
	}()
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Failed returns a failed result. The reason is folded to one line and cut
// to contract.MaxFailedReason characters (INTEGRATION-SPEC §5.1). An empty
// reason becomes "unknown error".
func Failed(reason string) contract.Result {
	reason = strings.Join(strings.Fields(reason), " ")
	if reason == "" {
		reason = "unknown error"
	}
	if utf8.RuneCountInString(reason) > contract.MaxFailedReason {
		reason = string([]rune(reason)[:contract.MaxFailedReason-1]) + "…"
	}
	return contract.Result{Result: contract.OutcomeFailed, Failed: &contract.Failed{Reason: reason}}
}

// lockedWriter serialises writes to the run log, which claude's and git's
// stdout and stderr copiers share.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// Logf writes one line to the run log (stderr).
func (r *Run) Logf(format string, a ...any) { r.logf(format, a...) }

// Log is the run log (stderr), for output such as claude's and git's.
func (r *Run) Log() io.Writer { return r.stderr }

// safeLogf is logf for recover paths: a panic in the run log is dropped.
func (r *Run) safeLogf(format string, a ...any) {
	defer func() { _ = recover() }()
	r.logf(format, a...)
}

func (r *Run) logf(format string, a ...any) {
	fmt.Fprintf(r.stderr, "agent-loop-claude %s: %s\n", r.Stage, fmt.Sprintf(format, a...))
}
