package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mayusuk/agent-loop-handlers/internal/testing/fakeclaude/fakebin"
)

var fakeBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fakeclaude-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fakeBin, err = fakebin.Build(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type record struct {
	Args     []string `json:"args"`
	Dir      string   `json:"dir"`
	StdinEOF bool     `json:"stdin_eof"`
	Pgid     int      `json:"pgid"`
	Pid      int      `json:"pid"`
}

// env returns the handler's environment plus the fake's script.
func env(script ...string) []string {
	return append(os.Environ(), script...)
}

func readRecord(t *testing.T, path string) record {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fake left no record: %v", err)
	}
	var r record
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestT2_FakeGetsPromptRunsInWorkspaceWithClosedStdin(t *testing.T) {
	ws := t.TempDir()
	rec := filepath.Join(t.TempDir(), "rec.json")
	prompt := "---\nimplement the item\n"
	var stdout, stderr bytes.Buffer
	_, err := Run(context.Background(), Options{
		Bin:    fakeBin,
		Args:   Args(prompt, "/run/settings.json"),
		Dir:    ws,
		Env:    env("FAKECLAUDE_RECORD="+rec, "FAKECLAUDE_STDERR=working"),
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	r := readRecord(t, rec)
	if got := r.Args[len(r.Args)-1]; got != prompt {
		t.Errorf("last argument = %q, want the prompt", got)
	}
	if r.Args[len(r.Args)-2] != "--" {
		t.Errorf("prompt not after --: %q", r.Args)
	}
	want, _ := filepath.EvalSymlinks(ws)
	if got, _ := filepath.EvalSymlinks(r.Dir); got != want {
		t.Errorf("dir = %q, want %q", r.Dir, ws)
	}
	if !r.StdinEOF {
		t.Error("stdin is not closed")
	}
	if r.Pgid != r.Pid || r.Pgid == syscall.Getpgrp() {
		t.Errorf("claude is not in its own process group: pid %d pgid %d", r.Pid, r.Pgid)
	}
	for _, a := range r.Args {
		if a == "--dangerously-skip-permissions" {
			t.Errorf("args contain --dangerously-skip-permissions: %q", r.Args)
		}
	}
	if !strings.Contains(stdout.String(), `"total_cost_usd"`) {
		t.Errorf("stdout not streamed: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "working") {
		t.Errorf("stderr not streamed: %q", stderr.String())
	}
}

func TestD12_Args(t *testing.T) {
	for _, tc := range []struct {
		settings string
		want     string
	}{
		{"", "-p --output-format json --permission-mode acceptEdits -- go"},
		{"/s.json", "-p --output-format json --permission-mode acceptEdits --settings /s.json -- go"},
	} {
		got := strings.Join(Args("go", tc.settings), " ")
		if got != tc.want {
			t.Errorf("Args(settings %q) = %q, want %q", tc.settings, got, tc.want)
		}
		if strings.Contains(got, "dangerously") {
			t.Errorf("Args contains a skip-permissions flag: %q", got)
		}
	}
}

func TestD13_Find(t *testing.T) {
	envOf := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if got, _ := Find("/flag/claude", envOf(map[string]string{EnvBin: "/env/claude"})); got != "/flag/claude" {
		t.Errorf("flag: got %q", got)
	}
	if got, _ := Find("", envOf(map[string]string{EnvBin: "/env/claude"})); got != "/env/claude" {
		t.Errorf("env: got %q", got)
	}
	t.Setenv("PATH", filepath.Dir(fakeBin))
	if got, err := Find("", envOf(nil)); err != nil || got != fakeBin {
		t.Errorf("PATH: got %q, %v; want %q", got, err, fakeBin)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := Find("", envOf(nil)); err == nil {
		t.Error("no claude anywhere: want an error")
	}
}

func TestD3_StopAt(t *testing.T) {
	got, err := StopAt("2026-11-14T16:00:00Z", DefaultMargin)
	if err != nil || !got.Equal(time.Date(2026, 11, 14, 15, 58, 0, 0, time.UTC)) {
		t.Errorf("StopAt = %v, %v", got, err)
	}
	if _, err := StopAt("tomorrow", DefaultMargin); err == nil {
		t.Error("bad deadline: want an error")
	}
}

func TestD3_StopTimePassedDoesNotStart(t *testing.T) {
	rec := filepath.Join(t.TempDir(), "rec.json")
	_, err := Run(context.Background(), Options{
		Bin: fakeBin, Args: Args("p", ""), Dir: t.TempDir(),
		Env:    env("FAKECLAUDE_RECORD=" + rec),
		StopAt: time.Now().Add(-time.Second),
	})
	if !errors.Is(err, ErrTimeLimit) {
		t.Fatalf("err = %v, want ErrTimeLimit", err)
	}
	if _, err := os.Stat(rec); err == nil {
		t.Error("claude was started after the stop time")
	}
}

// alive reports whether pid is still running, waiting up to 5s for a
// killed process to be reaped by init.
func alive(pid int) bool {
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return false
		}
	}
	return true
}

func childPid(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no child pid: %v", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

func TestFR18AC4_SleepPastDeadlineStopsGroup(t *testing.T) {
	for _, tc := range []struct {
		name   string
		script []string
	}{
		{"exits on TERM", nil},
		{"ignores TERM", []string{"FAKECLAUDE_IGNORE_TERM=1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pidfile := filepath.Join(t.TempDir(), "child.pid")
			start := time.Now()
			out, err := Run(context.Background(), Options{
				Bin: fakeBin, Args: Args("p", ""), Dir: t.TempDir(),
				Env:    env(append([]string{"FAKECLAUDE_CHILD_PIDFILE=" + pidfile, "FAKECLAUDE_SLEEP=60s"}, tc.script...)...),
				StopAt: time.Now().Add(time.Second),
				Grace:  500 * time.Millisecond,
			})
			if !errors.Is(err, ErrTimeLimit) {
				t.Fatalf("err = %v, want ErrTimeLimit", err)
			}
			if el := time.Since(start); el > 10*time.Second {
				t.Errorf("Run took %v", el)
			}
			if out.Usage != nil {
				t.Errorf("usage = %+v, want none (no output)", out.Usage)
			}
			if pid := childPid(t, pidfile); alive(pid) {
				syscall.Kill(pid, syscall.SIGKILL)
				t.Errorf("child %d outlived the run", pid)
			}
		})
	}
}

func TestD2_ChildKilledAfterNormalExit(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "child.pid")
	if _, err := Run(context.Background(), Options{
		Bin: fakeBin, Args: Args("p", ""), Dir: t.TempDir(),
		Env: env("FAKECLAUDE_CHILD_PIDFILE=" + pidfile),
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if pid := childPid(t, pidfile); alive(pid) {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("child %d outlived the run", pid)
	}
}

func TestD6_ChildHoldingStdoutDoesNotHang(t *testing.T) {
	start := time.Now()
	_, err := Run(context.Background(), Options{
		Bin: fakeBin, Args: Args("p", ""), Dir: t.TempDir(),
		Env:    env("FAKECLAUDE_SHELL=sleep 60 >&2 &"),
		Stderr: &bytes.Buffer{},
		Grace:  500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if el := time.Since(start); el > 10*time.Second {
		t.Errorf("Run took %v", el)
	}
}

func TestT2_CallerContextCancelled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err := Run(ctx, Options{
		Bin: fakeBin, Args: Args("p", ""), Dir: t.TempDir(),
		Env:    env("FAKECLAUDE_SLEEP=60s"),
		StopAt: time.Now().Add(time.Minute),
		Grace:  200 * time.Millisecond,
	})
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrTimeLimit) {
		t.Fatalf("err = %v, want the caller's context error", err)
	}
}

func TestT2_NonZeroExitIsErrorWithCode(t *testing.T) {
	out, err := Run(context.Background(), Options{
		Bin: fakeBin, Args: Args("p", ""), Dir: t.TempDir(),
		Env: env("FAKECLAUDE_EXIT=3"),
	})
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 3 {
		t.Fatalf("err = %v, want ExitError code 3", err)
	}
	if !strings.Contains(err.Error(), "code 3") {
		t.Errorf("message %q does not name the code", err)
	}
	if out.Usage == nil {
		t.Error("usage of a failed run is lost (work plan D5)")
	}
}

func TestD6_KilledBySignalIsErrorWithSignal(t *testing.T) {
	_, err := Run(context.Background(), Options{
		Bin: fakeBin, Args: Args("p", ""), Dir: t.TempDir(),
		Env: env("FAKECLAUDE_OUTPUT_FIRST=1", "FAKECLAUDE_SLEEP=60s", "FAKECLAUDE_SHELL=kill -KILL $PPID"),
	})
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != -1 || ee.Signal != syscall.SIGKILL {
		t.Fatalf("err = %v, want ExitError killed by SIGKILL", err)
	}
	if errors.Is(err, ErrTimeLimit) {
		t.Error("a kill outside the time limit is reported as the time limit")
	}
	if want := "claude was killed by signal " + syscall.SIGKILL.String(); err.Error() != want {
		t.Errorf("message %q, want %q", err, want)
	}
}

func TestD5_UsageKeptWhenRunTimesOut(t *testing.T) {
	out, err := Run(context.Background(), Options{
		Bin: fakeBin, Args: Args("p", ""), Dir: t.TempDir(),
		Env:    env("FAKECLAUDE_OUTPUT_FIRST=1", "FAKECLAUDE_SLEEP=60s"),
		StopAt: time.Now().Add(time.Second),
		Grace:  500 * time.Millisecond,
	})
	if !errors.Is(err, ErrTimeLimit) {
		t.Fatalf("err = %v, want ErrTimeLimit", err)
	}
	u := out.Usage
	if u == nil || u.Cost == nil || *u.Cost != 0.25 || *u.InputTokens != 123 || *u.OutputTokens != 40 {
		t.Fatalf("usage = %+v, want the output's usage", u)
	}
}

// Work plan #19 D2: claude ends on its own before the stop time, or
// before the caller cancels its context, but a child keeps its stderr open
// past that point and past the grace period. The run is reported by
// claude's exit status, not as a time limit or the caller's cancellation.
func TestFR18AC4_ExitBeforeStopTimeWhilePipesDrainIsNotTimeLimit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		exit   string
		code   int  // -2: no error
		cancel bool // cancel the caller's context instead of a stop time
	}{
		{"stop time, exit 0", "0", -2, false},
		{"stop time, exit 3", "3", 3, false},
		{"caller cancels, exit 0", "0", -2, true},
		{"caller cancels, exit 3", "3", 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pidfile := filepath.Join(t.TempDir(), "child.pid")
			ctx := context.Background()
			opts := Options{
				Bin: fakeBin, Args: Args("p", ""), Dir: t.TempDir(),
				Env:    env("FAKECLAUDE_SHELL=sleep 60 >&2 & printf %s $! >"+pidfile, "FAKECLAUDE_EXIT="+tc.exit),
				Stderr: &bytes.Buffer{},
				Grace:  2 * time.Second,
			}
			if tc.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				time.AfterFunc(500*time.Millisecond, cancel)
			} else {
				opts.StopAt = time.Now().Add(time.Second)
			}
			out, err := Run(ctx, opts)
			if tc.cancel && ctx.Err() == nil {
				t.Fatal("the caller's context was not cancelled during the run")
			}
			if errors.Is(err, ErrTimeLimit) || errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want claude's own exit status", err)
			}
			if tc.code == -2 {
				if err != nil {
					t.Fatalf("err = %v, want none", err)
				}
			} else {
				var ee *ExitError
				if !errors.As(err, &ee) || ee.Code != tc.code {
					t.Fatalf("err = %v, want ExitError code %d", err, tc.code)
				}
			}
			if out.Usage == nil {
				t.Error("usage lost")
			}
			if pid := childPid(t, pidfile); alive(pid) {
				syscall.Kill(pid, syscall.SIGKILL)
				t.Errorf("child %d outlived the run", pid)
			}
		})
	}
}

func TestT2_MissingBinaryIsError(t *testing.T) {
	_, err := Run(context.Background(), Options{Bin: filepath.Join(t.TempDir(), "nope"), Dir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "cannot start claude") {
		t.Fatalf("err = %v", err)
	}
}

func TestD4_UsageFromJSONOutput(t *testing.T) {
	out, err := Run(context.Background(), Options{Bin: fakeBin, Args: Args("p", ""), Dir: t.TempDir(), Env: env()})
	if err != nil {
		t.Fatal(err)
	}
	u := out.Usage
	if u == nil || u.Cost == nil || *u.Cost != 0.25 || *u.Currency != "USD" ||
		*u.InputTokens != 123 || *u.OutputTokens != 40 {
		t.Fatalf("usage = %+v", u)
	}
}

func TestD4_MalformedOrMissingOutputGivesNoUsage(t *testing.T) {
	for _, o := range []string{"", "not json", `{"total_cost_usd":`, `[1,2]`, `{"result":"x"}`, `{"a":1}{"b":2}`} {
		out, err := Run(context.Background(), Options{
			Bin: fakeBin, Args: Args("p", ""), Dir: t.TempDir(), Env: env("FAKECLAUDE_OUTPUT=" + o),
		})
		if err != nil {
			t.Errorf("output %q: err %v", o, err)
		}
		if out.Usage != nil {
			t.Errorf("output %q: usage = %+v, want none", o, out.Usage)
		}
	}
}

func TestD4_ParseUsageLenient(t *testing.T) {
	i := func(n int64) *int64 { return &n }
	for _, tc := range []struct {
		name     string
		in       string
		cost     *float64
		inTok    *int64
		outTok   *int64
		wantNone bool
	}{
		{name: "cost only", in: `{"total_cost_usd":1.5}`, cost: ptr(1.5)},
		{name: "tokens only", in: `{"usage":{"input_tokens":7,"output_tokens":2}}`, inTok: i(7), outTok: i(2)},
		{name: "wrong types", in: `{"total_cost_usd":"1","usage":{"input_tokens":"7","output_tokens":2.5}}`, wantNone: true},
		{name: "negative", in: `{"total_cost_usd":-1,"usage":{"input_tokens":-7}}`, wantNone: true},
		{name: "usage not an object", in: `{"usage":[1]}`, wantNone: true},
		{name: "last line after log lines", in: "starting\n{\"total_cost_usd\":2}\n", cost: ptr(2)},
		{name: "huge token count", in: `{"usage":{"output_tokens":99999999999999999999}}`, wantNone: true},
		{name: "input token sum saturates", in: `{"usage":{"input_tokens":9223372036854775807,"cache_creation_input_tokens":9223372036854775807,"cache_read_input_tokens":1}}`, inTok: i(math.MaxInt64)},
		{name: "input token sum at the limit", in: `{"usage":{"input_tokens":9223372036854775806,"cache_read_input_tokens":1}}`, inTok: i(math.MaxInt64)},
	} {
		u := ParseUsage([]byte(tc.in))
		if tc.wantNone {
			if u != nil {
				t.Errorf("%s: usage = %+v, want none", tc.name, u)
			}
			continue
		}
		if u == nil {
			t.Errorf("%s: no usage", tc.name)
			continue
		}
		if !eqF(u.Cost, tc.cost) || !eqI(u.InputTokens, tc.inTok) || !eqI(u.OutputTokens, tc.outTok) {
			t.Errorf("%s: usage = %+v", tc.name, u)
		}
		if (u.Cost != nil) != (u.Currency != nil) {
			t.Errorf("%s: currency set without cost or the reverse", tc.name)
		}
	}
}

func TestD5_TailBufferKeepsLastBytes(t *testing.T) {
	b := &tailBuffer{max: 4}
	for _, s := range []string{"ab", "cd", "ef", "ghijkl", "m"} {
		b.Write([]byte(s))
	}
	if got := string(b.Bytes()); got != "jklm" {
		t.Errorf("tail = %q, want jklm", got)
	}
}

func ptr(f float64) *float64 { return &f }

func eqF(a, b *float64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
func eqI(a, b *int64) bool   { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
