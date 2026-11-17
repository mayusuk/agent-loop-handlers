// Package gitx holds the git operations the coder stages (implement and
// address) need in a run's workspace: reading the head and branch, and
// pushing a commit to the item's branch with a plain push, never a forced
// one (plan D4).
package gitx

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Remote is the remote the worker's worktree pushes to (INTEGRATION-SPEC §3.3).
const Remote = "origin"

// Git runs git in dir and returns its stdout. An error includes the last
// line git wrote to stderr. Git never prompts (GIT_TERMINAL_PROMPT=0), so
// a run stays headless (INTEGRATION-SPEC §3.1).
func Git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return git(ctx, dir, nil, args...)
}

func git(ctx context.Context, dir string, log io.Writer, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Stdin = nil
	cmd.Stdout = &stdout
	if log != nil {
		log = &lockedWriter{w: log}
		cmd.Stdout = io.MultiWriter(&stdout, log)
		cmd.Stderr = io.MultiWriter(&stderr, log)
	} else {
		cmd.Stderr = &stderr
	}
	if err := cmd.Run(); err != nil {
		if line := lastLine(stderr.String()); line != "" {
			return stdout.Bytes(), fmt.Errorf("git %s: %s", args[0], line)
		}
		return stdout.Bytes(), fmt.Errorf("git %s: %w", args[0], err)
	}
	return stdout.Bytes(), nil
}

// lockedWriter serialises the writes of git's stdout and stderr copiers.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// lastLine is the last line of s that is not a hint.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" && !strings.HasPrefix(l, "hint:") {
			return l
		}
	}
	return ""
}

// Head returns the SHA HEAD points to.
func Head(ctx context.Context, dir string) (string, error) {
	out, err := Git(ctx, dir, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Branch returns the branch checked out in dir, or "" when HEAD is
// detached.
func Branch(ctx context.Context, dir string) (string, error) {
	out, err := Git(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		if _, err2 := Head(ctx, dir); err2 == nil {
			return "", nil // detached
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// IsAncestor reports whether commit a is an ancestor of commit b (or b
// itself).
func IsAncestor(ctx context.Context, dir, a, b string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", a, b)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
		return false, nil
	}
	if line := lastLine(stderr.String()); line != "" {
		return false, fmt.Errorf("git merge-base: %s", line)
	}
	return false, fmt.Errorf("git merge-base: %w", err)
}

// Dirty reports whether the working tree has changes that are not
// committed, untracked files included.
func Dirty(ctx context.Context, dir string) (bool, error) {
	out, err := Git(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return len(bytes.TrimSpace(out)) > 0, nil
}

// Push pushes commit sha to branch on Remote with a plain push: a push
// the remote refuses as not a fast-forward is an error, and nothing is
// ever forced (plan D4). git's output goes to log.
func Push(ctx context.Context, dir, sha, branch string, log io.Writer) error {
	if sha == "" || branch == "" || strings.HasPrefix(branch, "-") {
		return fmt.Errorf("invalid push of %q to %q", sha, branch)
	}
	out, err := git(ctx, dir, log, "push", "--porcelain", Remote, sha+":refs/heads/"+branch)
	if err != nil {
		// --porcelain reports a refused ref as "!<TAB>from:to<TAB>summary".
		for _, l := range strings.Split(string(out), "\n") {
			if f := strings.Split(l, "\t"); len(f) >= 3 && f[0] == "!" {
				return fmt.Errorf("git push: %s", strings.TrimSpace(f[2]))
			}
		}
	}
	return err
}
