package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// TestI12_4_ReleaseNotesKeepHeadingsInCodeFences: a "## " line inside a
// fenced code block is part of the section, not the start of the next one,
// so the notes are not silently cut short (work plan #58 D2, D8).
func TestI12_4_ReleaseNotesKeepHeadingsInCodeFences(t *testing.T) {
	for name, tc := range map[string]struct{ changelog, want string }{
		"backtick fence": {
			"## 0.2.0\n\n```sh\n## comment\n```\n- after\n\n## 0.1.0\n\n- old\n",
			"```sh\n## comment\n```\n- after\n",
		},
		"tilde fence": {
			"## 0.2.0\n\n~~~\n## comment\n~~~\n- after\n\n## 0.1.0\n\n- old\n",
			"~~~\n## comment\n~~~\n- after\n",
		},
		"indented fence": {
			"## 0.2.0\n\n- item:\n\n  ```sh\n## comment\n  ```\n- after\n\n## 0.1.0\n\n- old\n",
			"- item:\n\n  ```sh\n## comment\n  ```\n- after\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			out, stderr, err := releaseNotes(t, fakeRepo(t, "0.2.0", tc.changelog), "0.2.0")
			if err != nil {
				t.Fatalf("release-notes.sh: %v: %s", err, stderr)
			}
			if out != tc.want {
				t.Errorf("release notes = %q, want %q", out, tc.want)
			}
		})
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

// TestI12_4_ReleaseWorkflowChecksNotesBeforePublishing: release.yml runs only
// on a v* tag push, runs the tests and scripts/release-notes.sh before
// GoReleaser, passes the notes file with --release-notes, and pins every
// action to a full commit SHA (issue #58, work plan #58 D2).
func TestI12_4_ReleaseWorkflowChecksNotesBeforePublishing(t *testing.T) {
	checkReleaseWorkflow(t, readRepoFile(t, ".github/workflows/release.yml"))
}

// TestI12_4_ReleaseWorkflowCheckCatchesBrokenWorkflows: the check above fails
// when the notes step is dropped or moved after GoReleaser, the notes are not
// passed, another trigger is added, or an action is pinned to a tag.
func TestI12_4_ReleaseWorkflowCheckCatchesBrokenWorkflows(t *testing.T) {
	good := readRepoFile(t, ".github/workflows/release.yml")
	notes := "      - name: release notes\n        run: scripts/release-notes.sh \"${GITHUB_REF_NAME#v}\" > \"$RUNNER_TEMP/release-notes.md\"\n"
	if !strings.Contains(good, notes) {
		t.Fatalf("release.yml lacks the notes step %q", notes)
	}
	goreleaser := "      - uses: goreleaser/goreleaser-action@"
	for name, broken := range map[string]string{
		"no notes step":        strings.Replace(good, notes, "", 1),
		"notes after release":  strings.Replace(strings.Replace(good, notes, "", 1), "        env:\n", "        env:\n          X: y\n"+notes, 1),
		"no --release-notes":   strings.Replace(good, " --release-notes ${{ runner.temp }}/release-notes.md", "", 1),
		"no tests":             strings.Replace(good, "        run: go test -race -count=1 ./...\n", "        run: true\n", 1),
		"branch trigger":       strings.Replace(good, "on:\n  push:\n", "on:\n  push:\n    branches: [main]\n", 1),
		"another trigger":      strings.Replace(good, "on:\n", "on:\n  workflow_dispatch:\n", 1),
		"action pinned to tag": strings.Replace(good, goreleaser+"e435ccd777264be153ace6237001ef4d979d3a7a", goreleaser+"v6", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if broken == good {
				t.Fatal("mutation did not change release.yml")
			}
			ft := &fakeT{}
			checkReleaseWorkflow(ft, broken)
			if !ft.failed {
				t.Error("check passed a broken release.yml")
			}
		})
	}
}

// checker is the part of testing.T that checkReleaseWorkflow uses, so the
// mutation test can see whether it fails.
type checker interface {
	Helper()
	Errorf(format string, args ...any)
}

type fakeT struct{ failed bool }

func (*fakeT) Helper()                 {}
func (f *fakeT) Errorf(string, ...any) { f.failed = true }

func checkReleaseWorkflow(t checker, wf string) {
	t.Helper()
	if want := "on:\n  push:\n    tags: [\"v*\"]\npermissions:"; !strings.Contains(wf, want) {
		t.Errorf("release.yml trigger is not only a v* tag push: lacks %q", want)
	}
	tests := strings.Index(wf, "run: go test -race -count=1 ./...\n")
	notes := strings.Index(wf, "run: scripts/release-notes.sh \"${GITHUB_REF_NAME#v}\" > \"$RUNNER_TEMP/release-notes.md\"\n")
	release := strings.Index(wf, "uses: goreleaser/goreleaser-action@")
	switch {
	case tests < 0 || notes < 0 || release < 0:
		t.Errorf("release.yml lacks a step: tests at %d, notes at %d, goreleaser at %d", tests, notes, release)
	case !(tests < notes && notes < release):
		t.Errorf("release.yml steps out of order: tests at %d, notes at %d, goreleaser at %d", tests, notes, release)
	}
	if want := "args: release --clean --release-notes ${{ runner.temp }}/release-notes.md\n"; !strings.Contains(wf, want) {
		t.Errorf("release.yml does not pass the notes to GoReleaser: lacks %q", want)
	}
	uses := regexp.MustCompile(`(?m)^\s*- uses: (\S+)`).FindAllStringSubmatch(wf, -1)
	if len(uses) == 0 {
		t.Errorf("release.yml has no actions")
	}
	pinned := regexp.MustCompile(`^[\w.-]+/[\w.-]+@[0-9a-f]{40}$`)
	for _, u := range uses {
		if !pinned.MatchString(u[1]) {
			t.Errorf("release.yml action %s is not pinned to a full commit SHA", u[1])
		}
	}
}
