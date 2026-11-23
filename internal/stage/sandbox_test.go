package stage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mayusuk/agent-loop-handlers/internal/claude"
	"github.com/mayusuk/agent-loop-handlers/internal/prompt"
	"github.com/mayusuk/agent-loop/contract"
)

// recordedArgs returns the arguments the fake claude recorded at rec.
func recordedArgs(t *testing.T, rec string) []string {
	t.Helper()
	data, err := os.ReadFile(rec)
	if err != nil {
		t.Fatalf("claude did not run: %v", err)
	}
	var r struct {
		Args []string `json:"args"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	return r.Args
}

// settingsArg returns the value after --settings, and whether it is there.
func settingsArg(args []string) (string, bool) {
	i := slices.Index(args, "--settings")
	if i < 0 || i+1 >= len(args) {
		return "", false
	}
	return args[i+1], true
}

// TestPlanT8_SettingsArePassedToClaude: with AGENT_LOOP_CLAUDE_SETTINGS
// set, claude gets --settings <path>, for implement and review (plan T8,
// D12).
func TestPlanT8_SettingsArePassedToClaude(t *testing.T) {
	f := newFixture(t, false)
	settings := sandboxSettings(t)
	rec := filepath.Join(t.TempDir(), "rec.json")
	o := f.implement(commitAndDraft, doneDraft, "FAKECLAUDE_RECORD", rec, claude.EnvSettings, settings)
	if o.res.Result != contract.OutcomeDone {
		t.Fatalf("result = %s: %+v\n%s", o.res.Result, o.res.Failed, o.stderr)
	}
	if got, ok := settingsArg(recordedArgs(t, rec)); !ok || got != settings {
		t.Errorf("--settings = %q (%v), want %q", got, ok, settings)
	}
	if strings.Contains(o.stderr, claude.UnsandboxedWarning) {
		t.Errorf("a sandboxed run logged the unsandboxed warning")
	}

	f = newFixture(t, false)
	rec = filepath.Join(t.TempDir(), "rec.json")
	o = f.review(draftOnly, reviewDraft(``), "FAKECLAUDE_RECORD", rec, claude.EnvSettings, settings)
	if o.res.Result != contract.OutcomeDone {
		t.Fatalf("review result = %s: %+v\n%s", o.res.Result, o.res.Failed, o.stderr)
	}
	if got, ok := settingsArg(recordedArgs(t, rec)); !ok || got != settings {
		t.Errorf("review --settings = %q (%v), want %q", got, ok, settings)
	}
}

// TestPlanT8_NoSettingsIsFailedAndClaudeNeverStarts: without the settings
// variable the run is failed and claude never runs (plan T8, TECHNICAL-SPEC
// §7.2, work plan #9 D2).
func TestPlanT8_NoSettingsIsFailedAndClaudeNeverStarts(t *testing.T) {
	f := newFixture(t, false)
	rec := filepath.Join(t.TempDir(), "rec.json")
	o := f.implement(commitAndDraft, doneDraft, "FAKECLAUDE_RECORD", rec, claude.EnvSettings, "")
	if r := failedReason(t, o); !strings.HasPrefix(r, "no sandbox settings") {
		t.Errorf("reason = %q", r)
	}
	if _, err := os.Stat(rec); !os.IsNotExist(err) {
		t.Errorf("claude ran: %v", err)
	}
	if h := f.remoteHead(); h != "" {
		t.Errorf("remote branch = %s, want none", h)
	}
}

// TestPlanT8_UnsandboxedRunsAndWarns: with --unsandboxed and no settings,
// claude runs without --settings and the run log gets the warning (plan
// T8, work plan #9 D3, D4).
func TestPlanT8_UnsandboxedRunsAndWarns(t *testing.T) {
	f := newFixture(t, false)
	f.flags = prompt.Flags{Unsandboxed: true}
	rec := filepath.Join(t.TempDir(), "rec.json")
	o := f.implement(commitAndDraft, doneDraft, "FAKECLAUDE_RECORD", rec, claude.EnvSettings, "")
	if o.res.Result != contract.OutcomeDone {
		t.Fatalf("result = %s: %+v\n%s", o.res.Result, o.res.Failed, o.stderr)
	}
	if _, ok := settingsArg(recordedArgs(t, rec)); ok {
		t.Errorf("an unsandboxed run got --settings")
	}
	if !strings.Contains(o.stderr, claude.UnsandboxedWarning) {
		t.Errorf("no warning in the run log:\n%s", o.stderr)
	}
}

// TestPlanT8_UnsandboxedStillUsesGivenSettings: --unsandboxed doesn't drop
// settings the runner gave (work plan #9 D3).
func TestPlanT8_UnsandboxedStillUsesGivenSettings(t *testing.T) {
	f := newFixture(t, false)
	f.flags = prompt.Flags{Unsandboxed: true}
	settings := sandboxSettings(t)
	rec := filepath.Join(t.TempDir(), "rec.json")
	o := f.implement(commitAndDraft, doneDraft, "FAKECLAUDE_RECORD", rec, claude.EnvSettings, settings)
	if o.res.Result != contract.OutcomeDone {
		t.Fatalf("result = %s: %+v\n%s", o.res.Result, o.res.Failed, o.stderr)
	}
	if got, _ := settingsArg(recordedArgs(t, rec)); got != settings {
		t.Errorf("--settings = %q, want %q", got, settings)
	}
	if strings.Contains(o.stderr, claude.UnsandboxedWarning) {
		t.Errorf("a sandboxed run logged the unsandboxed warning")
	}
}

// TestPlanT8_SettingsWithoutSandboxAreFailed: a settings file that doesn't
// turn the sandbox on fails the run before claude starts, even with
// --unsandboxed (plan T8, work plan #9 D1, D3).
func TestPlanT8_SettingsWithoutSandboxAreFailed(t *testing.T) {
	for name, data := range map[string]string{
		"disabled":      `{"sandbox":{"enabled":false,"failIfUnavailable":true}}`,
		"no sandbox":    `{"permissions":{}}`,
		"may fall back": `{"sandbox":{"enabled":true}}`,
		"not json":      `nope`,
	} {
		for _, unsandboxed := range []bool{false, true} {
			f := newFixture(t, false)
			f.flags = prompt.Flags{Unsandboxed: unsandboxed}
			p := filepath.Join(t.TempDir(), "settings.json")
			os.WriteFile(p, []byte(data), 0o644)
			rec := filepath.Join(t.TempDir(), "rec.json")
			o := f.implement(commitAndDraft, doneDraft, "FAKECLAUDE_RECORD", rec, claude.EnvSettings, p)
			if r := failedReason(t, o); !strings.HasPrefix(r, "sandbox settings "+p+": ") {
				t.Errorf("%s (unsandboxed %v): reason = %q", name, unsandboxed, r)
			}
			if _, err := os.Stat(rec); !os.IsNotExist(err) {
				t.Errorf("%s (unsandboxed %v): claude ran", name, unsandboxed)
			}
		}
	}
}
