package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mayusuk/agent-loop-handlers/internal/run"
	"github.com/mayusuk/agent-loop/contract"
)

func inputFor(stage string) string {
	return `{"contract_version":"1.0","run_id":"r-1","stage":"` + stage + `","attempt":1,
"deadline":"2026-11-14T16:00:00Z","identity":"coder",
"project":{"name":"p","tracker":"github","code_host":"github"},
"repo":{"url":"https://github.com/o/r","base_branch":"main"},
"workspace":{"path":"/ws","branch":"agent/1","head":"9f2c"},
"item":{"ref":"1","url":"https://github.com/o/r/issues/1","type":"feature"},"change":null}`
}

// env writes an input for stage and returns the run's environment.
func env(t *testing.T, stage string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(dir, "input.json")
	if err := os.WriteFile(in, []byte(inputFor(stage)), 0o644); err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		run.EnvInput:    in,
		run.EnvResult:   filepath.Join(dir, "result.json"),
		run.EnvOut:      out,
		run.EnvContract: "1.0",
	}
}

func readFailed(t *testing.T, e map[string]string, stage string) string {
	t.Helper()
	data, err := os.ReadFile(e[run.EnvResult])
	if err != nil {
		t.Fatalf("no result: %v", err)
	}
	in := contract.Input{RunID: "r-1", Stage: contract.Stage(stage)}
	r, err := contract.ValidateResult(in, data, e[run.EnvOut])
	if err != nil {
		t.Fatalf("result is invalid: %v\n%s", err, data)
	}
	if r.Result != contract.OutcomeFailed {
		t.Fatalf("result = %q, want failed", r.Result)
	}
	return r.Failed.Reason
}

// TestImplementIsRouted: implement runs the stage, which checks the
// workspace before anything else (work plan #6 D2).
func TestImplementIsRouted(t *testing.T) {
	e := env(t, "implement")
	var stdout, stderr bytes.Buffer
	realMain([]string{"implement"}, func(k string) string { return e[k] }, &stdout, &stderr)
	if reason := readFailed(t, e, "implement"); !strings.HasPrefix(reason, "the workspace is not usable: ") {
		t.Errorf("reason = %q", reason)
	}
}

// TestReviewIsRouted: review runs the stage, which checks its input
// before anything else.
func TestReviewIsRouted(t *testing.T) {
	e := env(t, "review")
	var stdout, stderr bytes.Buffer
	realMain([]string{"review"}, func(k string) string { return e[k] }, &stdout, &stderr)
	if reason := readFailed(t, e, "review"); reason != "the input has no review object" {
		t.Errorf("reason = %q", reason)
	}
}

func TestD11_MintWritesValidFailed(t *testing.T) {
	e := env(t, "mint")
	var stdout, stderr bytes.Buffer
	if code := realMain([]string{"mint"}, func(k string) string { return e[k] }, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d; stderr:\n%s", code, stderr.String())
	}
	if reason := readFailed(t, e, "mint"); reason != "mint is not supported in this release (agent-loop v0.2)" {
		t.Errorf("reason = %q", reason)
	}
	if stdout.Len() != 0 {
		t.Errorf("a stage wrote to stdout: %q", stdout.String())
	}
}

// TestAddressIsRouted: address runs the stage, which checks its input
// before anything else.
func TestAddressIsRouted(t *testing.T) {
	e := env(t, "address")
	var stdout, stderr bytes.Buffer
	if code := realMain([]string{"address"}, func(k string) string { return e[k] }, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d; stderr:\n%s", code, stderr.String())
	}
	if reason := readFailed(t, e, "address"); reason != "the input has no address object" {
		t.Errorf("reason = %q", reason)
	}
}

func TestFR18AC3_StageMismatchThroughCLI(t *testing.T) {
	e := env(t, "review")
	var stdout, stderr bytes.Buffer
	realMain([]string{"implement"}, func(k string) string { return e[k] }, &stdout, &stderr)
	if reason := readFailed(t, e, "implement"); !strings.Contains(reason, `stage "review"`) {
		t.Errorf("reason = %q", reason)
	}
}

func TestStageArgumentsAreFailed(t *testing.T) {
	cases := map[string][]string{
		`implement: unknown argument "--model" (usage: implement [--skill <name>] [--claude <path>] [--deadline-margin <duration>] [--unsandboxed])`:      {"--model", "x"},
		`implement: --skill is given more than once (usage: implement [--skill <name>] [--claude <path>] [--deadline-margin <duration>] [--unsandboxed])`: {"--skill", "a", "--skill", "b"},
		`implement: --skill needs a skill name (usage: implement [--skill <name>] [--claude <path>] [--deadline-margin <duration>] [--unsandboxed])`:      {"--skill"},
	}
	for want, args := range cases {
		e := env(t, "implement")
		var stdout, stderr bytes.Buffer
		if code := realMain(append([]string{"implement"}, args...), func(k string) string { return e[k] }, &stdout, &stderr); code != 0 {
			t.Fatalf("exit = %d", code)
		}
		if reason := readFailed(t, e, "implement"); reason != want {
			t.Errorf("args %q: reason = %q, want %q", args, reason, want)
		}
	}
}

// TestD7_SkillFlagIsAccepted: --skill, --claude and --deadline-margin pass
// the argument check, so the stage's own input check is the result (work
// plan #4 D7, plan D3, D13).
func TestD7_SkillFlagIsAccepted(t *testing.T) {
	for _, args := range [][]string{
		{"--skill", "work-issue"}, {"--skill=work-issue"},
		{"--claude", "/x/claude"}, {"--claude=/x/claude", "--deadline-margin=30s"},
		{"--deadline-margin", "1m", "--skill", "review-pr"},
	} {
		e := env(t, "address")
		var stdout, stderr bytes.Buffer
		realMain(append([]string{"address"}, args...), func(k string) string { return e[k] }, &stdout, &stderr)
		if reason := readFailed(t, e, "address"); reason != "the input has no address object" {
			t.Errorf("args %q: reason = %q", args, reason)
		}
	}
}

func TestD7_VersionJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := realMain([]string{"version", "--json"}, os.Getenv, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d; stderr:\n%s", code, stderr.String())
	}
	var v map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &v); err != nil {
		t.Fatalf("not JSON: %v: %q", err, stdout.String())
	}
	if len(v) != 2 {
		t.Errorf("keys = %v, want version and contracts only", v)
	}
	if s, ok := v["version"].(string); !ok || s == "" {
		t.Errorf("version = %v", v["version"])
	}
	cs, ok := v["contracts"].([]any)
	if !ok || len(cs) != 1 || cs[0] != "1.0" {
		t.Errorf("contracts = %v, want [1.0]", v["contracts"])
	}
}

func TestD7_VersionText(t *testing.T) {
	old := version
	version = "v9.8.7"
	defer func() { version = old }()
	var stdout, stderr bytes.Buffer
	if code := realMain([]string{"version"}, os.Getenv, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if got, want := stdout.String(), "agent-loop-claude v9.8.7\ncontract versions: 1.0\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestUsageErrors(t *testing.T) {
	cases := map[string][]string{
		"no command":       nil,
		"unknown command":  {"materialise"},
		"bad version flag": {"version", "--yaml"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := realMain(args, os.Getenv, &stdout, &stderr); code != ExitUsage {
				t.Errorf("exit = %d, want %d", code, ExitUsage)
			}
			if !strings.Contains(stderr.String(), "usage:") {
				t.Errorf("stderr = %q", stderr.String())
			}
		})
	}
	var stdout, stderr bytes.Buffer
	if code := realMain([]string{"help"}, os.Getenv, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "usage:") {
		t.Errorf("help: exit = %d, stdout = %q", code, stdout.String())
	}
}

// TestBinaryExitCodes runs the built program as the worker does: the
// exit code is 0 when a result was written and non-zero only when none
// could be (plan D2).
func TestBinaryExitCodes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "agent-loop-claude")
	build := exec.Command("go", "build", "-ldflags", "-X main.version=v0.0.0-test", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	runBin := func(e map[string]string, args ...string) (int, string) {
		cmd := exec.Command(bin, args...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
		for k, v := range e {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		err := cmd.Run()
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode(), stdout.String()
		}
		if err != nil {
			t.Fatal(err)
		}
		return 0, stdout.String()
	}

	e := env(t, "mint")
	if code, _ := runBin(e, "mint"); code != 0 {
		t.Errorf("mint: exit = %d, want 0", code)
	}
	readFailed(t, e, "mint")

	e = env(t, "implement")
	e[run.EnvContract] = "2.0"
	if code, _ := runBin(e, "implement"); code != 0 {
		t.Errorf("contract 2.0: exit = %d, want 0", code)
	}
	if reason := readFailed(t, e, "implement"); !strings.Contains(reason, "contract 2.0 is not supported") {
		t.Errorf("contract 2.0: reason = %q", reason)
	}

	e = env(t, "implement")
	delete(e, run.EnvResult)
	if code, _ := runBin(e, "implement"); code != run.ExitNoResult {
		t.Errorf("no result path: exit = %d, want %d", code, run.ExitNoResult)
	}

	if code, _ := runBin(nil, "bogus"); code != ExitUsage {
		t.Errorf("unknown command: exit = %d, want %d", code, ExitUsage)
	}
	code, out := runBin(nil, "version", "--json")
	if code != 0 || strings.TrimSpace(out) != `{"version":"v0.0.0-test","contracts":["1.0"]}` {
		t.Errorf("version --json: exit = %d, out = %q", code, out)
	}
}
