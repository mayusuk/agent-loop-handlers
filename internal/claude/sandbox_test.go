package claude

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodSettings = `{"sandbox":{"enabled":true,"failIfUnavailable":true,"allowUnsandboxedCommands":false},"permissions":{}}`

func writeFile(t *testing.T, dir, name, data string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestPlanT8_CheckSettings: only a file that turns the sandbox on and
// makes it required passes (work plan #9 D1).
func TestPlanT8_CheckSettings(t *testing.T) {
	dir := t.TempDir()
	if err := CheckSettings(writeFile(t, dir, "ok.json", goodSettings)); err != nil {
		t.Errorf("good settings: %v", err)
	}
	bad := map[string]string{
		"empty":           ``,
		"array":           `[]`,
		"not json":        `sandbox = true`,
		"truncated":       `{"sandbox":{"enabled":true`,
		"no sandbox":      `{"permissions":{}}`,
		"sandbox null":    `{"sandbox":null}`,
		"disabled":        `{"sandbox":{"enabled":false,"failIfUnavailable":true}}`,
		"enabled string":  `{"sandbox":{"enabled":"true","failIfUnavailable":true}}`,
		"no fail flag":    `{"sandbox":{"enabled":true}}`,
		"may fall back":   `{"sandbox":{"enabled":true,"failIfUnavailable":false}}`,
		"no enabled flag": `{"sandbox":{"failIfUnavailable":true}}`,
		// Commands may retry outside the sandbox (#35 D1).
		"unsandboxed commands":         `{"sandbox":{"enabled":true,"failIfUnavailable":true,"allowUnsandboxedCommands":true}}`,
		"unsandboxed commands default": `{"sandbox":{"enabled":true,"failIfUnavailable":true}}`,
		"unsandboxed commands string":  `{"sandbox":{"enabled":true,"failIfUnavailable":true,"allowUnsandboxedCommands":"false"}}`,
		"unsandboxed commands null":    `{"sandbox":{"enabled":true,"failIfUnavailable":true,"allowUnsandboxedCommands":null}}`,
	}
	for name, data := range bad {
		p := writeFile(t, dir, strings.ReplaceAll(name, " ", "-")+".json", data)
		err := CheckSettings(p)
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		if !strings.HasPrefix(err.Error(), "sandbox settings "+p+": ") {
			t.Errorf("%s: error = %q", name, err)
		}
	}
	if err := CheckSettings(filepath.Join(dir, "missing.json")); err == nil || !strings.Contains(err.Error(), "cannot read") {
		t.Errorf("missing file: %v", err)
	}
}

// TestPlanT8_CheckSettingsRefusesUnsandboxedCommands: a file that lets
// commands retry outside the sandbox, explicitly or by Claude Code's
// default, is refused with its own reason (#35 D1).
func TestPlanT8_CheckSettingsRefusesUnsandboxedCommands(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string]string{
		"true":   `{"sandbox":{"enabled":true,"failIfUnavailable":true,"allowUnsandboxedCommands":true}}`,
		"absent": `{"sandbox":{"enabled":true,"failIfUnavailable":true}}`,
	} {
		p := writeFile(t, dir, name+".json", data)
		want := "sandbox settings " + p + ": sandbox.allowUnsandboxedCommands is not false"
		if err := CheckSettings(p); err == nil || err.Error() != want {
			t.Errorf("%s: error = %v, want %q", name, err, want)
		}
	}
}

// TestPlanT8_Sandbox: settings are required unless unsandboxed; given
// settings win over unsandboxed and are made absolute (work plan #9 D2,
// D3, D5).
func TestPlanT8_Sandbox(t *testing.T) {
	dir := t.TempDir()
	good := writeFile(t, dir, "ok.json", goodSettings)
	bad := writeFile(t, dir, "bad.json", `{"sandbox":{"enabled":false}}`)

	if _, err := Sandbox("", false); !errors.Is(err, ErrNoSandbox) {
		t.Errorf("no settings: err = %v", err)
	}
	if !strings.HasPrefix(ErrNoSandbox.Error(), "no sandbox settings") {
		t.Errorf("reason = %q", ErrNoSandbox)
	}
	if p, err := Sandbox("", true); p != "" || err != nil {
		t.Errorf("unsandboxed: %q, %v", p, err)
	}
	for _, unsandboxed := range []bool{false, true} {
		if p, err := Sandbox(good, unsandboxed); p != good || err != nil {
			t.Errorf("good (unsandboxed %v): %q, %v", unsandboxed, p, err)
		}
		if _, err := Sandbox(bad, unsandboxed); err == nil {
			t.Errorf("bad (unsandboxed %v): no error", unsandboxed)
		}
	}

	t.Chdir(dir)
	p, err := Sandbox("ok.json", false)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(p) || filepath.Base(p) != "ok.json" {
		t.Errorf("relative path gave %q", p)
	}
}
