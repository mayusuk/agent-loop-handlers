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

- [ ] `gitx.Parents`, with a test.
- [ ] `stage.Address` and the CLI wiring, with end-to-end tests using the fake claude and a bare remote.

## Acceptance criteria

- Replies to every ID with no push give a valid `done` with `head` equal to the input head; with a fix commit, `head` is the pushed SHA (§5.2 address, FR-5 AC2).
- A missing reply with an unmoved head gives `failed` (FR-5 AC2).
- `base_update`: one merge commit with the right parents is pushed and returned; a rebase, a one-parent commit, or two commits gives `failed` and pushes nothing (FR-5 AC6).

## Decisions

None yet.

## Verification

Tests use the fake claude and a local bare repository as `origin`; no GitHub or Claude.

## Risks and open questions

None needing the owner.

## Outcome

_Filled in before shipping._
