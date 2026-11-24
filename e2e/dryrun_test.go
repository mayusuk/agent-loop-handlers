package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mayusuk/agent-loop/contract"
)

// The fake claude's steps for each stage of the dry run. Each one also
// checks that the fake gh serves what the stage's prompt tells the agent
// to read, and refuses anything else (work plan D1).
const (
	dryImplement = `gh issue view 1 --comments | grep -q 'func Sub' &&
! gh api repos/o/r/pulls 2>/dev/null &&
printf 'package calc\n\nfunc Sub(a, b int) int { return a - b }\n' > sub.go &&
git add sub.go && git commit -qm 'Add Sub' &&
printf '%s' '{"result":"done","done":{"change":{"title":"Add Sub","body":"Adds Sub."}}}' > "$AGENT_LOOP_CLAUDE_DRAFT"`

	dryReview = `gh pr view 2 --comments | grep -q 'Adds Sub.' &&
printf '%s' '{"result":"done","done":{"summary":"One issue.","new_findings":[{"score":3,"path":"sub.go","line":3,"title":"Sub has no doc comment","body":"Add one."}]}}' > "$AGENT_LOOP_CLAUDE_DRAFT"`

	dryAddress = `gh pr view 2 --comments | grep -q 'F1 (score 3) Sub has no doc comment' &&
gh pr view 2 | grep -q 'MC-1' &&
printf 'package calc\n\n// Sub returns a - b.\nfunc Sub(a, b int) int { return a - b }\n' > sub.go &&
git commit -qam 'Document Sub' &&
printf '%s' '{"result":"done","done":{"replies":[{"finding":"F1","kind":"fixed","body":"Added."}],"manual_replies":[{"comment":"MC-1","kind":"fixed","body":"Added."}]}}' > "$AGENT_LOOP_CLAUDE_DRAFT"`
)

// TestPlanT9_HarnessDryRun runs the live smoke test's flow with the fake
// claude, so the harness is tested in CI where the live test is skipped
// (work plan D7).
func TestPlanT9_HarnessDryRun(t *testing.T) {
	s := newSmoke(t)
	s.claudeBin = fakeClaude
	s.sandbox = true
	s.stageEnv[contract.StageImplement] = []string{"FAKECLAUDE_SHELL=" + dryImplement}
	s.stageEnv[contract.StageReview] = []string{"FAKECLAUDE_SHELL=" + dryReview}
	s.stageEnv[contract.StageAddress] = []string{"FAKECLAUDE_SHELL=" + dryAddress}
	s.flow()

	calls := s.ghCalls()
	for _, want := range []string{"issue view 1 --comments", "api repos/o/r/pulls", "pr view 2 --comments"} {
		if !strings.Contains(calls, want) {
			t.Errorf("gh calls lack %q:\n%s", want, calls)
		}
	}
	if got := git(t, s.remote, "log", "--format=%s", "main.."+branch); got != "Document Sub\nAdd Sub" {
		t.Errorf("%s commits = %q", branch, got)
	}
}

// TestPlanT9_GhRefusesOtherCalls: the fake gh fails every call but the
// item and the change, so an agent can't reach GitHub through it (work
// plan D1).
func TestPlanT9_GhRefusesOtherCalls(t *testing.T) {
	s := newSmoke(t)
	gh := s.ghDir + "/gh"
	for _, args := range [][]string{
		{"issue", "view", "2"},
		{"pr", "view", "1"},
		{"pr", "comment", "2", "--body", "x"},
		{"api", "graphql"},
		{"auth", "status"},
		{},
	} {
		out, err := command(gh, args...)
		if err == nil {
			t.Errorf("gh %v succeeded: %s", args, out)
		} else if !strings.Contains(out, "offline, there is no GitHub here") {
			t.Errorf("gh %v: %q", args, out)
		}
	}
	if out, err := command(gh, "issue", "view", "1", "--comments"); err != nil || !strings.Contains(out, "func Sub") {
		t.Errorf("gh issue view 1 = %q, %v", out, err)
	}
	s.setChange("Adds Sub.", []finding{{ID: "F1", F: contract.NewFinding{Score: 4, Title: "T"}, Body: "B"}})
	out, err := command(gh, "pr", "view", "2", "--json", "body")
	if err != nil {
		t.Fatalf("gh pr view 2: %v", err)
	}
	for _, want := range []string{"Adds Sub.", "### F1 (score 4) T\n(no file)\n\nB", ownerID} {
		if !strings.Contains(out, want) {
			t.Errorf("gh pr view 2 lacks %q:\n%s", want, out)
		}
	}
}

// TestPlanT9_HandlerGetsNoGitHubCredentials: the owner's GitHub tokens,
// gh login, git config, ~/.netrc, ssh agent and ~/.ssh never reach the
// agent, and the real gh is on no directory of its PATH (work plan D8,
// INTEGRATION-SPEC §3.1).
func TestPlanT9_HandlerGetsNoGitHubCredentials(t *testing.T) {
	ownerGh := t.TempDir()
	writeFile(t, filepath.Join(ownerGh, "hosts.yml"), "github.com:\n  oauth_token: owner-secret\n", 0o600)
	ownerGit := filepath.Join(t.TempDir(), "gitconfig")
	writeFile(t, ownerGit, "[credential]\n\thelper = owner-secret\n", 0o644)
	// The owner's home has a netrc, an ssh key and the claude config.
	ownerHome := t.TempDir()
	writeFile(t, filepath.Join(ownerHome, ".netrc"), "machine github.com login owner password owner-secret\n", 0o600)
	writeFile(t, filepath.Join(ownerHome, ".ssh", "id_ed25519"), "owner-secret\n", 0o600)
	writeFile(t, filepath.Join(ownerHome, ".gitconfig"), "[credential]\n\thelper = owner-secret\n", 0o644)
	writeFile(t, filepath.Join(ownerHome, ".claude.json"), "{}\n", 0o600)
	// The real gh shares a directory with another tool the agent may need.
	realBin := t.TempDir()
	writeFile(t, filepath.Join(realBin, "gh"), "#!/bin/sh\necho real-gh owner-secret\n", 0o755)
	writeFile(t, filepath.Join(realBin, "owner-tool"), "#!/bin/sh\necho owner-tool runs\n", 0o755)
	for k, v := range map[string]string{
		"GH_TOKEN": "owner-secret", "GITHUB_TOKEN": "owner-secret",
		"GH_ENTERPRISE_TOKEN": "owner-secret", "GITHUB_ENTERPRISE_TOKEN": "owner-secret",
		"GH_HOST": "github.com", "GH_CONFIG_DIR": ownerGh, "GIT_CONFIG_GLOBAL": ownerGit,
		"SSH_AUTH_SOCK": filepath.Join(ownerHome, "agent.sock"), "HOME": ownerHome,
		"PATH": realBin + string(os.PathListSeparator) + os.Getenv("PATH"),
	} {
		t.Setenv(k, v)
	}

	s := newSmoke(t)
	s.claudeBin = fakeClaude
	s.sandbox = true
	dump := filepath.Join(s.root, "agent-env.txt")
	s.stageEnv[contract.StageImplement] = []string{"FAKECLAUDE_SHELL=" + `{ env
echo "gh-config: $(ls -A "$GH_CONFIG_DIR")"
echo "gh-path: $(command -v gh)"
(IFS=:; for d in $PATH; do [ -e "$d/gh" ] && echo "gh-on-path: $d/gh"; done)
echo "owner-tool: $(owner-tool)"
echo "git-helper: $(git config --get credential.helper)"
[ -r ~/.netrc ] && echo "netrc: readable"
[ -e ~/.ssh ] && echo "ssh-dir: present"
[ -e ~/.claude.json ] && echo "claude-config: linked"
git ls-remote https://github.invalid/owner/repo 2>&1 | grep -q "transport 'https' not allowed" && echo "git-https: refused"
git ls-remote git@github.invalid:owner/repo 2>&1 | grep -q "transport 'ssh' not allowed" && echo "git-ssh: refused"
$GIT_SSH_COMMAND git@github.invalid true 2>&1 | grep -q "ssh (agent-loop-claude smoke test): refused" && echo "ssh-command: refused"
true
} > '` + dump + `' &&
printf 'package calc\n' > sub.go && git add sub.go && git commit -qm 'Add Sub' &&
printf '%s' '{"result":"done","done":{"change":{"title":"Add Sub","body":"Adds Sub."}}}' > "$AGENT_LOOP_CLAUDE_DRAFT"`}
	ws := s.workspace("implement", "-b", branch, "origin/main")
	mustDone(t, s.run(s.input(contract.StageImplement, ws, s.base)), contract.StageImplement)

	data, err := os.ReadFile(dump)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "owner-secret") || strings.Contains(got, ownerGh) || strings.Contains(got, ownerGit) ||
		strings.Contains(got, ownerHome) || strings.Contains(got, realBin) {
		t.Errorf("the owner's GitHub credentials reached the agent:\n%s", got)
	}
	for _, k := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_HOST", "SSH_AUTH_SOCK"} {
		if strings.Contains("\n"+got, "\n"+k+"=") {
			t.Errorf("%s reached the agent", k)
		}
	}
	if n := strings.Count("\n"+got, "\ngh-on-path: "); n != 1 {
		t.Errorf("agent PATH has %d gh, want only the fake one:\n%s", n, got)
	}
	for _, bad := range []string{"\nnetrc: readable\n", "\nssh-dir: present\n"} {
		if strings.Contains("\n"+got, bad) {
			t.Errorf("agent environment has %q:\n%s", strings.TrimSpace(bad), got)
		}
	}
	for _, want := range []string{
		"\nHOME=" + filepath.Join(s.root, "home") + "\n",
		"\nGH_CONFIG_DIR=" + filepath.Join(s.root, "gh-config") + "\n",
		"\ngh-config: \n",
		"\ngh-path: " + filepath.Join(s.ghDir, "gh") + "\n",
		"\ngh-on-path: " + filepath.Join(s.ghDir, "gh") + "\n",
		"\nowner-tool: owner-tool runs\n",
		"\ngit-helper: \n",
		"\nclaude-config: linked\n",
		"\nGIT_CONFIG_NOSYSTEM=1\n",
		"\ngit-https: refused\n",
		"\ngit-ssh: refused\n",
		"\nssh-command: refused\n",
	} {
		if !strings.Contains("\n"+got, want) {
			t.Errorf("agent environment lacks %q:\n%s", strings.TrimSpace(want), got)
		}
	}
}

// TestT7_2_EachStageGetsItsOwnTempDir: every stage runs with TMPDIR set to
// its own run-<stage>/tmp, which exists and is in that stage's allowWrite
// (TECHNICAL-SPEC §7.2 "per-run temp directory", work plan 38 D3, issue
// #43 R3).
func TestT7_2_EachStageGetsItsOwnTempDir(t *testing.T) {
	s := newSmoke(t)
	s.claudeBin = fakeClaude
	s.sandbox = true
	record := `[ -d "$TMPDIR" ] && printf '%s' "$TMPDIR" > "$TMPDIR/seen" && `
	s.stageEnv[contract.StageImplement] = []string{"FAKECLAUDE_SHELL=" + record + dryImplement}
	s.stageEnv[contract.StageReview] = []string{"FAKECLAUDE_SHELL=" + record + dryReview}
	s.stageEnv[contract.StageAddress] = []string{"FAKECLAUDE_SHELL=" + record + dryAddress}
	s.flow()

	seen := map[string]contract.Stage{}
	for _, st := range []contract.Stage{contract.StageImplement, contract.StageReview, contract.StageAddress} {
		dir := filepath.Join(s.root, "run-"+string(st))
		want := filepath.Join(dir, "tmp")
		data, err := os.ReadFile(filepath.Join(want, "seen"))
		if err != nil {
			t.Fatalf("%s: TMPDIR not recorded in its own temp directory: %v", st, err)
		}
		got := string(data)
		if got != want {
			t.Errorf("%s: TMPDIR = %q, want %q", st, got, want)
		}
		if prev, ok := seen[got]; ok {
			t.Errorf("%s and %s share TMPDIR %q", prev, st, got)
		}
		seen[got] = st
		settings, err := os.ReadFile(filepath.Join(dir, "claude-settings.json"))
		if err != nil {
			t.Fatal(err)
		}
		var cs claudeSettings
		if err := json.Unmarshal(settings, &cs); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(cs.Sandbox.Filesystem.AllowWrite, resolve(want)) {
			t.Errorf("%s: allowWrite %q lacks its TMPDIR %s", st, cs.Sandbox.Filesystem.AllowWrite, resolve(want))
		}
	}
}
