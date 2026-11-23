package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mayusuk/agent-loop-handlers/internal/claude"
	"github.com/mayusuk/agent-loop-handlers/internal/run"
	"github.com/mayusuk/agent-loop-handlers/internal/testing/fakeclaude/fakebin"
	"github.com/mayusuk/agent-loop/contract"
)

// handlerBin is agent-loop-claude, and fakeClaude the fake claude, both
// built once in TestMain.
var handlerBin, fakeClaude string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "agent-loop-e2e-")
	if err != nil {
		panic(err)
	}
	code := func() int {
		defer os.RemoveAll(dir)
		handlerBin = filepath.Join(dir, "agent-loop-claude")
		if out, err := exec.Command("go", "build", "-o", handlerBin, "github.com/mayusuk/agent-loop-handlers/cmd/agent-loop-claude").CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "build agent-loop-claude: %v\n%s", err, out)
			return 1
		}
		if fakeClaude, err = fakebin.Build(dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return m.Run()
	}()
	os.Exit(code)
}

// The smoke repository, item and change (work plan D2).
const (
	branch     = "agent/1"
	itemRef    = "1"
	changeRef  = "2"
	ownerID    = "MC-1"
	stageLimit = 20 * time.Minute // work plan D6
)

// seed is the small Go repository the stages work on.
var seed = map[string]string{
	"go.mod": "module example.com/calc\n\ngo 1.25\n",
	"calc.go": `// Package calc does arithmetic.
package calc

// Add returns a + b.
func Add(a, b int) int { return a + b }
`,
	"calc_test.go": `package calc

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Fatalf("Add(2, 3) = %d, want 5", got)
	}
}
`,
	"CLAUDE.md": `# calc

A tiny Go module used by agent-loop-claude's smoke test. There is no GitHub here:
use only the ` + "`gh issue view`" + ` and ` + "`gh pr view`" + ` commands the run facts name, and never
` + "`git push`" + ` (the handler pushes).

Checks, run before you finish: ` + "`gofmt -l .`" + ` prints nothing, ` + "`go vet ./...`" + `, ` + "`go test ./...`" + `.
`,
	// The Go caches live here so a sandboxed agent can build (work plan D4).
	".gitignore": ".cache/\n",
}

const itemText = `title:	Add Sub
state:	OPEN
labels:	feature, status:in-progress
number:	1
--
Add ` + "`func Sub(a, b int) int`" + ` to package calc, returning a - b, with a table test
that covers a negative result.
`

// ownerComment is the change's one manual comment (work plan D3).
const ownerComment = `### ` + ownerID + ` (owner, conversation comment)

Please give Sub a doc comment that says what it returns.
`

// ghScript is the fake gh (work plan D1): it answers "gh issue view <item>"
// and "gh pr view <change>" from files next to it, and refuses the rest.
const ghScript = `#!/bin/sh
dir=$(cd "$(dirname "$0")" && pwd)
printf '%s\n' "$*" >> "$dir/calls.log"
case "$1 $2 $3" in
"issue view ` + itemRef + `") cat "$dir/item.txt"; exit 0 ;;
"pr view ` + changeRef + `") cat "$dir/change.txt"; exit 0 ;;
esac
echo "gh (agent-loop-claude smoke test): offline, there is no GitHub here." >&2
echo "Only 'gh issue view ` + itemRef + `' and 'gh pr view ` + changeRef + `' work; the review threads and comments are in 'gh pr view ` + changeRef + `'." >&2
exit 1
`

// smoke is one smoke run: a bare remote, the fake gh, and how to run the
// handler. The test plays the worker (work plan D2).
type smoke struct {
	t      *testing.T
	root   string
	remote string
	ghDir  string
	base   string
	// claudeBin is AGENT_LOOP_CLAUDE_BIN, or "" for PATH.
	claudeBin string
	// settings is AGENT_LOOP_CLAUDE_SETTINGS, or "" with unsandboxed.
	settings    string
	unsandboxed bool
	// stageEnv adds variables to one stage's handler environment.
	stageEnv map[contract.Stage][]string
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, data string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
}

// newSmoke seeds the bare remote's main with the seed repository and
// writes the fake gh with the item.
func newSmoke(t *testing.T) *smoke {
	t.Helper()
	root := t.TempDir()
	s := &smoke{t: t, root: root, remote: filepath.Join(root, "remote.git"), ghDir: filepath.Join(root, "gh"), stageEnv: map[contract.Stage][]string{}}
	git(t, root, "init", "-q", "--bare", "-b", "main", s.remote)
	src := filepath.Join(root, "seed")
	git(t, root, "clone", "-q", s.remote, src)
	identity(t, src)
	for name, data := range seed {
		writeFile(t, filepath.Join(src, name), data, 0o644)
	}
	git(t, src, "add", "-A")
	git(t, src, "commit", "-q", "-m", "Start calc")
	git(t, src, "push", "-q", "origin", "HEAD:main")
	s.base = git(t, src, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(s.ghDir, "gh"), ghScript, 0o755)
	writeFile(t, filepath.Join(s.ghDir, "item.txt"), itemText, 0o644)
	s.setChange("", nil)
	return s
}

func identity(t *testing.T, dir string) {
	git(t, dir, "config", "user.name", "agent-loop smoke")
	git(t, dir, "config", "user.email", "smoke@example.invalid")
}

// remoteHead is the bare remote's agent branch, or "".
func (s *smoke) remoteHead() string {
	cmd := exec.Command("git", "rev-parse", "--verify", "-q", "refs/heads/"+branch)
	cmd.Dir = s.remote
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}

// workspace clones the remote fresh for one stage and checks out what
// checkout names (work plan D2).
func (s *smoke) workspace(name string, checkout ...string) string {
	s.t.Helper()
	ws := filepath.Join(s.root, "ws-"+name)
	git(s.t, s.root, "clone", "-q", s.remote, ws)
	identity(s.t, ws)
	git(s.t, ws, append([]string{"checkout", "-q"}, checkout...)...)
	return ws
}

// finding is a posted review finding, with the ID the worker gave it.
type finding struct {
	ID   string
	F    contract.NewFinding
	Body string
}

// setChange writes what "gh pr view <change>" prints: the change's title
// and body, its findings and the owner's comment (work plan D1, D3).
func (s *smoke) setChange(desc string, findings []finding) {
	s.t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "number:\t%s\nstate:\tOPEN\nbase:\tmain\nhead:\t%s\n--\n%s\n", changeRef, branch, desc)
	if len(findings) > 0 {
		b.WriteString("\n## Review threads (reviewer)\n")
	}
	for _, f := range findings {
		loc := "(no file)"
		if f.F.Path != nil {
			loc = *f.F.Path
			if f.F.Line != nil {
				loc += fmt.Sprintf(":%d", *f.F.Line)
			}
		}
		fmt.Fprintf(&b, "\n### %s (score %d) %s\n%s\n\n%s\n", f.ID, f.F.Score, f.F.Title, loc, f.Body)
	}
	b.WriteString("\n## Comments\n\n" + ownerComment)
	writeFile(s.t, filepath.Join(s.ghDir, "change.txt"), b.String(), 0o644)
}

// input is the stage's input for workspace ws at head, as the worker
// writes it.
func (s *smoke) input(stage contract.Stage, ws, head string) contract.Input {
	identity := "coder"
	if stage == contract.StageReview {
		identity = "reviewer"
	}
	return contract.Input{
		ContractVersion: contract.Version,
		RunID:           "smoke-" + string(stage),
		Stage:           stage,
		Attempt:         1,
		Deadline:        time.Now().Add(stageLimit).UTC().Format(time.RFC3339),
		Identity:        identity,
		Project:         contract.InputProject{Name: "calc", Tracker: "github", CodeHost: "github"},
		Repo:            contract.InputRepo{URL: "file://" + s.remote, BaseBranch: "main"},
		Workspace:       contract.Workspace{Path: ws, Branch: branch, Head: head},
		Item:            contract.ItemRef{Ref: itemRef, URL: "file://" + filepath.Join(s.ghDir, "item.txt"), Type: "feature"},
	}
}

// change is the input's change once the implement head is pushed.
func (s *smoke) change(head string) *contract.ChangeRef {
	return &contract.ChangeRef{Ref: changeRef, URL: "file://" + filepath.Join(s.ghDir, "change.txt"), Head: head, BaseTip: s.base}
}

// stageRun is one finished stage run.
type stageRun struct {
	res contract.Result
	out string
	log string
}

// run writes in to input.json, runs "agent-loop-claude <stage>" with the
// handler environment (INTEGRATION-SPEC §3.1), and checks result.json with
// contract.ValidateResult.
func (s *smoke) run(in contract.Input) stageRun {
	t := s.t
	t.Helper()
	dir := filepath.Join(s.root, "run-"+string(in.Stage))
	out := filepath.Join(dir, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	inPath, resPath := filepath.Join(dir, "input.json"), filepath.Join(dir, "result.json")
	writeFile(t, inPath, string(data), 0o644)

	args := []string{string(in.Stage)}
	if s.unsandboxed {
		args = append(args, "--unsandboxed")
	}
	cmd := exec.Command(handlerBin, args...)
	cmd.Dir = in.Workspace.Path
	cache := filepath.Join(in.Workspace.Path, ".cache")
	env := []string{
		run.EnvInput + "=" + inPath,
		run.EnvResult + "=" + resPath,
		run.EnvOut + "=" + out,
		run.EnvContract + "=" + contract.Version,
		"PATH=" + s.ghDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"GOCACHE=" + filepath.Join(cache, "go-build"),
		"GOTMPDIR=" + filepath.Join(cache, "tmp"),
	}
	env = append(env, s.noGitHub()...)
	if err := os.MkdirAll(filepath.Join(cache, "tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if s.claudeBin != "" {
		env = append(env, claude.EnvBin+"="+s.claudeBin)
	}
	if s.settings != "" {
		env = append(env, claude.EnvSettings+"="+s.settings)
	}
	env = append(env, s.stageEnv[in.Stage]...)
	cmd.Env = append(handlerEnviron(), env...)
	var log bytes.Buffer
	cmd.Stdout, cmd.Stderr = &log, &log
	start := time.Now()
	runErr := cmd.Run()
	t.Logf("%s: %s, exit %v", in.Stage, time.Since(start).Round(time.Second), runErr)

	got, err := os.ReadFile(resPath)
	if err != nil {
		t.Fatalf("%s wrote no result (%v): %v\n%s", in.Stage, runErr, err, tail(log.String()))
	}
	res, err := contract.ValidateResult(in, got, out)
	if err != nil {
		t.Fatalf("%s result is invalid: %v\n%s\n%s", in.Stage, err, got, tail(log.String()))
	}
	return stageRun{res: res, out: out, log: log.String()}
}

// handlerEnviron is the test's environment without the variables the
// harness sets, so a stray one in the owner's shell can't change a run.
// It also drops every GH_* and GITHUB_* variable (GH_TOKEN, GITHUB_TOKEN,
// GH_ENTERPRISE_TOKEN, GH_HOST, ...) and git's own config locations, so
// no GitHub credential reaches the handler (work plan D8,
// INTEGRATION-SPEC §3.1).
func handlerEnviron() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "AGENT_LOOP_") || strings.HasPrefix(k, "FAKECLAUDE_") ||
			strings.HasPrefix(k, "GH_") || strings.HasPrefix(k, "GITHUB_") ||
			strings.HasPrefix(k, "GIT_CONFIG") ||
			k == "PATH" || k == "GOCACHE" || k == "GOTMPDIR" {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// noGitHub is the environment that keeps a real gh or git, if the agent
// runs one by its path, logged out (work plan D8): gh reads its login from
// an empty GH_CONFIG_DIR, and git reads no global or system config, so no
// credential helper hands it the owner's GitHub login. HOME stays, since
// the real claude keeps its own login there.
func (s *smoke) noGitHub() []string {
	s.t.Helper()
	cfg := filepath.Join(s.root, "gh-config")
	if err := os.MkdirAll(cfg, 0o700); err != nil {
		s.t.Fatal(err)
	}
	empty := filepath.Join(s.root, "gitconfig-empty")
	writeFile(s.t, empty, "", 0o644)
	return []string{
		"GH_CONFIG_DIR=" + cfg,
		"GIT_CONFIG_GLOBAL=" + empty,
		"GIT_CONFIG_NOSYSTEM=1",
	}
}

// tail is the end of a run log, enough to see why a stage went wrong.
func tail(log string) string {
	const max = 16 << 10
	if len(log) > max {
		return "...\n" + log[len(log)-max:]
	}
	return log
}

// mustDone fails the test unless the stage's result is done (work plan D5).
func mustDone(t *testing.T, r stageRun, stage contract.Stage) {
	t.Helper()
	if r.res.Result != contract.OutcomeDone {
		t.Fatalf("%s result = %s, want done: failed=%+v blocked=%+v needs_owner=%+v\n%s",
			stage, r.res.Result, r.res.Failed, r.res.Blocked, r.res.NeedsOwner, tail(r.log))
	}
}

func readOut(t *testing.T, out, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// flow runs implement, then review on the pushed head, then address on
// the findings, as the worker would (plan T9, work plan D2, D3, D5).
func (s *smoke) flow() {
	t := s.t
	t.Helper()

	// implement: a new agent branch from origin/main.
	ws := s.workspace("implement", "-b", branch, "origin/main")
	impl := s.run(s.input(contract.StageImplement, ws, s.base))
	mustDone(t, impl, contract.StageImplement)
	head := impl.res.Done.Implement.Head
	if head == s.base {
		t.Fatalf("implement head %s is the base: nothing was committed", head)
	}
	if rh := s.remoteHead(); rh != head {
		t.Fatalf("implement head %s is not on the bare remote (%s = %q)", head, branch, rh)
	}
	c := impl.res.Done.Implement.Change
	desc := c.Title + "\n\n" + readOut(t, impl.out, c.BodyFile)
	s.setChange(desc, nil)

	// review: detached at the implement head; nothing to rule on yet.
	ws = s.workspace("review", "--detach", head)
	in := s.input(contract.StageReview, ws, head)
	in.Change = s.change(head)
	round := 1
	in.Review = &contract.ReviewInput{Kind: contract.KindRound, Commit: head, Round: &round, BlockingThreshold: 4, RuleFindings: []string{}, RuleManual: []string{}}
	rev := s.run(in)
	mustDone(t, rev, contract.StageReview)
	if rh := s.remoteHead(); rh != head {
		t.Fatalf("the review moved %s from %s to %s", branch, head, rh)
	}
	var findings []finding
	ids := []string{}
	for i, f := range rev.res.Done.Review.NewFindings {
		id := fmt.Sprintf("F%d", i+1)
		ids = append(ids, id)
		findings = append(findings, finding{ID: id, F: f, Body: readOut(t, rev.out, f.BodyFile)})
	}
	t.Logf("review: %d findings", len(findings))
	s.setChange(desc, findings)

	// address: on the agent branch at the implement head.
	ws = s.workspace("address", "-b", branch, "origin/"+branch)
	in = s.input(contract.StageAddress, ws, head)
	in.Change = s.change(head)
	in.Address = &contract.AddressInput{Mode: contract.ModeFindings, AnswerFindings: ids, AnswerManual: []string{ownerID}}
	addr := s.run(in)
	mustDone(t, addr, contract.StageAddress)
	if got, rh := addr.res.Done.Address.Head, s.remoteHead(); got != rh {
		t.Fatalf("address head %s is not on the bare remote (%s = %q)", got, branch, rh)
	}
}

// ghCalls is what the agent asked the fake gh.
func (s *smoke) ghCalls() string {
	data, _ := os.ReadFile(filepath.Join(s.ghDir, "calls.log"))
	return string(data)
}

// sandboxSettings writes Claude Code settings that turn the sandbox on and
// make it required, the least agent-loop's local runner gives (plan D12,
// work plan D4).
func sandboxSettings(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "claude-settings.json")
	writeFile(t, p, `{"sandbox":{"enabled":true,"failIfUnavailable":true}}`+"\n", 0o644)
	return p
}

// command runs name with args and returns its combined output.
func command(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}
