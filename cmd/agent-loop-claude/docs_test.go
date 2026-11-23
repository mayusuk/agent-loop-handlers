package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mayusuk/agent-loop-handlers/internal/run"
)

func readRepoFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestI12_4_ChangelogNamesTheRelease: the newest CHANGELOG.md section is
// this release, and it names the contract versions `version` reports
// (INTEGRATION-SPEC §12.4, work plan #11 D1).
func TestI12_4_ChangelogNamesTheRelease(t *testing.T) {
	log := readRepoFile(t, "CHANGELOG.md")
	m := regexp.MustCompile(`(?m)^## (\S+)`).FindStringSubmatch(log)
	if m == nil || m[1] != Release {
		t.Fatalf("newest CHANGELOG.md section = %v, want ## %s", m, Release)
	}
	section := log[strings.Index(log, m[0]):]
	if next := strings.Index(section[len(m[0]):], "\n## "); next >= 0 {
		section = section[:len(m[0])+next]
	}
	if want := "Contract versions: " + strings.Join(run.Contracts, ", ") + "\n"; !strings.Contains(section, want) {
		t.Errorf("CHANGELOG.md %s section lacks %q", Release, want)
	}
}

// TestI12_4_ReadmeNamesTheReleaseAndProfiles: the README names the
// release and contract versions, and its profile has every required stage
// with the §12.4 command and contract (work plan #11 D4).
func TestI12_4_ReadmeNamesTheReleaseAndProfiles(t *testing.T) {
	readme := readRepoFile(t, "README.md")
	for _, want := range []string{
		"Release: **" + Release + "**",
		"for contract version **" + strings.Join(run.Contracts, ", ") + "**",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md lacks %q", want)
		}
	}
	for _, stage := range []string{"implement", "review", "address"} {
		block := regexp.MustCompile(`(?m)^\[stages\.` + stage + `\]\ncommand +=\s+\["agent-loop-claude", "` + stage + `"\]\ncontract +=\s+"1\.0"\n`)
		if !block.MatchString(readme) {
			t.Errorf("README.md has no [stages.%s] entry with command [\"agent-loop-claude\", %q] and contract \"1.0\"", stage, stage)
		}
	}
}
