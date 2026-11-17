package prompt

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mayusuk/agent-loop/contract"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// repo is a workspace clone of a bare remote whose main has one commit.
type repo struct {
	t      *testing.T
	remote string
	dir    string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	root := t.TempDir()
	r := &repo{t: t, remote: filepath.Join(root, "remote.git"), dir: filepath.Join(root, "ws")}
	r.run(root, "init", "-q", "--bare", "-b", "main", r.remote)
	r.run(root, "clone", "-q", r.remote, r.dir)
	r.git("checkout", "-q", "-b", "main")
	r.write("README.md", "hello\n")
	r.commit("init")
	r.git("push", "-q", "origin", "main")
	return r
}

func (r *repo) run(dir string, args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func (r *repo) git(args ...string) string { r.t.Helper(); return r.run(r.dir, args...) }

func (r *repo) write(path, data string) {
	r.t.Helper()
	p := filepath.Join(r.dir, path)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) commit(msg string) {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
}

// pushOverride commits a prompt file on main and pushes it, then puts the
// workspace on the item branch, as the worker does.
func (r *repo) pushOverride(stage contract.Stage, data string) {
	r.t.Helper()
	r.write(OverridePath(stage), data)
	r.commit("prompt")
	r.git("push", "-q", "origin", "main")
	r.git("fetch", "-q", "origin")
	r.git("checkout", "-q", "-B", "agent/1", "origin/main")
}

func input(stage contract.Stage) contract.Input {
	round := 2
	in := contract.Input{
		ContractVersion: "1.0",
		RunID:           "item/p/1#1:" + string(stage) + ":1:a",
		Stage:           stage,
		Attempt:         1,
		Deadline:        "2026-11-14T16:00:00Z",
		Identity:        "coder",
		Project:         contract.InputProject{Name: "p", Tracker: "github", CodeHost: "github"},
		Repo:            contract.InputRepo{URL: "https://github.com/o/r", BaseBranch: "main"},
		Workspace:       contract.Workspace{Path: "/ws/p-1-1", Branch: "agent/1", Head: "9f2c"},
		Item:            contract.ItemRef{Ref: "1", URL: "https://github.com/o/r/issues/1", Type: "feature"},
	}
	switch stage {
	case contract.StageReview:
		in.Identity = "reviewer"
		in.Change = &contract.ChangeRef{Ref: "57", URL: "https://github.com/o/r/pull/57", Head: "9f2c", BaseTip: "a1b2"}
		in.Review = &contract.ReviewInput{Kind: contract.KindRound, Commit: "9f2c", Round: &round,
			BlockingThreshold: 4, RuleFindings: []string{"F3", "F5"}, RuleManual: []string{"MC-981"}}
	case contract.StageAddress:
		in.Change = &contract.ChangeRef{Ref: "57", URL: "https://github.com/o/r/pull/57", Head: "9f2c", BaseTip: "a1b2"}
		in.Address = &contract.AddressInput{Mode: contract.ModeFindings, AnswerFindings: []string{"F3"}, AnswerManual: []string{}}
	}
	return in
}

func opts(r *repo, in contract.Input, log *[]string) Options {
	return Options{
		Input:     in,
		InputPath: "/run/input.json",
		DraftPath: "/run/tmp/agent-draft.json",
		Dir:       r.dir,
		Logf:      func(f string, a ...any) { *log = append(*log, fmt.Sprintf(f, a...)) },
	}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden.md")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update to create it)", err)
	}
	if got != string(want) {
		t.Errorf("prompt differs from %s (run go test -update to accept):\n%s", path, got)
	}
}

// TestI12_4_BuiltinPromptsGolden checks every built-in prompt, including
// its run facts and draft instructions, against a golden file.
func TestI12_4_BuiltinPromptsGolden(t *testing.T) {
	r := newRepo(t)
	baseUpdate := input(contract.StageAddress)
	baseUpdate.Address = &contract.AddressInput{Mode: contract.ModeBaseUpdate, AnswerFindings: []string{}, AnswerManual: []string{}}
	reviewBase := input(contract.StageReview)
	reviewBase.Review.Kind = contract.KindBaseUpdate
	reviewBase.Review.Round = nil
	reviewBase.Review.ResolvedPaths = []string{"api/login.go"}
	cases := map[string]contract.Input{
		"implement":           input(contract.StageImplement),
		"review":              input(contract.StageReview),
		"review-base_update":  reviewBase,
		"address":             input(contract.StageAddress),
		"address-base_update": baseUpdate,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			var log []string
			p, err := Build(context.Background(), opts(r, in, &log))
			if err != nil {
				t.Fatal(err)
			}
			if p.Source != SourceBuiltin {
				t.Errorf("source = %s, want builtin", p.Source)
			}
			for _, want := range []string{"## Rules for this run", "## Run facts", "## Draft",
				"/run/tmp/agent-draft.json", "/run/input.json", "Item: 1 (feature)"} {
				if !strings.Contains(p.Text, want) {
					t.Errorf("prompt lacks %q", want)
				}
			}
			if len(log) != 1 || log[0] != "prompt: built-in "+string(in.Stage)+" prompt" {
				t.Errorf("log = %q", log)
			}
			golden(t, name, p.Text)
		})
	}
}

func TestI12_4_OverrideOnBaseBranchWins(t *testing.T) {
	r := newRepo(t)
	r.pushOverride(contract.StageImplement, "\xef\xbb\xbf# Our implement prompt\n\nDo it our way.\n")
	var log []string
	p, err := Build(context.Background(), opts(r, input(contract.StageImplement), &log))
	if err != nil {
		t.Fatal(err)
	}
	if p.Source != SourceOverride || p.Path != ".agent-loop/prompts/implement.md" {
		t.Errorf("source = %s %s", p.Source, p.Path)
	}
	if !strings.HasPrefix(p.Text, "# Our implement prompt\n\nDo it our way.\n\n---\n\n## Rules for this run") {
		t.Errorf("prompt does not start with the override and the rules:\n%s", p.Text)
	}
	if strings.Contains(p.Text, "# Implement the work item") {
		t.Error("prompt still holds the built-in stage prompt")
	}
	for _, want := range []string{"## Run facts", "## Draft"} {
		if !strings.Contains(p.Text, want) {
			t.Errorf("override prompt lacks %q (D1)", want)
		}
	}
	if len(log) != 1 || log[0] != "prompt: .agent-loop/prompts/implement.md from origin/main" {
		t.Errorf("log = %q", log)
	}
}

// TestI12_4_OverrideOffBaseIsIgnored: a prompt file on the item branch, in
// the working tree, or on an unpushed local main is not read (§12.4).
func TestI12_4_OverrideOffBaseIsIgnored(t *testing.T) {
	cases := map[string]func(r *repo){
		"item branch": func(r *repo) {
			r.git("checkout", "-q", "-b", "agent/1")
			r.write(OverridePath(contract.StageImplement), "item branch prompt\n")
			r.commit("prompt on item branch")
			r.git("push", "-q", "origin", "agent/1")
		},
		"working tree": func(r *repo) {
			r.git("checkout", "-q", "-b", "agent/1")
			r.write(OverridePath(contract.StageImplement), "working tree prompt\n")
		},
		"unpushed local main": func(r *repo) {
			r.write(OverridePath(contract.StageImplement), "local main prompt\n")
			r.commit("prompt on local main")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRepo(t)
			setup(r)
			var log []string
			p, err := Build(context.Background(), opts(r, input(contract.StageImplement), &log))
			if err != nil {
				t.Fatal(err)
			}
			if p.Source != SourceBuiltin || strings.Contains(p.Text, " prompt\n\n---") {
				t.Errorf("source = %s; the off-base prompt file was used:\n%s", p.Source, p.Text)
			}
		})
	}
}

func TestI12_4_OverrideIsPerStage(t *testing.T) {
	r := newRepo(t)
	r.pushOverride(contract.StageReview, "our review prompt\n")
	var log []string
	p, err := Build(context.Background(), opts(r, input(contract.StageImplement), &log))
	if err != nil {
		t.Fatal(err)
	}
	if p.Source != SourceBuiltin {
		t.Errorf("implement used the review override")
	}
}

// TestD6_SkillWinsAndIsLogged: --skill beats a prompt file on the base
// branch, and the log says the file was ignored (plan D6).
func TestD6_SkillWinsAndIsLogged(t *testing.T) {
	r := newRepo(t)
	r.pushOverride(contract.StageImplement, "our implement prompt\n")
	var log []string
	o := opts(r, input(contract.StageImplement), &log)
	o.Skill = "work-issue"
	p, err := Build(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if p.Source != SourceSkill {
		t.Errorf("source = %s, want skill", p.Source)
	}
	if strings.Contains(p.Text, "our implement prompt") {
		t.Error("the prompt file was used despite --skill")
	}
	for _, want := range []string{"`/work-issue`", "result-only mode", "do not post to GitHub", "## Run facts", "## Draft"} {
		if !strings.Contains(p.Text, want) {
			t.Errorf("skill prompt lacks %q", want)
		}
	}
	want := []string{
		"prompt: .agent-loop/prompts/implement.md on origin/main is ignored: --skill work-issue wins",
		"prompt: skill work-issue",
	}
	if strings.Join(log, "\n") != strings.Join(want, "\n") {
		t.Errorf("log = %q, want %q", log, want)
	}
	golden(t, "skill-implement", p.Text)
}

func TestD6_SkillWithoutPromptFileLogsNoIgnore(t *testing.T) {
	r := newRepo(t)
	var log []string
	o := opts(r, input(contract.StageReview), &log)
	o.Skill = "review-pr"
	p, err := Build(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Text, "for the change named") {
		t.Errorf("review skill prompt should name the change:\n%s", p.Text)
	}
	if len(log) != 1 || log[0] != "prompt: skill review-pr" {
		t.Errorf("log = %q", log)
	}
}

func TestD5_SkillIgnoresAnInvalidPromptFile(t *testing.T) {
	r := newRepo(t)
	r.pushOverride(contract.StageImplement, strings.Repeat("x", MaxOverride+1))
	var log []string
	o := opts(r, input(contract.StageImplement), &log)
	o.Skill = "work-issue"
	if _, err := Build(context.Background(), o); err != nil {
		t.Errorf("err = %v, want the ignored file not to matter", err)
	}
}

// TestD5_BadPromptFilesFail: an oversized, non-UTF-8, empty or
// non-regular prompt file is an error, which the stage turns into failed.
func TestD5_BadPromptFilesFail(t *testing.T) {
	cases := map[string]struct {
		data string
		want string
	}{
		"over 64 KiB": {strings.Repeat("x", MaxOverride+1), "is 65537 bytes, over the 65536-byte limit"},
		"not UTF-8":   {"# prompt \xff\xfe\n", "is not UTF-8"},
		"empty":       {" \n\t\n", "is empty"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRepo(t)
			r.pushOverride(contract.StageImplement, c.data)
			var log []string
			_, err := Build(context.Background(), opts(r, input(contract.StageImplement), &log))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if !strings.Contains(err.Error(), ".agent-loop/prompts/implement.md on origin/main") {
				t.Errorf("reason does not name the file: %v", err)
			}
		})
	}
}

func TestD5_PromptFileAtTheLimitIsUsed(t *testing.T) {
	r := newRepo(t)
	r.pushOverride(contract.StageImplement, strings.Repeat("x", MaxOverride))
	var log []string
	p, err := Build(context.Background(), opts(r, input(contract.StageImplement), &log))
	if err != nil {
		t.Fatal(err)
	}
	if p.Source != SourceOverride {
		t.Errorf("source = %s", p.Source)
	}
}

// pushSymlinkOverride puts the implement prompt file on the base branch as
// a symlink to a real file.
func (r *repo) pushSymlinkOverride() {
	r.t.Helper()
	r.write("prompts/real.md", "real prompt\n")
	if err := os.MkdirAll(filepath.Join(r.dir, OverrideDir), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.Symlink("../../prompts/real.md", filepath.Join(r.dir, OverridePath(contract.StageImplement))); err != nil {
		r.t.Fatal(err)
	}
	r.commit("symlink prompt")
	r.git("push", "-q", "origin", "main")
}

// TestD5_SkillIgnoresANonRegularPromptFile: with --skill, a prompt file on
// the base that is a symlink or a directory is ignored and logged, not a
// failure (plan D5, D6).
func TestD5_SkillIgnoresANonRegularPromptFile(t *testing.T) {
	cases := map[string]func(r *repo){
		"symlink":   func(r *repo) { r.pushSymlinkOverride() },
		"directory": func(r *repo) { r.pushOverride(contract.Stage("implement.md/x"), "nested\n") },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRepo(t)
			setup(r)
			var log []string
			o := opts(r, input(contract.StageImplement), &log)
			o.Skill = "work-issue"
			p, err := Build(context.Background(), o)
			if err != nil {
				t.Fatalf("err = %v, want the ignored file not to matter", err)
			}
			if p.Source != SourceSkill {
				t.Errorf("source = %s, want skill", p.Source)
			}
			want := []string{
				"prompt: .agent-loop/prompts/implement.md on origin/main is ignored: --skill work-issue wins",
				"prompt: skill work-issue",
			}
			if strings.Join(log, "\n") != strings.Join(want, "\n") {
				t.Errorf("log = %q, want %q", log, want)
			}
		})
	}
}

// TestD5_SkillStillFailsWithoutTheBaseBranch: --skill does not hide a base
// branch that can't be read (plan D3).
func TestD5_SkillStillFailsWithoutTheBaseBranch(t *testing.T) {
	r := newRepo(t)
	in := input(contract.StageImplement)
	in.Repo.BaseBranch = "develop"
	var log []string
	o := opts(r, in, &log)
	o.Skill = "work-issue"
	_, err := Build(context.Background(), o)
	if err == nil || !strings.Contains(err.Error(), "cannot read the base branch origin/develop") {
		t.Fatalf("err = %v", err)
	}
}

func TestD3_SymlinkPromptFileFails(t *testing.T) {
	r := newRepo(t)
	r.pushSymlinkOverride()
	var log []string
	_, err := Build(context.Background(), opts(r, input(contract.StageImplement), &log))
	if err == nil || !strings.Contains(err.Error(), "is not a regular file") {
		t.Fatalf("err = %v", err)
	}
}

func TestD3_DirectoryPromptPathFails(t *testing.T) {
	r := newRepo(t)
	r.pushOverride(contract.Stage("implement.md/x"), "nested\n")
	var log []string
	_, err := Build(context.Background(), opts(r, input(contract.StageImplement), &log))
	if err == nil || !strings.Contains(err.Error(), "is not a regular file") {
		t.Fatalf("err = %v", err)
	}
}

func TestD3_MissingBaseRefFails(t *testing.T) {
	r := newRepo(t)
	in := input(contract.StageImplement)
	in.Repo.BaseBranch = "develop"
	var log []string
	_, err := Build(context.Background(), opts(r, in, &log))
	if err == nil || !strings.Contains(err.Error(), "cannot read the base branch origin/develop") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildRejectsMintAndInvalidSkill(t *testing.T) {
	r := newRepo(t)
	var log []string
	if _, err := Build(context.Background(), opts(r, input(contract.StageMint), &log)); err == nil {
		t.Error("mint has no prompt in this release")
	}
	o := opts(r, input(contract.StageImplement), &log)
	o.Skill = "-rf"
	if _, err := Build(context.Background(), o); err == nil {
		t.Error("an invalid skill name was accepted")
	}
}

func TestD7_ParseFlags(t *testing.T) {
	ok := map[string]struct {
		args []string
		want string
	}{
		"none":         {nil, ""},
		"separate":     {[]string{"--skill", "work-issue"}, "work-issue"},
		"equals":       {[]string{"--skill=review-pr"}, "review-pr"},
		"plugin skill": {[]string{"--skill", "my-plugin:address-pr"}, "my-plugin:address-pr"},
	}
	for name, c := range ok {
		f, err := ParseFlags(c.args)
		if err != nil || f.Skill != c.want {
			t.Errorf("%s: got %q, %v; want %q", name, f.Skill, err, c.want)
		}
	}
	bad := map[string]struct {
		args []string
		want string
	}{
		"unknown":    {[]string{"--model", "x"}, `unknown argument "--model"`},
		"positional": {[]string{"x"}, `unknown argument "x"`},
		"no name":    {[]string{"--skill"}, "--skill needs a skill name"},
		"empty name": {[]string{"--skill="}, `invalid skill name ""`},
		"flag name":  {[]string{"--skill", "--x"}, `invalid skill name "--x"`},
		"space":      {[]string{"--skill", "a b"}, `invalid skill name "a b"`},
		"twice":      {[]string{"--skill", "a", "--skill", "b"}, "--skill is given more than once"},
	}
	for name, c := range bad {
		_, err := ParseFlags(c.args)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}
}

// TestD1_CommitRuleDependsOnTheStage: the rules tell implement and address
// to commit on the branch, and review to change and commit nothing, so a
// review skill that sees only the rules is not told to commit (plan D1, D4).
func TestD1_CommitRuleDependsOnTheStage(t *testing.T) {
	const commit = "Commit your work on the checked-out branch"
	const noCommit = "Do not change, commit or push anything"
	cases := map[contract.Stage]bool{
		contract.StageImplement: true,
		contract.StageAddress:   true,
		contract.StageReview:    false,
	}
	for stage, commits := range cases {
		t.Run(string(stage), func(t *testing.T) {
			tail, err := Tail(Options{Input: input(stage), InputPath: "/run/input.json", DraftPath: "/run/tmp/agent-draft.json"})
			if err != nil {
				t.Fatal(err)
			}
			rules, _, _ := strings.Cut(tail, Separator)
			if got := strings.Contains(rules, commit); got != commits {
				t.Errorf("rules contain %q = %v, want %v:\n%s", commit, got, commits, rules)
			}
			if got := strings.Contains(rules, noCommit); got == commits {
				t.Errorf("rules contain %q = %v, want %v:\n%s", noCommit, got, !commits, rules)
			}
			if !strings.Contains(rules, "never push") && !strings.Contains(rules, "commit or push anything") {
				t.Errorf("rules do not forbid pushing:\n%s", rules)
			}
		})
	}
}
