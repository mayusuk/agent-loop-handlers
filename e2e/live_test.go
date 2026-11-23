// Package e2e holds agent-loop-claude's smoke test with the real Claude
// Code (plan T9, D15) and its dry run with the fake claude. See the README
// for how to run the live test.
package e2e

import (
	"os"
	"os/exec"
	"testing"

	"github.com/mayusuk/agent-loop-handlers/internal/claude"
)

// The live test's switches (plan D15, work plan D4).
const (
	// EnvLive turns the live test on.
	EnvLive = "AGENT_LOOP_LIVE"
	// EnvLiveUnsandboxed passes --unsandboxed instead of sandbox settings.
	EnvLiveUnsandboxed = "AGENT_LOOP_LIVE_UNSANDBOXED"
)

// TestPlanT9_LiveSmoke runs implement, then review on the pushed head,
// then address on the findings, with the real claude against a bare local
// remote. Each result must pass contract.ValidateResult and be done, and
// each pushed head must be on the remote (plan T9, work plan D5). It calls
// the real Claude Code and costs money, so it is skipped unless
// AGENT_LOOP_LIVE=1. It never calls GitHub (work plan D1).
func TestPlanT9_LiveSmoke(t *testing.T) {
	if os.Getenv(EnvLive) != "1" {
		t.Skipf("set %s=1 to run the live smoke test with the real claude (see the README)", EnvLive)
	}
	s := newSmoke(t)
	s.claudeBin = os.Getenv(claude.EnvBin)
	if s.claudeBin == "" {
		bin, err := exec.LookPath(claude.DefaultBin)
		if err != nil {
			t.Fatalf("%s=1 but claude is not on PATH and %s is not set: %v", EnvLive, claude.EnvBin, err)
		}
		s.claudeBin = bin
	}
	switch {
	case os.Getenv(EnvLiveUnsandboxed) == "1":
		s.unsandboxed = true
		t.Logf("running claude without the sandbox (%s=1)", EnvLiveUnsandboxed)
	case os.Getenv(claude.EnvSettings) != "":
		s.settings = os.Getenv(claude.EnvSettings)
	default:
		s.settings = sandboxSettings(t)
	}
	t.Logf("claude: %s; workspaces under %s", s.claudeBin, s.root)
	defer func() { t.Logf("gh calls:\n%s", s.ghCalls()) }()
	s.flow()
}
