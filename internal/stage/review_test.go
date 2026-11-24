package stage

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mayusuk/agent-loop/contract"
)

// newReviewFixture makes a bare remote whose agent/1 adds src.go (3 lines)
// and docs/a.md on main, and a workspace detached at that commit, as the
// worker prepares a review (INTEGRATION-SPEC §3.3).
func newReviewFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{t: t, root: root, remote: filepath.Join(root, "remote.git"), ws: filepath.Join(root, "ws")}
	git(t, root, "init", "-q", "--bare", "-b", "main", f.remote)
	seed := filepath.Join(root, "seed")
	git(t, root, "clone", "-q", f.remote, seed)
	f.identity(seed)
	git(t, seed, "commit", "-q", "--allow-empty", "-m", "base")
	git(t, seed, "push", "-q", "origin", "HEAD:main")
	git(t, seed, "checkout", "-q", "-b", "agent/1")
	os.WriteFile(filepath.Join(seed, "src.go"), []byte("package a\n\nfunc A() {}\n"), 0o644)
	os.MkdirAll(filepath.Join(seed, "docs"), 0o755)
	os.WriteFile(filepath.Join(seed, "docs", "a.md"), []byte("a\n"), 0o644)
	git(t, seed, "add", ".")
	git(t, seed, "commit", "-q", "-m", "change")
	git(t, seed, "push", "-q", "origin", "agent/1")
	git(t, root, "clone", "-q", f.remote, f.ws)
	f.identity(f.ws)
	git(t, f.ws, "checkout", "-q", "--detach", "origin/agent/1")
	f.head = git(t, f.ws, "rev-parse", "HEAD")
	return f
}

// refs is every ref on the remote with its SHA.
func (f *fixture) refs() string {
	f.t.Helper()
	cmd := exec.Command("git", "for-each-ref", "--format=%(refname) %(objectname)")
	cmd.Dir = f.remote
	out, err := cmd.Output()
	if err != nil {
		f.t.Fatal(err)
	}
	return string(out)
}

func (f *fixture) reviewInput() contract.Input {
	in := f.input(contract.StageReview)
	in.Review = &contract.ReviewInput{
		Kind: contract.KindRound, Commit: f.head, BlockingThreshold: 4,
		RuleFindings: []string{"F1", "F2"}, RuleManual: []string{"MC-1"},
	}
	return in
}

// review runs the review stage and checks the remote is unchanged (D9).
func (f *fixture) review(shell, draft string, extra ...string) outcome {
	f.t.Helper()
	before := f.refs()
	o := f.runStage(f.reviewInput(), Review, shell, draft, extra...)
	if after := f.refs(); after != before {
		f.t.Errorf("the remote changed during review:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	return o
}

const (
	rulings = `"rulings":[{"finding":"F1","status":"addressed"},{"finding":"F2","status":"open","note":"Still nil."}],` +
		`"manual_rulings":[{"comment":"MC-1","ruling":"addressed"}]`
)

// reviewDraft is a done draft ruling on every ID with the given new
// findings JSON.
func reviewDraft(findings string) string {
	return `{"result":"done","done":{"summary":"One issue.",` + rulings + `,"new_findings":[` + findings + `]}}`
}

func reviewDone(t *testing.T, o outcome) *contract.ReviewDone {
	t.Helper()
	if o.res.Result != contract.OutcomeDone {
		t.Fatalf("result = %s: %+v\n%s", o.res.Result, o.res.Failed, o.stderr)
	}
	return o.res.Done.Review
}

func body(t *testing.T, o outcome, f contract.NewFinding) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(o.out, f.BodyFile))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestI5_2_ReviewDoneRulesEveryIDAtTheCommit(t *testing.T) {
	f := newReviewFixture(t)
	o := f.review(draftOnly, reviewDraft(`{"score":4,"path":"src.go","line":3,"title":"A is empty","body":"Fill it."}`))
	d := reviewDone(t, o)
	if d.Commit != f.head || d.Kind != contract.KindRound {
		t.Errorf("commit %s kind %s, want %s round", d.Commit, d.Kind, f.head)
	}
	if len(d.Rulings) != 2 || len(d.ManualRulings) != 1 {
		t.Errorf("rulings %+v manual %+v", d.Rulings, d.ManualRulings)
	}
	nf := d.NewFindings[0]
	if nf.Path == nil || *nf.Path != "src.go" || nf.Line == nil || *nf.Line != 3 {
		t.Errorf("finding = %+v, want src.go:3 kept", nf)
	}
	if b := body(t, o, nf); b != "Fill it." {
		t.Errorf("body = %q", b)
	}
	if o.res.Usage == nil || o.res.Usage.Cost == nil {
		t.Errorf("usage = %+v, want the fake's", o.res.Usage)
	}
}

func TestFR4AC4AC8_MissingRulingIsFailed(t *testing.T) {
	cases := map[string]string{
		"F2":   `{"result":"done","done":{"summary":"s","rulings":[{"finding":"F1","status":"addressed"}],"manual_rulings":[{"comment":"MC-1","ruling":"open"}]}}`,
		"MC-1": `{"result":"done","done":{"summary":"s","rulings":[{"finding":"F1","status":"addressed"},{"finding":"F2","status":"open"}]}}`,
	}
	for id, draft := range cases {
		t.Run(id, func(t *testing.T) {
			f := newReviewFixture(t)
			if r := failedReason(t, f.review(draftOnly, draft)); !strings.Contains(r, id) {
				t.Errorf("reason = %q, want it to name %s", r, id)
			}
		})
	}
}

func TestD8_PathNotAtTheCommitMovesToTheBody(t *testing.T) {
	f := newReviewFixture(t)
	// new.go exists only in a commit the agent made after review.commit.
	shell := `echo x > new.go && git add new.go && git commit -qm new && ` + draftOnly
	o := f.review(shell, reviewDraft(
		`{"score":3,"path":"gone.go","line":5,"title":"t1","body":"b1"},`+
			`{"score":3,"path":"docs","title":"t2","body":"b2"},`+
			`{"score":3,"path":"new.go","line":1,"title":"t3","body":"b3"},`+
			`{"score":3,"path":"docs/a.md","title":"t4","body":"b4"},`+
			`{"score":3,"path":"./src.go","line":1,"title":"t5","body":"b5"}`))
	d := reviewDone(t, o)
	want := map[int]string{0: "Location: `gone.go:5`\n\nb1", 1: "Location: `docs`\n\nb2", 2: "Location: `new.go:1`\n\nb3", 4: "Location: `./src.go:1`\n\nb5"}
	for i, w := range want {
		nf := d.NewFindings[i]
		if nf.Path != nil || nf.Line != nil {
			t.Errorf("finding %d = %+v, want no path or line", i+1, nf)
		}
		if b := body(t, o, nf); b != w {
			t.Errorf("finding %d body = %q, want %q", i+1, b, w)
		}
	}
	if nf := d.NewFindings[3]; nf.Path == nil || *nf.Path != "docs/a.md" || body(t, o, nf) != "b4" {
		t.Errorf("finding 4 = %+v, want docs/a.md kept as is", nf)
	}
	if !strings.Contains(o.stderr, "the workspace head moved to") {
		t.Errorf("the moved head is not logged:\n%s", o.stderr)
	}
}

func TestD8_LineNotInTheFileIsDropped(t *testing.T) {
	f := newReviewFixture(t)
	o := f.review(draftOnly, reviewDraft(
		`{"score":2,"path":"src.go","line":4,"title":"t1","body":"b1"},`+
			`{"score":2,"line":2,"title":"t2","body":"b2"}`))
	d := reviewDone(t, o)
	if nf := d.NewFindings[0]; nf.Path == nil || *nf.Path != "src.go" || nf.Line != nil || body(t, o, nf) != "b1" {
		t.Errorf("finding 1 = %+v, want src.go with no line and the body unchanged", nf)
	}
	if nf := d.NewFindings[1]; nf.Path != nil || nf.Line != nil || body(t, o, nf) != "b2" {
		t.Errorf("finding 2 = %+v, want no location", nf)
	}
}

func TestFR18AC1_BlockedReviewDraftIsFailed(t *testing.T) {
	f := newReviewFixture(t)
	if r := failedReason(t, f.review(draftOnly, blockedDraft)); !strings.Contains(r, "blocked") {
		t.Errorf("reason = %q", r)
	}
}

func TestD9_ReviewNeverPushesTheAgentsCommits(t *testing.T) {
	f := newReviewFixture(t)
	shell := `echo x > b.txt && git add b.txt && git commit -qm b && echo y > dirty.txt && ` + draftOnly
	o := f.review(shell, reviewDraft(``))
	if d := reviewDone(t, o); d.Commit != f.head {
		t.Errorf("commit = %s, want %s", d.Commit, f.head)
	}
	if !strings.Contains(o.stderr, "uncommitted changes") {
		t.Errorf("uncommitted changes are not logged:\n%s", o.stderr)
	}
}

func TestD2_ReviewWorkspaceNotAtTheCommitIsFailed(t *testing.T) {
	f := newReviewFixture(t)
	git(t, f.ws, "checkout", "-q", "--detach", "origin/main")
	o := f.review(`touch ran && `+draftOnly, reviewDraft(``))
	if r := failedReason(t, o); !strings.Contains(r, "not review.commit "+f.head) {
		t.Errorf("reason = %q", r)
	}
	if _, err := os.Stat(filepath.Join(f.ws, "ran")); err == nil {
		t.Errorf("claude ran in the wrong workspace")
	}
}

// Every path form work plan #7 D4 treats as not repository-relative moves
// to the body, even when it names a file that is at the commit (#29 R2).
func TestD8_BadPathFormsMoveToTheBody(t *testing.T) {
	paths := []string{"/src.go", `docs\a.md`, "docs//a.md", "src.go/", "./src.go", "docs/./a.md", "docs/../src.go", ".", ".."}
	var findings []string
	for i, p := range paths {
		findings = append(findings, fmt.Sprintf(`{"score":3,"path":%q,"line":1,"title":"t%d","body":"b"}`, p, i))
	}
	f := newReviewFixture(t)
	o := f.review(draftOnly, reviewDraft(strings.Join(findings, ",")))
	d := reviewDone(t, o)
	for i, p := range paths {
		nf := d.NewFindings[i]
		if nf.Path != nil || nf.Line != nil {
			t.Errorf("%s: finding = %+v, want no path or line", p, nf)
		}
		if b, want := body(t, o, nf), "Location: `"+p+":1`\n\nb"; b != want {
			t.Errorf("%s: body = %q, want %q", p, b, want)
		}
	}
}

// A path with pathspec magic is not a file at the commit, so its location
// moves to the body and the review is not failed (#29 review R1).
func TestD8_PathspecMagicMovesToTheBody(t *testing.T) {
	paths := []string{":(bad)src.go", ":!src.go", ":(glob)src.go", ":/src.go"}
	var findings []string
	for i, p := range paths {
		findings = append(findings, fmt.Sprintf(`{"score":3,"path":%q,"line":1,"title":"t%d","body":"b"}`, p, i))
	}
	f := newReviewFixture(t)
	o := f.review(draftOnly, reviewDraft(strings.Join(findings, ",")))
	d := reviewDone(t, o)
	for i, p := range paths {
		nf := d.NewFindings[i]
		if nf.Path != nil || nf.Line != nil {
			t.Errorf("%s: finding = %+v, want no path or line", p, nf)
		}
		if b, want := body(t, o, nf), "Location: `"+p+":1`\n\nb"; b != want {
			t.Errorf("%s: body = %q, want %q", p, b, want)
		}
	}
}

// A backtick or a control character in the path keeps the Location line
// one line and one code span (#29 R4, D2).
func TestD8_LocationLineSurvivesBackticksAndNewlines(t *testing.T) {
	f := newReviewFixture(t)
	o := f.review(draftOnly, reviewDraft(
		`{"score":3,"path":"a`+"`"+`b.go","line":2,"title":"t1","body":"b1"},`+
			`{"score":3,"path":"x\ny.go","title":"t2","body":"b2"}`))
	d := reviewDone(t, o)
	for i, want := range []string{"Location: ``a`b.go:2``\n\nb1", "Location: `x\\ny.go`\n\nb2"} {
		if b := body(t, o, d.NewFindings[i]); b != want {
			t.Errorf("finding %d body = %q, want %q", i+1, b, want)
		}
	}
}

// A path with a space at each end keeps both spaces in the Location line
// (#29 review R3, D2).
func TestD8_LocationLineKeepsEdgeSpaces(t *testing.T) {
	f := newReviewFixture(t)
	o := f.review(draftOnly, reviewDraft(`{"score":3,"path":" src.go ","title":"t","body":"b"}`))
	if b, want := body(t, o, reviewDone(t, o).NewFindings[0]), "Location: `  src.go  `\n\nb"; b != want {
		t.Errorf("body = %q, want %q", b, want)
	}
}

func TestD8_CodeSpan(t *testing.T) {
	for in, want := range map[string]string{
		"a.go":            "`a.go`",
		"a`b":             "``a`b``",
		"a``b`c":          "```a``b`c```",
		"`a":              "`` `a ``",
		"a`":              "`` a` ``",
		"a\nb\tc\x01\x7f": "`a\\nb\\tc\\x01\\x7f`",
		"a\r\n":           "`a\\r\\n`",
		" a.go ":          "`  a.go  `",
		" a.go":           "` a.go`",
		"a.go ":           "`a.go `",
		" a` ":            "``  a`  ``",
		" `a":             "`` `a``",
	} {
		if got := codeSpan(in); got != want {
			t.Errorf("codeSpan(%q) = %q, want %q", in, got, want)
		}
	}
}

// A body the Location prefix pushes over 64 KiB is failed like any
// oversized text; one that still fits is kept (work plan #7 D4, #29 R3).
func TestD8_LocationPrefixOverflowIsFailed(t *testing.T) {
	prefix := len("Location: `gone.go`\n\n")
	finding := func(n int) string {
		return reviewDraft(`{"score":3,"path":"gone.go","title":"t","body":"` + strings.Repeat("x", n) + `"}`)
	}
	f := newReviewFixture(t)
	o := f.review(draftOnly, finding(contract.MaxTextFile-prefix))
	if b := body(t, o, reviewDone(t, o).NewFindings[0]); len(b) != contract.MaxTextFile {
		t.Errorf("body is %d bytes, want exactly %d", len(b), contract.MaxTextFile)
	}
	f = newReviewFixture(t)
	if r := failedReason(t, f.review(draftOnly, finding(contract.MaxTextFile-prefix+1))); !strings.Contains(r, "larger than 64 KiB") {
		t.Errorf("reason = %q, want the body over 64 KiB", r)
	}
}

// A git failure reading a file at the commit fails the run, never moving
// the location to the body (work plan #7 D5, #29 R1).
func TestD5_ReviewGitReadFailureIsFailed(t *testing.T) {
	f := newReviewFixture(t)
	shell := `rm -rf .git/objects/?? .git/objects/pack && ` + draftOnly
	r := failedReason(t, f.review(shell, reviewDraft(`{"score":3,"path":"src.go","line":1,"title":"t","body":"b"}`)))
	if want := "cannot read src.go at " + f.head + ": "; !strings.HasPrefix(r, want) {
		t.Errorf("reason = %q, want it to start %q", r, want)
	}
}
