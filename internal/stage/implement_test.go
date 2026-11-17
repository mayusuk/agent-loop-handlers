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
	t := f.t
	t.Helper()
	t.Setenv("FAKECLAUDE_SHELL", shell)
	t.Setenv("DRAFT", draft)
	for i := 0; i+1 < len(extra); i += 2 {
		t.Setenv(extra[i], extra[i+1])
	}
	in := contract.Input{
		ContractVersion: contract.Version,
		RunID:           "r-1",
		Stage:           contract.StageImplement,
		Attempt:         1,
		Deadline:        time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		Identity:        "coder",
		Project:         contract.InputProject{Name: "p", Tracker: "github", CodeHost: "github"},
		Repo:            contract.InputRepo{URL: "https://github.com/o/r", BaseBranch: "main"},
		Workspace:       contract.Workspace{Path: f.ws, Branch: "agent/1", Head: f.head},
		Item:            contract.ItemRef{Ref: "1", URL: "https://github.com/o/r/issues/1", Type: "feature"},
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
	}
	getenv := func(k string) string { return vars[k] }
	var stderr bytes.Buffer
	code := run.Main(contract.StageImplement, nil, getenv, &stderr, func(r *run.Run) contract.Result {
		return Implement(context.Background(), r, prompt.Flags{}, Env{Getenv: getenv, Environ: os.Environ()})
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
