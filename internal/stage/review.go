package stage

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/mayusuk/agent-loop-handlers/internal/draft"
	"github.com/mayusuk/agent-loop-handlers/internal/gitx"
	"github.com/mayusuk/agent-loop-handlers/internal/prompt"
	"github.com/mayusuk/agent-loop-handlers/internal/run"
	"github.com/mayusuk/agent-loop/contract"
)

// Review runs the review stage (plan T6). It reviews review.commit in the
// detached workspace and never pushes (plan D9, INTEGRATION-SPEC §3.3,
// §3.4).
func Review(ctx context.Context, r *run.Run, f prompt.Flags, env Env) contract.Result {
	in := r.Input
	if in.Review == nil {
		return run.Failed("the input has no review object")
	}
	if dl, err := time.Parse(time.RFC3339, in.Deadline); err == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, dl)
		defer cancel()
	}
	dir, commit := in.Workspace.Path, in.Review.Commit

	head, err := gitx.Head(ctx, dir)
	if err != nil {
		return run.Failed(fmt.Sprintf("the workspace is not usable: %v", err))
	}
	if head != commit {
		return run.Failed(fmt.Sprintf("the workspace head is %s, not review.commit %s", head, commit))
	}

	res := agent(ctx, r, f, env, func(d *draft.Done) error {
		return fixLocations(ctx, dir, commit, d, r.Logf)
	})

	// The review is of review.commit whatever the agent did to the tree
	// (work plan D3).
	if h, err := gitx.Head(ctx, dir); err == nil && h != commit {
		r.Logf("the workspace head moved to %s; the review is of %s and nothing is pushed", h, commit)
	}
	if dirty, err := gitx.Dirty(ctx, dir); err == nil && dirty {
		r.Logf("the review run left uncommitted changes; nothing is pushed")
	}
	return res
}

// fixLocations adjusts each new finding whose location is not in commit,
// so one bad location does not fail the whole review (plan D8, work plan
// D1, D4): a path that is not a file at commit becomes null, its location
// moved to the top of the body; a line that is not in the file becomes
// null.
func fixLocations(ctx context.Context, dir, commit string, d *draft.Done, logf func(string, ...any)) error {
	for i := range d.NewFindings {
		nf := &d.NewFindings[i]
		if nf.Path != nil && strings.TrimSpace(*nf.Path) == "" {
			nf.Path = nil
		}
		if nf.Path == nil {
			if nf.Line != nil {
				logf("new finding %d: line %d has no path; the line is dropped", i+1, *nf.Line)
				nf.Line = nil
			}
			continue
		}
		path := *nf.Path
		n, ok := 0, false
		if repoPath(path) {
			var err error
			if n, ok, err = gitx.FileLines(ctx, dir, commit, path); err != nil {
				return fmt.Errorf("cannot read %s at %s: %v", path, commit, err)
			}
		}
		if !ok {
			loc := path
			if nf.Line != nil {
				loc = fmt.Sprintf("%s:%d", path, *nf.Line)
			}
			logf("new finding %d: %s is not a file at %s; its location moves to the body", i+1, path, commit)
			nf.Body = "Location: " + codeSpan(loc) + "\n\n" + nf.Body
			nf.Path, nf.Line = nil, nil
			continue
		}
		if nf.Line != nil && (*nf.Line < 1 || *nf.Line > n) {
			logf("new finding %d: line %d is not in %s (%d lines); the line is dropped", i+1, *nf.Line, path, n)
			nf.Line = nil
		}
	}
	return nil
}

// repoPath reports whether p is a repository-relative slash path, the
// form the contract accepts for a finding's path (work plan D4).
func repoPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, `\`) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// codeSpan writes s as one Markdown code span on one line (#29 D2):
// control characters are escaped as in Go (\n, \t, \x01), and the
// backtick fence is one longer than the longest run of backticks in s,
// with a space inside each end when s starts or ends with a backtick, as
// CommonMark requires.
func codeSpan(s string) string {
	var b strings.Builder
	run, longest := 0, 0
	for _, c := range s {
		if c == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
		if unicode.IsControl(c) {
			q := strconv.QuoteRune(c)
			b.WriteString(q[1 : len(q)-1])
			continue
		}
		b.WriteRune(c)
	}
	text := b.String()
	if strings.HasPrefix(text, "`") || strings.HasSuffix(text, "`") {
		text = " " + text + " "
	}
	fence := strings.Repeat("`", longest+1)
	return fence + text + fence
}
