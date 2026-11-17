package stage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mayusuk/agent-loop-handlers/internal/draft"
	"github.com/mayusuk/agent-loop-handlers/internal/gitx"
	"github.com/mayusuk/agent-loop-handlers/internal/prompt"
	"github.com/mayusuk/agent-loop-handlers/internal/run"
	"github.com/mayusuk/agent-loop/contract"
)

// Address runs the address stage (plan T7). In findings and manual modes
// it pushes the agent's new commits, if any, and sets head; in base_update
// mode it pushes only one merge commit of the base into the branch (plan
// D10, INTEGRATION-SPEC §5.2 address).
func Address(ctx context.Context, r *run.Run, f prompt.Flags, env Env) contract.Result {
	in := r.Input
	if in.Address == nil {
		return run.Failed("the input has no address object")
	}
	ws := in.Workspace
	baseUpdate := in.Address.Mode == contract.ModeBaseUpdate
	if baseUpdate && (in.Change == nil || in.Change.BaseTip == "") {
		return run.Failed("a base_update address run needs change.base_tip")
	}
	if dl, err := time.Parse(time.RFC3339, in.Deadline); err == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, dl)
		defer cancel()
	}

	if err := checkWorkspace(ctx, ws); err != nil {
		return run.Failed(err.Error())
	}

	var fix draft.Fix
	if baseUpdate {
		fix = func(d *draft.Done) error {
			if len(d.Replies) > 0 || len(d.ManualReplies) > 0 {
				return errors.New("a base_update address run must not reply (FR-5 AC6); nothing was pushed")
			}
			return nil
		}
	}
	res := agent(ctx, r, f, env, fix)
	if res.Result != contract.OutcomeDone {
		return res
	}
	fail := func(reason string) contract.Result { return withUsage(run.Failed(reason), res.Usage) }

	head, err := gitx.Head(ctx, ws.Path)
	if err != nil {
		return fail(err.Error())
	}
	ad := res.Done.Address
	ad.Head = head

	if baseUpdate {
		if err := checkBaseMerge(ctx, ws.Path, ws.Head, head, "origin/"+in.Repo.BaseBranch, in.Change.BaseTip); err != nil {
			return fail(err.Error() + "; nothing was pushed")
		}
	} else {
		if head != ws.Head {
			if ok, err := gitx.IsAncestor(ctx, ws.Path, ws.Head, head); err != nil {
				return fail(err.Error())
			} else if !ok {
				return fail(fmt.Sprintf("HEAD %s does not build on %s; nothing was pushed", head, ws.Head))
			}
		}
		// FR-5 AC2 is checked before the push, so a failed result pushes
		// nothing (work plan D1). The worker checks deferred_to itself.
		mf, mm := contract.MissingReplies(in, ad, func(string) bool { return true })
		if len(mf) > 0 && head == ws.Head {
			return fail(fmt.Sprintf("the head did not move and %s have no reply (FR-5 AC2)", strings.Join(mf, ", ")))
		}
		if len(mm) > 0 {
			return fail(fmt.Sprintf("manual comments %s have no reply (FR-5 AC2); nothing was pushed", strings.Join(mm, ", ")))
		}
	}

	if dirty, err := gitx.Dirty(ctx, ws.Path); err == nil && dirty {
		r.Logf("uncommitted changes in the workspace are not pushed")
	}
	if head == ws.Head {
		r.Logf("no new commit; nothing to push")
		return res
	}
	if err := gitx.Push(ctx, ws.Path, head, ws.Branch, r.Log()); err != nil {
		return fail(fmt.Sprintf("push to %s failed: %v", ws.Branch, err))
	}
	r.Logf("pushed %s to %s", head, ws.Branch)
	return res
}

// checkBaseMerge checks that head is one merge commit whose first parent
// is the input head and whose second parent is a commit of base that
// contains baseTip (plan D10, FR-5 AC6, work plan D2).
func checkBaseMerge(ctx context.Context, dir, inHead, head, base, baseTip string) error {
	if head == inHead {
		return fmt.Errorf("no merge commit since %s", inHead)
	}
	parents, err := gitx.Parents(ctx, dir, head)
	if err != nil {
		return err
	}
	if len(parents) != 2 {
		return fmt.Errorf("HEAD %s is not a merge commit of two parents (it has %d)", head, len(parents))
	}
	if parents[0] != inHead {
		return fmt.Errorf("HEAD %s is not one merge commit on %s: its first parent is %s", head, inHead, parents[0])
	}
	if ok, err := gitx.IsAncestor(ctx, dir, baseTip, parents[1]); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("the merged commit %s does not contain base tip %s", parents[1], baseTip)
	}
	if ok, err := gitx.IsAncestor(ctx, dir, parents[1], base); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("the merged commit %s is not on %s", parents[1], base)
	}
	return nil
}
