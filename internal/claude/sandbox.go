package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNoSandbox is returned when AGENT_LOOP_CLAUDE_SETTINGS is not set and
// --unsandboxed was not given (plan T8, D12, TECHNICAL-SPEC §7.2).
var ErrNoSandbox = errors.New(`no sandbox settings: ` + EnvSettings + ` is not set (pass --unsandboxed only for runner = "local-unsandboxed")`)

// UnsandboxedWarning is logged when claude runs without the sandbox
// (work plan #9 D4, TECHNICAL-SPEC §7.2).
const UnsandboxedWarning = "warning: running claude without the sandbox (--unsandboxed): it has the owner's full user access: it can read the owner's files, reach any network address, and open the local API socket."

// Sandbox returns the settings file to pass to claude with --settings:
// settings (AGENT_LOOP_CLAUDE_SETTINGS) made absolute and checked with
// CheckSettings. With no settings it returns "" when unsandboxed is true
// (an unsandboxed run) and ErrNoSandbox otherwise (work plan #9 D2, D3,
// D5).
func Sandbox(settings string, unsandboxed bool) (string, error) {
	if settings == "" {
		if unsandboxed {
			return "", nil
		}
		return "", ErrNoSandbox
	}
	abs, err := filepath.Abs(settings)
	if err != nil {
		return "", fmt.Errorf("sandbox settings %s: %v", settings, err)
	}
	if err := CheckSettings(abs); err != nil {
		return "", err
	}
	return abs, nil
}

// CheckSettings checks that the Claude Code settings file at path turns
// the sandbox on and makes it required: a JSON object whose
// sandbox.enabled and sandbox.failIfUnavailable are both true (work plan
// #9 D1).
func CheckSettings(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("sandbox settings %s: cannot read: %v", path, errors.Unwrap(err))
	}
	var s struct {
		Sandbox *struct {
			Enabled           *bool `json:"enabled"`
			FailIfUnavailable *bool `json:"failIfUnavailable"`
		} `json:"sandbox"`
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return fmt.Errorf("sandbox settings %s: not a JSON object", path)
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("sandbox settings %s: invalid JSON: %v", path, err)
	}
	switch {
	case s.Sandbox == nil || s.Sandbox.Enabled == nil || !*s.Sandbox.Enabled:
		return fmt.Errorf("sandbox settings %s: sandbox.enabled is not true", path)
	case s.Sandbox.FailIfUnavailable == nil || !*s.Sandbox.FailIfUnavailable:
		return fmt.Errorf("sandbox settings %s: sandbox.failIfUnavailable is not true", path)
	}
	return nil
}
