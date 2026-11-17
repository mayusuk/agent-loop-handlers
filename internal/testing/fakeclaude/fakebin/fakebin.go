// Package fakebin builds the fake claude binary (package fakeclaude) for
// tests. Call Build from TestMain and point the code under test at the
// returned path, for example through AGENT_LOOP_CLAUDE_BIN.
package fakebin

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// Package is the fake's import path.
const Package = "github.com/mayusuk/agent-loop-handlers/internal/testing/fakeclaude"

// Build compiles the fake into dir as "claude" and returns its path.
func Build(dir string) (string, error) {
	bin := filepath.Join(dir, "claude")
	out, err := exec.Command("go", "build", "-o", bin, Package).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("build fake claude: %v\n%s", err, out)
	}
	return bin, nil
}
