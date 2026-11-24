package gitx

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// clone makes a bare remote with one commit on main and a clone of it on
// branch agent/1, and returns the clone and the remote.
func clone(t *testing.T) (ws, remote string) {
	t.Helper()
	root := t.TempDir()
	remote = filepath.Join(root, "remote.git")
	ws = filepath.Join(root, "ws")
	run(t, root, "init", "-q", "--bare", "-b", "main", remote)
	run(t, root, "clone", "-q", remote, ws)
	run(t, ws, "config", "user.name", "t")
	run(t, ws, "config", "user.email", "t@example.com")
	run(t, ws, "commit", "-q", "--allow-empty", "-m", "base")
	run(t, ws, "push", "-q", "origin", "HEAD:main")
	run(t, ws, "checkout", "-q", "-b", "agent/1")
	return ws, remote
}

func commit(t *testing.T, ws, name string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(ws, name), []byte(name), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, ws, "add", name)
	run(t, ws, "commit", "-q", "-m", name)
	return run(t, ws, "rev-parse", "HEAD")
}

func TestHeadBranchDirty(t *testing.T) {
	ctx := context.Background()
	ws, _ := clone(t)
	head, err := Head(ctx, ws)
	if err != nil || head != run(t, ws, "rev-parse", "HEAD") {
		t.Fatalf("Head = %q, %v", head, err)
	}
	if b, err := Branch(ctx, ws); err != nil || b != "agent/1" {
		t.Fatalf("Branch = %q, %v", b, err)
	}
	if d, err := Dirty(ctx, ws); err != nil || d {
		t.Fatalf("Dirty = %v, %v; want clean", d, err)
	}
	os.WriteFile(filepath.Join(ws, "new"), []byte("x"), 0o644)
	if d, err := Dirty(ctx, ws); err != nil || !d {
		t.Fatalf("Dirty = %v, %v; want dirty", d, err)
	}
	run(t, ws, "checkout", "-q", "--detach")
	if b, err := Branch(ctx, ws); err != nil || b != "" {
		t.Fatalf("detached Branch = %q, %v", b, err)
	}
	if _, err := Branch(ctx, t.TempDir()); err == nil {
		t.Fatal("Branch outside a repository: no error")
	}
}

func TestIsAncestor(t *testing.T) {
	ctx := context.Background()
	ws, _ := clone(t)
	base := run(t, ws, "rev-parse", "HEAD")
	next := commit(t, ws, "a")
	if ok, err := IsAncestor(ctx, ws, base, next); err != nil || !ok {
		t.Errorf("base ancestor of next = %v, %v", ok, err)
	}
	if ok, err := IsAncestor(ctx, ws, next, base); err != nil || ok {
		t.Errorf("next ancestor of base = %v, %v", ok, err)
	}
	if _, err := IsAncestor(ctx, ws, "0000000000000000000000000000000000000000", next); err == nil {
		t.Error("unknown commit: no error")
	}
}

func TestD4_PushIsPlainAndFastForwardOnly(t *testing.T) {
	ctx := context.Background()
	ws, remote := clone(t)
	first := commit(t, ws, "a")
	var log bytes.Buffer
	if err := Push(ctx, ws, first, "agent/1", &log); err != nil {
		t.Fatalf("Push: %v\n%s", err, log.String())
	}
	if got := run(t, remote, "rev-parse", "refs/heads/agent/1"); got != first {
		t.Fatalf("remote head = %s, want %s", got, first)
	}

	// Another commit lands on the remote; a push of a sibling is refused
	// and the remote keeps its head.
	other := filepath.Join(t.TempDir(), "other")
	run(t, "", "clone", "-q", "-b", "agent/1", remote, other)
	run(t, other, "config", "user.name", "t")
	run(t, other, "config", "user.email", "t@example.com")
	theirs := commit(t, other, "theirs")
	run(t, other, "push", "-q", "origin", "agent/1")

	mine := commit(t, ws, "mine")
	err := Push(ctx, ws, mine, "agent/1", &log)
	if err == nil {
		t.Fatal("non-fast-forward push: no error")
	}
	if !strings.Contains(err.Error(), "[rejected]") {
		t.Errorf("error = %q, want the rejection reason", err)
	}
	if got := run(t, remote, "rev-parse", "refs/heads/agent/1"); got != theirs {
		t.Fatalf("remote head = %s, want %s (unchanged)", got, theirs)
	}
}

func TestPushRejectsBadArguments(t *testing.T) {
	for _, c := range [][2]string{{"", "agent/1"}, {"abc", ""}, {"abc", "--force"}} {
		if err := Push(context.Background(), t.TempDir(), c[0], c[1], nil); err == nil {
			t.Errorf("Push(%q, %q): no error", c[0], c[1])
		}
	}
}

func TestFileLines(t *testing.T) {
	ctx := context.Background()
	ws, _ := clone(t)
	os.WriteFile(filepath.Join(ws, "n.txt"), []byte("1\n2\n3"), 0o644)
	os.WriteFile(filepath.Join(ws, "e.txt"), nil, 0o644)
	os.MkdirAll(filepath.Join(ws, "dir"), 0o755)
	os.WriteFile(filepath.Join(ws, "dir", "x"), []byte("x\n"), 0o644)
	run(t, ws, "add", ".")
	run(t, ws, "commit", "-q", "-m", "files")
	head := run(t, ws, "rev-parse", "HEAD")
	for _, c := range []struct {
		path string
		n    int
		ok   bool
	}{
		{"n.txt", 3, true},
		{"dir/x", 1, true},
		{"e.txt", 0, true},
		{"missing.txt", 0, false},
		{"dir", 0, false},
		{"dir/", 0, false},
		{"d*/x", 0, false},
		{"", 0, false},
	} {
		n, ok, err := FileLines(ctx, ws, head, c.path)
		if err != nil || n != c.n || ok != c.ok {
			t.Errorf("FileLines(%s) = %d, %v, %v; want %d, %v", c.path, n, ok, err, c.n, c.ok)
		}
	}
	// A file added after the commit is not in it.
	os.WriteFile(filepath.Join(ws, "later.txt"), []byte("l\n"), 0o644)
	run(t, ws, "add", "later.txt")
	run(t, ws, "commit", "-q", "-m", "later")
	if _, ok, err := FileLines(ctx, ws, head, "later.txt"); ok || err != nil {
		t.Errorf("FileLines(later.txt at the earlier commit) = %v, %v; want not a file", ok, err)
	}
}

// A git failure is an error, never "not a file" (work plan #7 D5, #29).
func TestD5_FileLinesGitFailureIsAnError(t *testing.T) {
	ws, _ := clone(t)
	head := commit(t, ws, "a.txt")
	if _, ok, err := FileLines(context.Background(), ws, strings.Repeat("0", 40), "a.txt"); err == nil || ok {
		t.Errorf("FileLines(unknown commit) = %v, %v; want an error", ok, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok, err := FileLines(ctx, ws, head, "a.txt"); err == nil || ok {
		t.Errorf("FileLines(done ctx) = %v, %v; want an error", ok, err)
	}
	if _, ok, err := FileLines(context.Background(), t.TempDir(), head, "a.txt"); err == nil || ok {
		t.Errorf("FileLines(not a repository) = %v, %v; want an error", ok, err)
	}
	// The commit resolves but its tree is gone.
	tree := run(t, ws, "rev-parse", head+"^{tree}")
	if err := os.Remove(filepath.Join(ws, ".git", "objects", tree[:2], tree[2:])); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := FileLines(context.Background(), ws, head, "a.txt"); err == nil || ok {
		t.Errorf("FileLines(missing tree) = %v, %v; want an error", ok, err)
	}
}

func TestParents(t *testing.T) {
	ctx := context.Background()
	ws, _ := clone(t)
	base := run(t, ws, "rev-parse", "HEAD")
	if p, err := Parents(ctx, ws, base); err != nil || len(p) != 0 {
		t.Fatalf("root Parents = %v, %v", p, err)
	}
	a := commit(t, ws, "a")
	run(t, ws, "checkout", "-q", "-b", "side", base)
	b := commit(t, ws, "b")
	run(t, ws, "checkout", "-q", "agent/1")
	run(t, ws, "merge", "-q", "--no-edit", "side")
	m := run(t, ws, "rev-parse", "HEAD")
	if p, err := Parents(ctx, ws, a); err != nil || len(p) != 1 || p[0] != base {
		t.Errorf("Parents(a) = %v, %v, want [%s]", p, err, base)
	}
	if p, err := Parents(ctx, ws, m); err != nil || len(p) != 2 || p[0] != a || p[1] != b {
		t.Errorf("Parents(merge) = %v, %v, want [%s %s]", p, err, a, b)
	}
	if _, err := Parents(ctx, ws, "0123456789012345678901234567890123456789"); err == nil {
		t.Errorf("Parents of a missing commit gave no error")
	}
}
