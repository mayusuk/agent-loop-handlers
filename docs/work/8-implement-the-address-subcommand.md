# Work plan: Implement the `address` subcommand, including `base_update` mode

**Issue:** [#8](https://github.com/mayusuk/agent-loop-handlers/issues/8) (feature)
**Branch:** `issue/8-implement-the-address-subcommand`
**Base:** `origin/main @ b1d71f1` (checked before branching)
**Status:** Draft

## Summary

Wire the runner, prompts and draft conversion into the `address` subcommand, as `implement` and `review` do. In `findings` and `manual` modes the handler pushes any new commits and sets `head` (the input head when nothing was committed). In `base_update` mode it checks that the agent made exactly one merge commit of the base into the branch (plan D10) before pushing it, and the replies must be empty.

## Context

- Plan T7 (#8), plan D4, D10; INTEGRATION-SPEC §3.3, §4.2 address, §5.2 address; FR-5 AC2, AC6.
- `stage.agent` builds the prompt, runs claude and converts the draft with an optional `draft.Fix`; `stage.checkWorkspace` and `gitx.Push` come from implement (#6). The prompt already carries the address mode, the IDs to answer and the base-update instructions (#4).
- `contract.ValidateResult` (run in `run.Finish`) checks FR-5 AC2 and the empty replies and moved head of `base_update`; `contract.MissingReplies` gives the missing IDs.

## Approach

- `internal/gitx`: `Parents(ctx, dir, sha)`, the commit's parents in order.
- `internal/stage/address.go`: `Address(ctx, r, flags, env)`: check the address object and the workspace, run the agent (in `base_update`, a fix that refuses replies), then for `done`:
  - `findings` / `manual`: an unmoved head gives `head` = input head and no push; a moved head must build on the input head, and is pushed. FR-5 AC2 is checked before the push.
  - `base_update`: check the merge shape (D10, work plan D2), then push.
- `cmd/agent-loop-claude`: route `address` to the stage.

## Steps

- [x] `gitx.Parents`, with a test.
- [x] `stage.Address` and the CLI wiring, with end-to-end tests using the fake claude and a bare remote.

## Acceptance criteria

- Replies to every ID with no push give a valid `done` with `head` equal to the input head; with a fix commit, `head` is the pushed SHA (§5.2 address, FR-5 AC2).
- A missing reply with an unmoved head gives `failed` (FR-5 AC2).
- `base_update`: one merge commit with the right parents is pushed and returned; a rebase, a one-parent commit, or two commits gives `failed` and pushes nothing (FR-5 AC6).

## Decisions

- **D1** [gap] §5.2 address, FR-5 AC2: in `findings` and `manual` modes the handler checks FR-5 AC2 itself before pushing: an unmoved head with a finding that has no reply gives `failed: the head did not move and <IDs> have no reply (FR-5 AC2)`, and a manual comment with no reply gives `failed: manual comments <IDs> have no reply (FR-5 AC2); nothing was pushed`, whether or not the head moved. A `deferred` reply counts whatever item it names; the worker checks the item is open (FR-5 AC5). Why: `ValidateResult` would fail the result anyway, but after the push; a failed run should not leave new commits on the branch.
- **D2** [ambiguity] §5.2 address `base_update`, plan D10: "a base commit that contains `base_tip`" is read as: the merge's second parent has `change.base_tip` as an ancestor and is itself an ancestor of `origin/<base_branch>` in the workspace. An input with no `change` or no `change.base_tip` in `base_update` mode is `failed` before claude runs. Every shape failure ends with `; nothing was pushed`. Why: a merge of a side branch that contains the base tip would bring in commits that are not on the base.
- **D3** [gap] §5.2 address `base_update`: a `done` draft with any reply is `failed: a base_update address run must not reply (FR-5 AC6); nothing was pushed`, before any text file is written. `blocked` and `needs_owner` are allowed in `base_update` mode, as in the other modes, and push nothing, whatever the agent committed. Why: FR-18 AC1 allows them from address and FR-5 AC6 does not exclude them.
- **D4** [gap] §5.2 address: in `findings` and `manual` modes, a moved head must build on the input head (as in implement, #6); otherwise `failed: HEAD <sha> does not build on <sha>; nothing was pushed`. The workspace must be on `workspace.branch` at `workspace.head` before claude runs (#6 work plan D2). Uncommitted changes are logged and not pushed.

## Verification

Tests use the fake claude and a local bare repository as `origin`; no GitHub or Claude.

## Risks and open questions

None needing the owner.

## Outcome

_Filled in before shipping._
