// Package stage holds the stage subcommands' bodies: each builds the
// prompt, runs Claude Code in the workspace, converts the agent's draft and
// does the stage's git work (plan T5–T7).
package stage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mayusuk/agent-loop-handlers/internal/claude"
	"github.com/mayusuk/agent-loop-handlers/internal/draft"
	"github.com/mayusuk/agent-loop-handlers/internal/gitx"
	"github.com/mayusuk/agent-loop-handlers/internal/prompt"
	"github.com/mayusuk/agent-loop-handlers/internal/run"
	"github.com/mayusuk/agent-loop/contract"
)

// EnvDraft is the draft path in claude's environment (plan D1).
const EnvDraft = "AGENT_LOOP_CLAUDE_DRAFT"

// DraftName is the draft's file name in its private directory (work plan D1).
const DraftName = "agent-draft.json"

// Env is what a stage reads from the handler's environment.
type Env struct {
	// Getenv reads one variable.
	Getenv func(string) string
	// Environ is the environment claude starts with; EnvDraft is added.
	Environ []string
}

// Implement runs the implement stage (plan T5).
func Implement(ctx context.Context, r *run.Run, f prompt.Flags, env Env) contract.Result {
	in := r.Input
	ws := in.Workspace
	// git after claude is bounded by the input deadline itself.
	if dl, err := time.Parse(time.RFC3339, in.Deadline); err == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, dl)
		defer cancel()
	}

	if err := checkWorkspace(ctx, ws); err != nil {
		return run.Failed(err.Error())
	}

	res := agent(ctx, r, f, env)
	if res.Result != contract.OutcomeDone {
		return res
	}

	head, err := gitx.Head(ctx, ws.Path)
	if err != nil {
		return withUsage(run.Failed(err.Error()), res.Usage)
	}
	if head == ws.Head {
		return withUsage(run.Failed("no new commit since "+ws.Head), res.Usage)
	}
	if ok, err := gitx.IsAncestor(ctx, ws.Path, ws.Head, head); err != nil {
		return withUsage(run.Failed(err.Error()), res.Usage)
	} else if !ok {
		return withUsage(run.Failed(fmt.Sprintf("HEAD %s does not build on %s; nothing was pushed", head, ws.Head)), res.Usage)
	}
	if dirty, err := gitx.Dirty(ctx, ws.Path); err == nil && dirty {
		r.Logf("uncommitted changes in the workspace are not pushed")
	}
	if err := gitx.Push(ctx, ws.Path, head, ws.Branch, r.Log()); err != nil {
		return withUsage(run.Failed(fmt.Sprintf("push to %s failed: %v", ws.Branch, err)), res.Usage)
	}
	r.Logf("pushed %s to %s", head, ws.Branch)
	res.Done.Implement.Head = head
	return res
}

// checkWorkspace checks the workspace is on the input's branch at its
// head (work plan D2).
func checkWorkspace(ctx context.Context, ws contract.Workspace) error {
	branch, err := gitx.Branch(ctx, ws.Path)
	if err != nil {
		return fmt.Errorf("the workspace is not usable: %v", err)
	}
	if branch != ws.Branch {
		if branch == "" {
			branch = "a detached HEAD"
		}
		return fmt.Errorf("the workspace is on %s, not %s", branch, ws.Branch)
	}
	head, err := gitx.Head(ctx, ws.Path)
	if err != nil {
		return fmt.Errorf("the workspace is not usable: %v", err)
	}
	if head != ws.Head {
		return fmt.Errorf("the workspace head is %s, not %s", head, ws.Head)
	}
	return nil
}

// agent builds the prompt, runs claude and converts its draft. A claude
// that did not end cleanly is failed, whatever draft it left (work plan
// D5). Usage is set on every result once claude started (work plan D6).
func agent(ctx context.Context, r *run.Run, f prompt.Flags, env Env) contract.Result {
	in := r.Input
	stopAt, err := claude.StopAt(in.Deadline, claude.DefaultMargin)
	if err != nil {
		return run.Failed(err.Error())
	}
	bin, err := claude.Find("", env.Getenv)
	if err != nil {
		return run.Failed(err.Error())
	}
	dir, err := os.MkdirTemp("", "agent-loop-claude-")
	if err != nil {
		return run.Failed("cannot make the draft directory: " + err.Error())
	}
	defer os.RemoveAll(dir)
	draftPath := filepath.Join(dir, DraftName)

	p, err := prompt.Build(ctx, prompt.Options{
		Input:     in,
		InputPath: env.Getenv(run.EnvInput),
		DraftPath: draftPath,
		Skill:     f.Skill,
		Logf:      r.Logf,
	})
	if err != nil {
		return run.Failed(err.Error())
	}

	out, err := claude.Run(ctx, claude.Options{
		Bin:    bin,
		Args:   claude.Args(p.Text, env.Getenv(claude.EnvSettings)),
		Dir:    in.Workspace.Path,
		Env:    append(append([]string(nil), env.Environ...), EnvDraft+"="+draftPath),
		StopAt: stopAt,
		Stdout: r.Log(),
		Stderr: r.Log(),
	})
	if err != nil {
		if errors.Is(err, claude.ErrTimeLimit) {
			return withUsage(run.Failed("time limit reached"), out.Usage)
		}
		return withUsage(run.Failed(err.Error()), out.Usage)
	}
	return withUsage(draft.Convert(in, draftPath, r.Out), out.Usage)
}

func withUsage(res contract.Result, u *contract.Usage) contract.Result {
	res.Usage = u
	return res
}
