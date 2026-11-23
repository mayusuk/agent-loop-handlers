package stage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mayusuk/agent-loop-handlers/internal/claude"
	"github.com/mayusuk/agent-loop-handlers/internal/prompt"
	"github.com/mayusuk/agent-loop-handlers/internal/run"
	"github.com/mayusuk/agent-loop-handlers/internal/testing/fakeclaude/fakebin"
	"github.com/mayusuk/agent-loop/contract"
)

var fakeBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fakeclaude-")
	if err != nil {
		panic(err)
	}
	fakeBin, err = fakebin.Build(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

const (
	doneDraft       = `{"result":"done","done":{"change":{"title":"Add a","body":"Adds a."}}}`
	blockedDraft    = `{"result":"blocked","blocked":{"reason":"Needs #7."}}`
	needsOwnerDraft = `{"result":"needs_owner","needs_owner":{"question":"Which API?"}}`
	// commitAndDraft is the fake agent: commit a file, write the draft.
	commitAndDraft = `echo a > a.txt && git add a.txt && git commit -qm a && printf '%s' "$DRAFT" > "$AGENT_LOOP_CLAUDE_DRAFT"`
	draftOnly      = `printf '%s' "$DRAFT" > "$AGENT_LOOP_CLAUDE_DRAFT"`
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

type fixture struct {
	t      *testing.T
	root   string
	remote string
	ws     string
	head   string
	// baseTip is the base tip a base-update fixture gives change.base_tip.
	baseTip string
	// flags are the stage's arguments; deadline, if set, replaces the
	// input's hour-away deadline.
	flags    prompt.Flags
	deadline time.Time
}

// newFixture makes a bare remote with main, and a workspace cloned from it
// on agent/1. With existing, agent/1 already has a pushed commit from an
// interrupted run and the workspace checks it out (§3.3, §4.1).
func newFixture(t *testing.T, existing bool) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{t: t, root: root, remote: filepath.Join(root, "remote.git"), ws: filepath.Join(root, "ws")}
	git(t, root, "init", "-q", "--bare", "-b", "main", f.remote)
	seed := filepath.Join(root, "seed")
	git(t, root, "clone", "-q", f.remote, seed)
	f.identity(seed)
	git(t, seed, "commit", "-q", "--allow-empty", "-m", "base")
	git(t, seed, "push", "-q", "origin", "HEAD:main")
	if existing {
		git(t, seed, "checkout", "-q", "-b", "agent/1")
		git(t, seed, "commit", "-q", "--allow-empty", "-m", "interrupted run")
		git(t, seed, "push", "-q", "origin", "agent/1")
	}
	git(t, root, "clone", "-q", f.remote, f.ws)
	f.identity(f.ws)
	if existing {
		git(t, f.ws, "checkout", "-q", "agent/1")
	} else {
		git(t, f.ws, "checkout", "-q", "-b", "agent/1", "origin/main")
	}
	f.head = git(t, f.ws, "rev-parse", "HEAD")
	return f
}

func (f *fixture) identity(dir string) {
	git(f.t, dir, "config", "user.name", "t")
	git(f.t, dir, "config", "user.email", "t@example.com")
}

func (f *fixture) remoteHead() string {
	f.t.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", "-q", "refs/heads/agent/1")
	cmd.Dir = f.remote
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}

type outcome struct {
	res    contract.Result
	in     contract.Input
	out    string
	stderr string
}

// implement runs the implement stage through run.Main with the fake
// claude running shell and writing draft.
func (f *fixture) implement(shell, draft string, extra ...string) outcome {
	return f.runStage(f.input(contract.StageImplement), Implement, shell, draft, extra...)
}

// input is the stage's input for the fixture's workspace, with an
// hour-away deadline unless f.deadline is set.
func (f *fixture) input(stage contract.Stage) contract.Input {
	deadline := time.Now().Add(time.Hour)
	if !f.deadline.IsZero() {
		deadline = f.deadline
	}
	identity := "coder"
	if stage == contract.StageReview {
		identity = "reviewer"
	}
	return contract.Input{
		ContractVersion: contract.Version,
		RunID:           "r-1",
		Stage:           stage,
		Attempt:         1,
		Deadline:        deadline.UTC().Format(time.RFC3339),
		Identity:        identity,
		Project:         contract.InputProject{Name: "p", Tracker: "github", CodeHost: "github"},
		Repo:            contract.InputRepo{URL: "https://github.com/o/r", BaseBranch: "main"},
		Workspace:       contract.Workspace{Path: f.ws, Branch: "agent/1", Head: f.head},
		Item:            contract.ItemRef{Ref: "1", URL: "https://github.com/o/r/issues/1", Type: "feature"},
	}
}

type stageFunc func(context.Context, *run.Run, prompt.Flags, Env) contract.Result

// runStage runs body for in through run.Main with the fake claude running
// shell and writing draft, and validates the result.
func (f *fixture) runStage(in contract.Input, body stageFunc, shell, draft string, extra ...string) outcome {
	t := f.t
	t.Helper()
	t.Setenv("FAKECLAUDE_SHELL", shell)
	t.Setenv("DRAFT", draft)
	for i := 0; i+1 < len(extra); i += 2 {
		t.Setenv(extra[i], extra[i+1])
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	os.Mkdir(out, 0o755)
	inPath := filepath.Join(dir, "input.json")
	os.WriteFile(inPath, data, 0o644)
	vars := map[string]string{
		run.EnvInput:    inPath,
		run.EnvResult:   filepath.Join(dir, "result.json"),
		run.EnvOut:      out,
		run.EnvContract: "1.0",
		claude.EnvBin:   fakeBin,
		// The local runner's sandbox settings (plan D12); a test unsets
		// them by passing AGENT_LOOP_CLAUDE_SETTINGS "" in extra.
		claude.EnvSettings: sandboxSettings(t),
	}
	for i := 0; i+1 < len(extra); i += 2 {
		if strings.HasPrefix(extra[i], "AGENT_LOOP_") {
			vars[extra[i]] = extra[i+1]
		}
	}
	getenv := func(k string) string { return vars[k] }
	var stderr bytes.Buffer
	code := run.Main(in.Stage, nil, getenv, &stderr, func(r *run.Run) contract.Result {
		return body(context.Background(), r, f.flags, Env{Getenv: getenv, Environ: os.Environ()})
	})
	if code != run.ExitResultWritten {
		t.Fatalf("exit = %d\n%s", code, stderr.String())
	}
	got, err := os.ReadFile(vars[run.EnvResult])
	if err != nil {
		t.Fatal(err)
	}
	res, err := contract.ValidateResult(in, got, out)
	if err != nil {
		t.Fatalf("result invalid: %v\n%s", err, got)
	}
	return outcome{res: res, in: in, out: out, stderr: stderr.String()}
}

// sandboxSettings writes settings that turn the sandbox on and make it
// required, as agent-loop's local runner does (work plan #9 D1).
func sandboxSettings(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "claude-settings.json")
	if err := os.WriteFile(p, []byte(`{"sandbox":{"enabled":true,"failIfUnavailable":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func failedReason(t *testing.T, o outcome) string {
	t.Helper()
	if o.res.Result != contract.OutcomeFailed {
		t.Fatalf("result = %s, want failed\n%s", o.res.Result, o.stderr)
	}
	return o.res.Failed.Reason
}

func TestI5_2_ImplementDonePushesAndSetsHead(t *testing.T) {
	f := newFixture(t, false)
	o := f.implement(commitAndDraft, doneDraft)
	if o.res.Result != contract.OutcomeDone {
		t.Fatalf("result = %s: %+v\n%s", o.res.Result, o.res.Failed, o.stderr)
	}
	d := o.res.Done.Implement
	if d.Head == "" || d.Head != f.remoteHead() {
		t.Fatalf("done.head = %q, remote head = %q", d.Head, f.remoteHead())
	}
	if d.Head != git(t, f.ws, "rev-parse", "HEAD") {
		t.Errorf("done.head is not the workspace HEAD")
	}
	if d.Change.Title != "Add a" {
		t.Errorf("title = %q", d.Change.Title)
	}
	if body, _ := os.ReadFile(filepath.Join(o.out, d.Change.BodyFile)); string(body) != "Adds a." {
		t.Errorf("body = %q", body)
	}
	if o.res.Usage == nil || o.res.Usage.Cost == nil {
		t.Errorf("usage = %+v, want the fake's", o.res.Usage)
	}
}

func TestI4_1_RerunBuildsOnInterruptedPushes(t *testing.T) {
	f := newFixture(t, true)
	before := f.remoteHead()
	o := f.implement(commitAndDraft, doneDraft)
	if o.res.Result != contract.OutcomeDone {
		t.Fatalf("result = %s: %+v\n%s", o.res.Result, o.res.Failed, o.stderr)
	}
	head := o.res.Done.Implement.Head
	if head != f.remoteHead() {
		t.Fatalf("done.head %s is not the remote head %s", head, f.remoteHead())
	}
	if parent := git(t, f.ws, "rev-parse", head+"^"); parent != before {
		t.Errorf("new commit's parent = %s, want the interrupted run's %s", parent, before)
	}
}

func TestD3_NoNewCommitIsFailed(t *testing.T) {
	f := newFixture(t, false)
	o := f.implement(draftOnly, doneDraft)
	if r := failedReason(t, o); r != "no new commit since "+f.head {
		t.Errorf("reason = %q", r)
	}
	if h := f.remoteHead(); h != "" {
		t.Errorf("remote branch = %s, want none", h)
	}
}

func TestD3_ResetBranchIsNotPushed(t *testing.T) {
	f := newFixture(t, true)
	before := f.remoteHead()
	// The agent throws away the interrupted run's commit and starts again.
	o := f.implement(`git reset -q --hard origin/main && `+commitAndDraft, doneDraft)
	if r := failedReason(t, o); !strings.Contains(r, "does not build on "+f.head) {
		t.Errorf("reason = %q", r)
	}
	if h := f.remoteHead(); h != before {
		t.Errorf("remote head = %s, want %s unchanged", h, before)
	}
}

func TestD4_RefusedPushIsFailedAndNotForced(t *testing.T) {
	f := newFixture(t, true)
	// Someone else pushes to the branch after the workspace was made.
	other := filepath.Join(f.root, "other")
	git(t, f.root, "clone", "-q", "-b", "agent/1", f.remote, other)
	f.identity(other)
	git(t, other, "commit", "-q", "--allow-empty", "-m", "theirs")
	git(t, other, "push", "-q", "origin", "agent/1")
	theirs := f.remoteHead()

	o := f.implement(commitAndDraft, doneDraft)
	if r := failedReason(t, o); !strings.HasPrefix(r, "push to agent/1 failed: ") || !strings.Contains(r, "rejected") {
		t.Errorf("reason = %q", r)
	}
	if h := f.remoteHead(); h != theirs {
		t.Errorf("remote head = %s, want %s (not forced)", h, theirs)
	}
}

func TestD5_ClaudeFailureIsFailed(t *testing.T) {
	f := newFixture(t, false)
	o := f.implement(commitAndDraft, doneDraft, "FAKECLAUDE_EXIT", "3")
	if r := failedReason(t, o); r != "claude exited with code 3" {
		t.Errorf("reason = %q", r)
	}
	if h := f.remoteHead(); h != "" {
		t.Errorf("remote branch = %s, want none", h)
	}
	if o.res.Usage == nil {
		t.Error("usage is missing from a failed run (D6)")
	}
}

func TestFR18AC3_MissingDraftIsFailed(t *testing.T) {
	f := newFixture(t, false)
	o := f.implement("true", "")
	if r := failedReason(t, o); !strings.HasPrefix(r, "draft: ") {
		t.Errorf("reason = %q", r)
	}
}

func TestI5_2_BlockedAndNeedsOwnerPushNothing(t *testing.T) {
	for _, c := range []struct {
		draft string
		want  contract.Outcome
	}{{blockedDraft, contract.OutcomeBlocked}, {needsOwnerDraft, contract.OutcomeNeedsOwner}} {
		t.Run(string(c.want), func(t *testing.T) {
			f := newFixture(t, false)
			o := f.implement(commitAndDraft, c.draft)
			if o.res.Result != c.want {
				t.Fatalf("result = %s, want %s\n%s", o.res.Result, c.want, o.stderr)
			}
			if h := f.remoteHead(); h != "" {
				t.Errorf("remote branch = %s, want none", h)
			}
			if o.res.Usage == nil {
				t.Error("usage is missing (D6)")
			}
		})
	}
}

func TestD2_WorkspaceMustMatchTheInput(t *testing.T) {
	f := newFixture(t, false)
	git(t, f.ws, "checkout", "-q", "-b", "other")
	o := f.implement(commitAndDraft, doneDraft)
	if r := failedReason(t, o); r != "the workspace is on other, not agent/1" {
		t.Errorf("reason = %q", r)
	}

	f = newFixture(t, false)
	want := f.head
	git(t, f.ws, "commit", "-q", "--allow-empty", "-m", "moved")
	o = f.implement(commitAndDraft, doneDraft)
	if r := failedReason(t, o); !strings.Contains(r, "not "+want) {
		t.Errorf("reason = %q", r)
	}
}

func TestD1_DraftIsOutsideWorkspaceAndRemoved(t *testing.T) {
	f := newFixture(t, false)
	rec := filepath.Join(t.TempDir(), "draftpath")
	o := f.implement(`echo "$AGENT_LOOP_CLAUDE_DRAFT" > `+rec+` && `+commitAndDraft, doneDraft)
	if o.res.Result != contract.OutcomeDone {
		t.Fatalf("result = %s\n%s", o.res.Result, o.stderr)
	}
	data, _ := os.ReadFile(rec)
	p := strings.TrimSpace(string(data))
	if filepath.Base(p) != DraftName {
		t.Errorf("draft path = %q", p)
	}
	if strings.HasPrefix(p, f.ws) || strings.HasPrefix(p, o.out) {
		t.Errorf("draft path %q is inside the workspace or AGENT_LOOP_OUT", p)
	}
	if _, err := os.Stat(filepath.Dir(p)); !os.IsNotExist(err) {
		t.Errorf("draft directory still exists: %v", err)
	}
}

// TestD1_DraftIsUnderTMPDIR: the draft directory is made under TMPDIR, the
// per-run temporary directory the sandbox lets claude write to (work plan
// D1).
func TestD1_DraftIsUnderTMPDIR(t *testing.T) {
	f := newFixture(t, false)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	rec := filepath.Join(t.TempDir(), "draftpath")
	o := f.implement(`echo "$AGENT_LOOP_CLAUDE_DRAFT" > `+rec+` && `+commitAndDraft, doneDraft)
	if o.res.Result != contract.OutcomeDone {
		t.Fatalf("result = %s\n%s", o.res.Result, o.stderr)
	}
	data, _ := os.ReadFile(rec)
	p := strings.TrimSpace(string(data))
	if !strings.HasPrefix(p, tmp+string(filepath.Separator)+"agent-loop-claude-") {
		t.Errorf("draft path %q is not in an agent-loop-claude-* directory under TMPDIR %q", p, tmp)
	}
}

// TestD3_UncommittedChangesAreNotPushed: only the commit is pushed; a file
// the agent left uncommitted is not on the remote, and the log says so
// (work plan D3).
func TestD3_UncommittedChangesAreNotPushed(t *testing.T) {
	f := newFixture(t, false)
	o := f.implement(`echo a > a.txt && git add a.txt && git commit -qm a && echo b > b.txt && echo c >> a.txt && `+draftOnly, doneDraft)
	if o.res.Result != contract.OutcomeDone {
		t.Fatalf("result = %s: %+v\n%s", o.res.Result, o.res.Failed, o.stderr)
	}
	head := o.res.Done.Implement.Head
	if head != f.remoteHead() || head != git(t, f.ws, "rev-parse", "HEAD") {
		t.Fatalf("done.head %s, remote %s: want the commit with a", head, f.remoteHead())
	}
	if files := git(t, f.remote, "ls-tree", "--name-only", head); files != "a.txt" {
		t.Errorf("remote tree = %q, want only a.txt", files)
	}
	if a := git(t, f.remote, "show", head+":a.txt"); a != "a" {
		t.Errorf("remote a.txt = %q, want the committed %q", a, "a")
	}
	if !strings.Contains(o.stderr, "uncommitted changes in the workspace are not pushed") {
		t.Errorf("log does not mention the uncommitted changes:\n%s", o.stderr)
	}
}

// TestFR18AC4_TimeLimitIsFailed: a claude still running at the deadline
// minus --deadline-margin is stopped, and the result is exactly "time limit
// reached", with nothing pushed even though it committed and wrote a done
// draft (FR-18 AC4, plan D3, work plan D5).
func TestFR18AC4_TimeLimitIsFailed(t *testing.T) {
	f := newFixture(t, false)
	f.deadline = time.Now().Add(4 * time.Second)
	f.flags = prompt.Flags{DeadlineMargin: 2 * time.Second, MarginSet: true}
	start := time.Now()
	// The fake commits and writes a done draft, then hangs past the stop
	// time.
	o := f.implement(commitAndDraft, doneDraft, "FAKECLAUDE_SLEEP", "30s")
	if r := failedReason(t, o); r != "time limit reached" {
		t.Errorf("reason = %q", r)
	}
	// claude ran (the 2s margin, not the 2m default, set the stop time)
	// and committed, but nothing was pushed.
	if git(t, f.ws, "rev-parse", "HEAD") == f.head {
		t.Error("claude did not run: --deadline-margin was not used")
	}
	if took := time.Since(start); took > 15*time.Second {
		t.Errorf("took %s; claude was not stopped at the margin", took)
	}
	if h := f.remoteHead(); h != "" {
		t.Errorf("remote branch = %s, want none", h)
	}
}

// TestPlanD3_DefaultMarginStopsBeforeStarting: without --deadline-margin
// the margin is 2 minutes, so a deadline a minute away is already past the
// stop time and claude never runs (plan D3).
func TestPlanD3_DefaultMarginStopsBeforeStarting(t *testing.T) {
	f := newFixture(t, false)
	f.deadline = time.Now().Add(time.Minute)
	rec := filepath.Join(t.TempDir(), "ran")
	o := f.implement(`touch `+rec+` && `+commitAndDraft, doneDraft)
	if r := failedReason(t, o); r != "time limit reached" {
		t.Errorf("reason = %q", r)
	}
	if _, err := os.Stat(rec); !os.IsNotExist(err) {
		t.Errorf("claude ran: %v", err)
	}
}

// TestPlanD13_ClaudeFlagWinsOverTheEnvironment: --claude names the binary
// even when AGENT_LOOP_CLAUDE_BIN points elsewhere (plan D13).
func TestPlanD13_ClaudeFlagWinsOverTheEnvironment(t *testing.T) {
	f := newFixture(t, false)
	f.flags = prompt.Flags{Claude: fakeBin}
	o := f.implement(commitAndDraft, doneDraft, claude.EnvBin, filepath.Join(t.TempDir(), "missing"))
	if o.res.Result != contract.OutcomeDone {
		t.Fatalf("result = %s: %+v\n%s", o.res.Result, o.res.Failed, o.stderr)
	}
	if o.res.Done.Implement.Head != f.remoteHead() {
		t.Errorf("done.head %s is not the remote head %s", o.res.Done.Implement.Head, f.remoteHead())
	}

	// Without the flag the environment's missing binary is used and fails.
	f = newFixture(t, false)
	o = f.implement(commitAndDraft, doneDraft, claude.EnvBin, filepath.Join(t.TempDir(), "missing"))
	failedReason(t, o)
}
