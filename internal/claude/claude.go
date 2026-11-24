// Package claude runs Claude Code headless (claude -p) in a run's
// workspace: stdin closed, in its own process group, stopped before the
// input deadline, with its output streamed to the run log. It reads the
// run's cost and tokens from Claude Code's JSON output (plan T2, D3, D12,
// D13). Claude's final text is only logged, never the result
// (TECHNICAL-SPEC §7.1).
package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mayusuk/agent-loop/contract"
)

// Environment variables the handler reads.
const (
	// EnvBin overrides the claude binary when --claude isn't given (D13).
	EnvBin = "AGENT_LOOP_CLAUDE_BIN"
	// EnvSettings is the sandbox settings file the local runner generates
	// for each run (TECHNICAL-SPEC §7.2, D12).
	EnvSettings = "AGENT_LOOP_CLAUDE_SETTINGS"
)

// DefaultBin is the name looked up on PATH.
const DefaultBin = "claude"

// DefaultMargin is how long before the input deadline claude is stopped,
// so the handler can still write its result (plan D3).
const DefaultMargin = 2 * time.Minute

// DefaultGrace is how long claude has to exit after SIGTERM before its
// process group gets SIGKILL (work plan D2).
const DefaultGrace = 5 * time.Second

// tailSize bounds the stdout kept for reading usage (work plan D5).
const tailSize = 4 << 20

// ErrTimeLimit is returned when claude was stopped at, or not started
// because of, the stop time (FR-18 AC4).
var ErrTimeLimit = errors.New("time limit reached")

// ExitError is a claude run that ended other than with exit code 0.
type ExitError struct {
	// Code is the exit code, or -1 when claude was killed by Signal.
	Code   int
	Signal syscall.Signal
}

func (e *ExitError) Error() string {
	if e.Code < 0 {
		return fmt.Sprintf("claude was killed by signal %s", e.Signal)
	}
	return fmt.Sprintf("claude exited with code %d", e.Code)
}

// Find returns the claude binary: flag (--claude) if set, else
// AGENT_LOOP_CLAUDE_BIN, else claude on PATH (D13).
func Find(flag string, getenv func(string) string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if p := getenv(EnvBin); p != "" {
		return p, nil
	}
	p, err := exec.LookPath(DefaultBin)
	if err != nil {
		return "", fmt.Errorf("claude not found: set --claude or %s, or put claude on PATH: %w", EnvBin, err)
	}
	return p, nil
}

// Args returns claude's arguments for one headless run. settings is the
// sandbox settings file, or "" for none. The prompt is last, after "--",
// so a prompt that starts with "-" isn't read as a flag (work plan D1).
// It never contains --dangerously-skip-permissions (D12).
func Args(prompt, settings string) []string {
	args := []string{"-p", "--output-format", "json", "--permission-mode", "acceptEdits"}
	if settings != "" {
		args = append(args, "--settings", settings)
	}
	return append(args, "--", prompt)
}

// StopAt returns when claude must be stopped: the input deadline (RFC
// 3339) minus margin (D3).
func StopAt(deadline string, margin time.Duration) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, deadline)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid deadline %q: %w", deadline, err)
	}
	return t.Add(-margin), nil
}

// Options is one claude run.
type Options struct {
	// Bin is the claude binary (Find).
	Bin string
	// Args are its arguments (Args).
	Args []string
	// Dir is the workspace, claude's working directory.
	Dir string
	// Env is claude's environment; nil means the handler's own.
	Env []string
	// StopAt is when claude is stopped (StopAt). The zero time means no
	// time limit.
	StopAt time.Time
	// Grace is the time between SIGTERM and SIGKILL; 0 means DefaultGrace.
	Grace time.Duration
	// Stdout and Stderr receive claude's output, the run log. nil
	// discards it.
	Stdout, Stderr io.Writer
}

// Outcome is what a run reports, whether it succeeded or not.
type Outcome struct {
	// Usage is the run's cost and tokens, or nil when the output gives
	// none (work plan D4, D5).
	Usage *contract.Usage
}

// Run runs claude and waits for it and its process group. It returns
// ErrTimeLimit when claude was stopped at opts.StopAt, an *ExitError when
// it ended otherwise than with code 0, and ctx's error when ctx ended
// first. Outcome is set in every case, so usage counts for failed runs.
func Run(ctx context.Context, opts Options) (Outcome, error) {
	parent := ctx
	if !opts.StopAt.IsZero() {
		if !time.Now().Before(opts.StopAt) {
			return Outcome{}, ErrTimeLimit // work plan D3
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, opts.StopAt)
		defer cancel()
	}
	grace := opts.Grace
	if grace <= 0 {
		grace = DefaultGrace
	}

	tail := &tailBuffer{max: tailSize}
	cmd := exec.CommandContext(ctx, opts.Bin, opts.Args...)
	cmd.Dir = opts.Dir
	cmd.Env = opts.Env
	cmd.Stdin = nil // the null device: stdin is closed
	cmd.Stdout = io.MultiWriter(orDiscard(opts.Stdout), tail)
	cmd.Stderr = orDiscard(opts.Stderr)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// stopped records that claude itself was still running when the
	// context ended. Only then is the run a time limit (or the caller's
	// cancellation): a claude that already exited, whose output is still
	// draining, is reported by its own exit status (work plan #19 D2).
	var stopped atomic.Bool
	cmd.Cancel = func() error {
		done := errors.Is(cmd.Process.Signal(syscall.Signal(0)), os.ErrProcessDone)
		if !done {
			stopped.Store(true)
		}
		if err := signalGroup(cmd, syscall.SIGTERM); err != nil {
			return err
		}
		if done {
			return os.ErrProcessDone // exec then injects no error
		}
		return nil
	}
	cmd.WaitDelay = grace

	if err := cmd.Start(); err != nil {
		return Outcome{}, fmt.Errorf("cannot start claude: %w", err)
	}
	werr := cmd.Wait()
	// Whatever ended the run, nothing claude started outlives it (D2).
	_ = signalGroup(cmd, syscall.SIGKILL)

	out := Outcome{Usage: ParseUsage(tail.Bytes())}
	if stopped.Load() {
		// Stopped by the context: by the stop time unless the caller's
		// context ended first.
		if perr := parent.Err(); perr != nil {
			return out, perr
		}
		return out, ErrTimeLimit
	}
	var ee *exec.ExitError
	if errors.As(werr, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return out, &ExitError{Code: -1, Signal: ws.Signal()}
		}
		return out, &ExitError{Code: ee.ExitCode()}
	}
	if werr != nil && !errors.Is(werr, exec.ErrWaitDelay) {
		return out, fmt.Errorf("claude: %w", werr)
	}
	// exec.ErrWaitDelay: claude exited 0 but a child held its output open
	// past the grace period; the pipes were closed (work plan D6).
	return out, nil
}

// signalGroup sends sig to claude's process group. A group that is
// already gone is not an error.
func signalGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func orDiscard(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}

// ParseUsage reads usage from claude --output-format json output (work
// plan D4, D5). It tries the whole output as one JSON object, then each
// line from the last. It never fails: output it can't read gives nil.
func ParseUsage(out []byte) *contract.Usage {
	if u, ok := usageFrom(bytes.TrimSpace(out)); ok {
		return u
	}
	lines := bytes.Split(out, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if u, ok := usageFrom(bytes.TrimSpace(lines[i])); ok {
			return u
		}
	}
	return nil
}

// usageFrom returns ok when data is one JSON object, and the usage it
// holds, which may be nil.
func usageFrom(data []byte) (*contract.Usage, bool) {
	if len(data) == 0 || data[0] != '{' {
		return nil, false
	}
	var v map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if dec.Decode(&v) != nil || dec.More() {
		return nil, false
	}
	var u contract.Usage
	found := false
	if c, ok := number(v["total_cost_usd"]); ok {
		usd := "USD"
		u.Cost, u.Currency = &c, &usd
		found = true
	}
	if m, ok := v["usage"].(map[string]any); ok {
		var in int64
		seen := false
		for _, k := range []string{"input_tokens", "cache_creation_input_tokens", "cache_read_input_tokens"} {
			if n, ok := integer(m[k]); ok {
				// Saturate rather than wrap negative (work plan #19 D1).
				if in > math.MaxInt64-n {
					in = math.MaxInt64
				} else {
					in += n
				}
				seen = true
			}
		}
		if seen {
			u.InputTokens = &in
			found = true
		}
		if n, ok := integer(m["output_tokens"]); ok {
			u.OutputTokens = &n
			found = true
		}
	}
	if !found {
		return nil, true
	}
	return &u, true
}

// number returns a finite, non-negative JSON number.
func number(v any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, err := n.Float64()
	if err != nil || f < 0 || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, false
	}
	return f, true
}

// integer returns a non-negative JSON integer.
func integer(v any) (int64, bool) {
	n, ok := v.(json.Number)
	if !ok || strings.ContainsAny(n.String(), ".eE") {
		return 0, false
	}
	i, err := n.Int64()
	if err != nil || i < 0 {
		return 0, false
	}
	return i, true
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) >= t.max {
		t.buf = append(t.buf[:0], p[len(p)-t.max:]...)
		return n, nil
	}
	if over := len(t.buf) + len(p) - t.max; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	t.buf = append(t.buf, p...)
	return n, nil
}

func (t *tailBuffer) Bytes() []byte { return t.buf }
