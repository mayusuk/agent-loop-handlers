package run

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mayusuk/agent-loop/contract"
)

const testRunID = "item/p/42#1:implement:1:a"

func inputJSON(stage contract.Stage, version string) string {
	return `{
  "contract_version": "` + version + `",
  "run_id": "` + testRunID + `",
  "stage": "` + string(stage) + `",
  "attempt": 1,
  "deadline": "2026-11-14T16:00:00Z",
  "identity": "coder",
  "project": { "name": "p", "tracker": "github", "code_host": "github" },
  "repo": { "url": "https://github.com/o/r", "base_branch": "main" },
  "workspace": { "path": "/ws", "branch": "agent/42", "head": "9f2c" },
  "item": { "ref": "42", "url": "https://github.com/o/r/issues/42", "type": "feature" },
  "change": null,
  "a_later_minor_field": true
}`
}

// fixture is one run's environment in a temporary directory.
type fixture struct {
	t      *testing.T
	env    map[string]string
	stderr bytes.Buffer
}

func newFixture(t *testing.T, input string) *fixture {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(dir, "input.json")
	if err := os.WriteFile(in, []byte(input), 0o444); err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, env: map[string]string{
		EnvInput:    in,
		EnvResult:   filepath.Join(dir, "result.json"),
		EnvOut:      out,
		EnvContract: "1.0",
	}}
}

func (f *fixture) getenv(k string) string { return f.env[k] }

func (f *fixture) main(stage contract.Stage, body Body) int {
	return Main(stage, nil, f.getenv, &f.stderr, body)
}

// result reads the written result and checks it against the contract
// schema, for the given stage.
func (f *fixture) result(stage contract.Stage) contract.Result {
	f.t.Helper()
	data, err := os.ReadFile(f.env[EnvResult])
	if err != nil {
		f.t.Fatalf("no result written: %v\nstderr:\n%s", err, f.stderr.String())
	}
	r, err := contract.ParseResult(stage, data)
	if err != nil {
		f.t.Fatalf("result does not parse: %v\n%s", err, data)
	}
	if r.ContractVersion != contract.Version {
		f.t.Errorf("contract_version = %q, want %q", r.ContractVersion, contract.Version)
	}
	if r.RunID == "" {
		f.t.Errorf("run_id is empty")
	}
	return r
}

// failedReason asserts the written result is failed with a valid one-line
// reason, and returns the reason.
func (f *fixture) failedReason(stage contract.Stage) string {
	f.t.Helper()
	r := f.result(stage)
	if r.Result != contract.OutcomeFailed || r.Failed == nil {
		f.t.Fatalf("result = %q, want failed", r.Result)
	}
	in := contract.Input{Stage: stage, RunID: r.RunID}
	data, _ := json.Marshal(r)
	if _, err := contract.ValidateResult(in, data, ""); err != nil {
		f.t.Errorf("written failed result is invalid: %v", err)
	}
	return r.Failed.Reason
}

func ok(r *Run) contract.Result { return Failed("stage ran") }

func mustNotRun(t *testing.T) Body {
	return func(*Run) contract.Result {
		t.Error("body ran although a check failed")
		return Failed("body ran")
	}
}

func TestFR18AC3_ValidInputRunsBody(t *testing.T) {
	f := newFixture(t, inputJSON(contract.StageImplement, "1.0"))
	var got *Run
	code := f.main(contract.StageImplement, func(r *Run) contract.Result {
		got = r
		return Failed("stage ran")
	})
	if code != ExitResultWritten {
		t.Fatalf("exit = %d, want %d", code, ExitResultWritten)
	}
	if got == nil {
		t.Fatal("body did not run")
	}
	if got.Input.RunID != testRunID || got.Input.Item.Ref != "42" || got.Out != f.env[EnvOut] {
		t.Errorf("run = %+v", got)
	}
	if reason := f.failedReason(contract.StageImplement); reason != "stage ran" {
		t.Errorf("reason = %q", reason)
	}
	if r := f.result(contract.StageImplement); r.RunID != testRunID {
		t.Errorf("run_id = %q, want %q", r.RunID, testRunID)
	}
}

func TestFR18AC3_MissingResultPathExitsNonZero(t *testing.T) {
	f := newFixture(t, inputJSON(contract.StageImplement, "1.0"))
	delete(f.env, EnvResult)
	if code := f.main(contract.StageImplement, mustNotRun(t)); code != ExitNoResult {
		t.Fatalf("exit = %d, want %d", code, ExitNoResult)
	}
	if !strings.Contains(f.stderr.String(), EnvResult+" is not set") {
		t.Errorf("stderr = %q", f.stderr.String())
	}
}

func TestFR18AC3_UnwritableResultExitsNonZero(t *testing.T) {
	f := newFixture(t, inputJSON(contract.StageImplement, "1.0"))
	f.env[EnvResult] = filepath.Join(t.TempDir(), "missing-dir", "result.json")
	if code := f.main(contract.StageImplement, ok); code != ExitNoResult {
		t.Fatalf("exit = %d, want %d", code, ExitNoResult)
	}
	if !strings.Contains(f.stderr.String(), "cannot write the result") {
		t.Errorf("stderr = %q", f.stderr.String())
	}
}

func TestFR18AC3_MissingEnvironmentIsFailed(t *testing.T) {
	for _, k := range []string{EnvInput, EnvOut, EnvContract} {
		t.Run(k, func(t *testing.T) {
			f := newFixture(t, inputJSON(contract.StageImplement, "1.0"))
			delete(f.env, k)
			if code := f.main(contract.StageImplement, mustNotRun(t)); code != ExitResultWritten {
				t.Fatalf("exit = %d", code)
			}
			if reason := f.failedReason(contract.StageImplement); reason != k+" is not set" {
				t.Errorf("reason = %q", reason)
			}
			want := testRunID
			if k == EnvInput {
				want = UnknownRunID
			}
			if r := f.result(contract.StageImplement); r.RunID != want {
				t.Errorf("run_id = %q, want %q", r.RunID, want)
			}
		})
	}
}

func TestFR18AC3_UnreadableInputIsFailed(t *testing.T) {
	cases := map[string]func(f *fixture){
		"missing file": func(f *fixture) { f.env[EnvInput] = filepath.Join(t.TempDir(), "nope.json") },
		"directory":    func(f *fixture) { f.env[EnvInput] = t.TempDir() },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, inputJSON(contract.StageImplement, "1.0"))
			mutate(f)
			f.main(contract.StageImplement, mustNotRun(t))
			if reason := f.failedReason(contract.StageImplement); !strings.HasPrefix(reason, "cannot read the input: ") {
				t.Errorf("reason = %q", reason)
			}
		})
	}
}

func TestFR18AC3_InvalidInputIsFailed(t *testing.T) {
	cases := map[string]struct {
		input, prefix, runID string
	}{
		"not json":       {"{not json", "invalid input: ", UnknownRunID},
		"empty":          {"", "invalid input: ", UnknownRunID},
		"input major 2":  {inputJSON(contract.StageImplement, "2.0"), "invalid input: ", testRunID},
		"bad version":    {inputJSON(contract.StageImplement, "one"), "invalid input: ", testRunID},
		"no run_id":      {`{"contract_version":"1.0","stage":"implement"}`, "invalid input: run_id is empty", UnknownRunID},
		"wrong run type": {`{"contract_version":"1.0","run_id":7,"stage":"implement"}`, "invalid input: ", UnknownRunID},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, c.input)
			f.main(contract.StageImplement, mustNotRun(t))
			if reason := f.failedReason(contract.StageImplement); !strings.HasPrefix(reason, c.prefix) {
				t.Errorf("reason = %q, want prefix %q", reason, c.prefix)
			}
			if r := f.result(contract.StageImplement); r.RunID != c.runID {
				t.Errorf("run_id = %q, want %q", r.RunID, c.runID)
			}
		})
	}
}

func TestD7_ContractMajorOtherThanOneIsFailed(t *testing.T) {
	for _, v := range []string{"2.0", "0.9", "1", "x.y"} {
		t.Run(v, func(t *testing.T) {
			f := newFixture(t, inputJSON(contract.StageImplement, "1.0"))
			f.env[EnvContract] = v
			f.main(contract.StageImplement, mustNotRun(t))
			reason := f.failedReason(contract.StageImplement)
			if !strings.Contains(reason, v) {
				t.Errorf("reason %q does not name %q", reason, v)
			}
			if r := f.result(contract.StageImplement); r.RunID != testRunID {
				t.Errorf("run_id = %q, want %q", r.RunID, testRunID)
			}
		})
	}
}

func TestD7_LaterMinorIsAccepted(t *testing.T) {
	f := newFixture(t, inputJSON(contract.StageImplement, "1.3"))
	f.env[EnvContract] = "1.3"
	ran := false
	f.main(contract.StageImplement, func(*Run) contract.Result { ran = true; return Failed("x") })
	if !ran {
		t.Fatalf("body did not run; stderr:\n%s", f.stderr.String())
	}
	if r := f.result(contract.StageImplement); r.ContractVersion != "1.0" {
		t.Errorf("contract_version = %q, want 1.0", r.ContractVersion)
	}
}

func TestFR18AC3_StageMismatchIsFailed(t *testing.T) {
	f := newFixture(t, inputJSON(contract.StageReview, "1.0"))
	f.main(contract.StageImplement, mustNotRun(t))
	reason := f.failedReason(contract.StageImplement)
	if reason != `the input is for stage "review" but this is the implement handler` {
		t.Errorf("reason = %q", reason)
	}
}

func TestFR18AC3_OutNotADirectoryIsFailed(t *testing.T) {
	f := newFixture(t, inputJSON(contract.StageImplement, "1.0"))
	f.env[EnvOut] = f.env[EnvInput]
	f.main(contract.StageImplement, mustNotRun(t))
	if reason := f.failedReason(contract.StageImplement); !strings.HasPrefix(reason, EnvOut+" is not a directory") {
		t.Errorf("reason = %q", reason)
	}
	f = newFixture(t, inputJSON(contract.StageImplement, "1.0"))
	f.env[EnvOut] = filepath.Join(t.TempDir(), "missing")
	f.main(contract.StageImplement, mustNotRun(t))
	if reason := f.failedReason(contract.StageImplement); !strings.HasPrefix(reason, EnvOut+" is not usable") {
		t.Errorf("reason = %q", reason)
	}
}

func TestFR18AC3_RejectedResultIsReplacedByFailed(t *testing.T) {
	f := newFixture(t, inputJSON(contract.StageImplement, "1.0"))
	f.main(contract.StageImplement, func(*Run) contract.Result {
		// body_file does not exist in AGENT_LOOP_OUT.
		return contract.Result{Result: contract.OutcomeDone, Done: &contract.Done{Implement: &contract.ImplementDone{
			Head:   strings.Repeat("a", 40),
			Change: &contract.NewChange{Title: "t", BodyFile: "body.md"},
		}}}
	})
	reason := f.failedReason(contract.StageImplement)
	if !strings.Contains(reason, "invalid result") || !strings.Contains(reason, "body") {
		t.Errorf("reason = %q", reason)
	}
	if !strings.Contains(f.stderr.String(), "result rejected") {
		t.Errorf("stderr = %q", f.stderr.String())
	}
}

func TestFR18AC1_OutcomeNotAllowedIsReplacedByFailed(t *testing.T) {
	f := newFixture(t, inputJSON(contract.StageReview, "1.0"))
	f.main(contract.StageReview, func(*Run) contract.Result {
		return contract.Result{Result: contract.OutcomeComplete, Complete: &contract.Complete{}}
	})
	if reason := f.failedReason(contract.StageReview); !strings.Contains(reason, "not allowed") {
		t.Errorf("reason = %q", reason)
	}
}

func TestFinishWritesValidResultAndOwnsRunIDAndVersion(t *testing.T) {
	f := newFixture(t, inputJSON(contract.StageImplement, "1.0"))
	if err := os.WriteFile(filepath.Join(f.env[EnvOut], "reason.md"), []byte("needs #7"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.main(contract.StageImplement, func(*Run) contract.Result {
		return contract.Result{
			ContractVersion: "9.9", RunID: "someone-else",
			Result: contract.OutcomeBlocked, Blocked: &contract.Blocked{ReasonFile: "reason.md"},
		}
	})
	r := f.result(contract.StageImplement)
	if r.Result != contract.OutcomeBlocked || r.Blocked.ReasonFile != "reason.md" {
		t.Fatalf("result = %+v", r)
	}
	if r.RunID != testRunID {
		t.Errorf("run_id = %q, want %q", r.RunID, testRunID)
	}
	entries, _ := os.ReadDir(filepath.Dir(f.env[EnvResult]))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".result-") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

func TestFinishReplacesAnExistingResult(t *testing.T) {
	f := newFixture(t, inputJSON(contract.StageImplement, "1.0"))
	if err := os.WriteFile(f.env[EnvResult], []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.main(contract.StageImplement, ok)
	if reason := f.failedReason(contract.StageImplement); reason != "stage ran" {
		t.Errorf("reason = %q", reason)
	}
}

func TestD2_PanicInStageWritesFailed(t *testing.T) {
	f := newFixture(t, inputJSON(contract.StageImplement, "1.0"))
	code := f.main(contract.StageImplement, func(*Run) contract.Result {
		panic("boom\nsecond line")
	})
	if code != ExitResultWritten {
		t.Fatalf("exit = %d", code)
	}
	if reason := f.failedReason(contract.StageImplement); reason != "internal error: boom second line" {
		t.Errorf("reason = %q", reason)
	}
	if !strings.Contains(f.stderr.String(), "panic: boom") || !strings.Contains(f.stderr.String(), "goroutine") {
		t.Errorf("stderr has no panic and stack: %q", f.stderr.String())
	}
}

func TestD2_PanicAfterFinishKeepsFirstResult(t *testing.T) {
	f := newFixture(t, inputJSON(contract.StageImplement, "1.0"))
	code := f.main(contract.StageImplement, func(r *Run) contract.Result {
		r.Finish(Failed("first"))
		panic("late")
	})
	if code != ExitResultWritten {
		t.Fatalf("exit = %d", code)
	}
	if reason := f.failedReason(contract.StageImplement); reason != "first" {
		t.Errorf("reason = %q", reason)
	}
}

func TestFinishTwiceKeepsFirstResult(t *testing.T) {
	f := newFixture(t, inputJSON(contract.StageImplement, "1.0"))
	code := f.main(contract.StageImplement, func(r *Run) contract.Result {
		r.Finish(Failed("first"))
		return Failed("second")
	})
	if code != ExitResultWritten {
		t.Fatalf("exit = %d", code)
	}
	if reason := f.failedReason(contract.StageImplement); reason != "first" {
		t.Errorf("reason = %q", reason)
	}
	if !strings.Contains(f.stderr.String(), "Finish called twice") {
		t.Errorf("stderr = %q", f.stderr.String())
	}
}

func TestFailedReasonIsOneValidLine(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"multi-line": {"a\n  b\r\nc\t d", "a b c d"},
		"empty":      {" \n ", "unknown error"},
		"plain":      {"x", "x"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := Failed(c.in).Failed.Reason; got != c.want {
				t.Errorf("Failed(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
	long := Failed(strings.Repeat("é", 2*contract.MaxFailedReason)).Failed.Reason
	if n := utf8.RuneCountInString(long); n != contract.MaxFailedReason {
		t.Errorf("long reason has %d characters, want %d", n, contract.MaxFailedReason)
	}
	if !strings.HasSuffix(long, "…") {
		t.Errorf("long reason does not end with an ellipsis")
	}
	exact := strings.Repeat("a", contract.MaxFailedReason)
	if got := Failed(exact).Failed.Reason; got != exact {
		t.Errorf("a reason of exactly %d characters was changed", contract.MaxFailedReason)
	}
}

func TestContractsListsVersionOneZero(t *testing.T) {
	if len(Contracts) != 1 || Contracts[0] != "1.0" {
		t.Errorf("Contracts = %v, want [1.0]", Contracts)
	}
}

// panickyLog is a run log that panics on the first write containing
// trigger, to inject a panic into Finish.
type panickyLog struct {
	bytes.Buffer
	trigger string
	fired   bool
}

func (p *panickyLog) Write(b []byte) (int, error) {
	if !p.fired && strings.Contains(string(b), p.trigger) {
		p.fired = true
		panic("log write failed")
	}
	return p.Buffer.Write(b)
}

func rejected(*Run) contract.Result {
	// body_file does not exist in AGENT_LOOP_OUT, so ValidateResult rejects it.
	return contract.Result{Result: contract.OutcomeDone, Done: &contract.Done{Implement: &contract.ImplementDone{
		Head:   strings.Repeat("a", 40),
		Change: &contract.NewChange{Title: "t", BodyFile: "body.md"},
	}}}
}

func TestD2_PanicInFinishBeforeWriteWritesFailed(t *testing.T) {
	f := newFixture(t, inputJSON(contract.StageImplement, "1.0"))
	log := &panickyLog{trigger: "result rejected"}
	code := Main(contract.StageImplement, nil, f.getenv, log, rejected)
	if code != ExitResultWritten {
		t.Fatalf("exit = %d, want %d; log:\n%s", code, ExitResultWritten, log.String())
	}
	if !log.fired {
		t.Fatal("the injected panic did not fire")
	}
	reason := f.failedReason(contract.StageImplement)
	if reason != "internal error: cannot finish the result: log write failed" {
		t.Errorf("reason = %q", reason)
	}
	if r := f.result(contract.StageImplement); r.RunID != testRunID {
		t.Errorf("run_id = %q, want %q", r.RunID, testRunID)
	}
	if s := log.String(); !strings.Contains(s, "panic in Finish: log write failed") || !strings.Contains(s, "goroutine") {
		t.Errorf("log has no panic and stack: %q", s)
	}
}

func TestD2_PanicInFinishAfterWriteKeepsResult(t *testing.T) {
	f := newFixture(t, inputJSON(contract.StageImplement, "1.0"))
	log := &panickyLog{trigger: "result: failed: stage ran"}
	code := Main(contract.StageImplement, nil, f.getenv, log, ok)
	if code != ExitResultWritten {
		t.Fatalf("exit = %d, want %d; log:\n%s", code, ExitResultWritten, log.String())
	}
	if !log.fired {
		t.Fatal("the injected panic did not fire")
	}
	if reason := f.failedReason(contract.StageImplement); reason != "stage ran" {
		t.Errorf("reason = %q, want the written result kept", reason)
	}
}

func TestD2_PanicInBodyThenInFinishWritesFailed(t *testing.T) {
	f := newFixture(t, inputJSON(contract.StageImplement, "1.0"))
	log := &panickyLog{trigger: "result: failed: internal error: boom"}
	code := Main(contract.StageImplement, nil, f.getenv, log, func(*Run) contract.Result { panic("boom") })
	if code != ExitResultWritten {
		t.Fatalf("exit = %d; log:\n%s", code, log.String())
	}
	if reason := f.failedReason(contract.StageImplement); reason != "internal error: boom" {
		t.Errorf("reason = %q", reason)
	}
}

func TestD2_PanicInFinishWithUnwritableResultExitsNonZero(t *testing.T) {
	f := newFixture(t, inputJSON(contract.StageImplement, "1.0"))
	f.env[EnvResult] = filepath.Join(t.TempDir(), "missing", "result.json")
	log := &panickyLog{trigger: "result rejected"}
	code := Main(contract.StageImplement, nil, f.getenv, log, rejected)
	if code != ExitNoResult {
		t.Fatalf("exit = %d, want %d", code, ExitNoResult)
	}
	if !strings.Contains(log.String(), "cannot write the failed result") {
		t.Errorf("log = %q", log.String())
	}
}

func TestD2_PanickingLogInMainRecoverDoesNotCrash(t *testing.T) {
	f := newFixture(t, inputJSON(contract.StageImplement, "1.0"))
	log := &panickyLog{trigger: "panic: boom"}
	code := Main(contract.StageImplement, nil, f.getenv, log, func(*Run) contract.Result { panic("boom") })
	if code != ExitResultWritten {
		t.Fatalf("exit = %d", code)
	}
	if reason := f.failedReason(contract.StageImplement); reason != "internal error: boom" {
		t.Errorf("reason = %q", reason)
	}
}
