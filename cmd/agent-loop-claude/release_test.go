package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// releaseNotes runs scripts/release-notes.sh in dir, which holds a
// CHANGELOG.md and cmd/agent-loop-claude/main.go.
func releaseNotes(t *testing.T, dir string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "release-notes.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", append([]string{script}, args...)...)
	cmd.Dir = dir
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	err = cmd.Run()
	return out.String(), errb.String(), err
}

// fakeRepo writes a CHANGELOG.md and a main.go whose Release is release.
func fakeRepo(t *testing.T, release, changelog string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "agent-loop-claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	main := "package main\n\nconst Release = \"" + release + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "cmd", "agent-loop-claude", "main.go"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte(changelog), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestI12_4_ReleaseNotesAreTheChangelogSection: for this release, the
// release notes are the body of its CHANGELOG.md section, without the
// heading (work plan #58 D2).
func TestI12_4_ReleaseNotesAreTheChangelogSection(t *testing.T) {
	out, stderr, err := releaseNotes(t, filepath.Join("..", ".."), Release)
	if err != nil {
		t.Fatalf("release-notes.sh %s: %v: %s", Release, err, stderr)
	}
	log := readRepoFile(t, "CHANGELOG.md")
	heading := "## " + Release + "\n"
	i := strings.Index(log, heading)
	if i < 0 {
		t.Fatalf("CHANGELOG.md has no %q", heading)
	}
	body := log[i+len(heading):]
	if next := strings.Index(body, "\n## "); next >= 0 {
		body = body[:next+1]
	}
	if want := strings.TrimSpace(body) + "\n"; out != want {
		t.Errorf("release notes =\n%s\nwant\n%s", out, want)
	}
}

// TestI12_4_ReleaseNotesStopAtTheNextSection: only the named version's
// section is printed, not older ones (work plan #58 D2).
func TestI12_4_ReleaseNotesStopAtTheNextSection(t *testing.T) {
	dir := fakeRepo(t, "0.2.0", "# Changelog\n\nIntro.\n\n## 0.2.0\n\n### Added\n\n- new\n\n## 0.1.0\n\n- old\n")
	out, stderr, err := releaseNotes(t, dir, "0.2.0")
	if err != nil {
		t.Fatalf("release-notes.sh: %v: %s", err, stderr)
	}
	if want := "### Added\n\n- new\n"; out != want {
		t.Errorf("release notes = %q, want %q", out, want)
	}
}

// TestI12_4_ReleaseNotesRefuseABadRelease: the script fails, so nothing is
// published, when the tag's version is not Release, the section is missing
// or empty, or no version is given (work plan #58 D2).
func TestI12_4_ReleaseNotesRefuseABadRelease(t *testing.T) {
	for name, tc := range map[string]struct {
		release, changelog string
		args               []string
		wantErr            string
	}{
		"tag is not Release":       {"0.1.0", "## 0.2.0\n\n- new\n", []string{"0.2.0"}, "is not Release (0.1.0)"},
		"tag with a leading v":     {"0.2.0", "## 0.2.0\n\n- new\n", []string{"v0.2.0"}, "is not Release (0.2.0)"},
		"no section":               {"0.2.0", "## 0.1.0\n\n- old\n", []string{"0.2.0"}, `no "## 0.2.0" section`},
		"section is a prefix":      {"0.2.0", "## 0.2.0-rc1\n\n- rc\n", []string{"0.2.0"}, `no "## 0.2.0" section`},
		"empty section":            {"0.2.0", "## 0.2.0\n\n  \n## 0.1.0\n\n- old\n", []string{"0.2.0"}, "or it is empty"},
		"empty section at the end": {"0.2.0", "## 0.2.0\n", []string{"0.2.0"}, "or it is empty"},
		"no version":               {"0.2.0", "## 0.2.0\n\n- new\n", nil, "usage:"},
		"empty version":            {"0.2.0", "## 0.2.0\n\n- new\n", []string{""}, "usage:"},
	} {
		t.Run(name, func(t *testing.T) {
			out, stderr, err := releaseNotes(t, fakeRepo(t, tc.release, tc.changelog), tc.args...)
			if err == nil {
				t.Fatalf("release-notes.sh succeeded with %q", out)
			}
			if out != "" {
				t.Errorf("stdout = %q, want nothing", out)
			}
			if !strings.Contains(stderr, tc.wantErr) {
				t.Errorf("stderr = %q, want %q", stderr, tc.wantErr)
			}
		})
	}
}

// TestI12_4_ReadmeInstallsWithHomebrew: the README installs the formula the
// release workflow writes to mayusuk/homebrew-tap (issue #58), and
// .goreleaser.yaml writes that formula with the version set from the tag.
func TestI12_4_ReadmeInstallsWithHomebrew(t *testing.T) {
	if readme := readRepoFile(t, "README.md"); !strings.Contains(readme, "brew install mayusuk/tap/agent-loop-claude\n") {
		t.Error("README.md lacks brew install mayusuk/tap/agent-loop-claude")
	}
	cfg := readRepoFile(t, ".goreleaser.yaml")
	for _, want := range []string{
		"main: ./cmd/agent-loop-claude\n",
		"-X main.version={{ .Version }}",
		"  - name: agent-loop-claude\n",
		"      owner: mayusuk\n      name: homebrew-tap\n      branch: main\n",
		"token: \"{{ .Env.HOMEBREW_TAP_GITHUB_TOKEN }}\"",
		"directory: Formula\n",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf(".goreleaser.yaml lacks %q", want)
		}
	}
}
