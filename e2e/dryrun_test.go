package e2e

import (
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
	s.settings = sandboxSettings(t)
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
