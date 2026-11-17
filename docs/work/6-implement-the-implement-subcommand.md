# Work plan: Implement the `implement` subcommand

**Issue:** [#6](https://github.com/mayusuk/agent-loop-handlers/issues/6) (feature)
**Branch:** `issue/6-implement-the-implement-subcommand`
**Base:** `origin/main @ 6412621` (checked before branching)
**Status:** Done

## Summary

Wire the runner (#3), the prompts (#4) and the draft conversion (#5) into the `implement` subcommand. The handler builds the prompt, runs Claude Code in the workspace, converts the draft, and for `done` pushes `HEAD` to the item's branch with a plain push and sets `done.head` to the pushed SHA (plan D4). The git helpers live in `internal/gitx` so `address` (T7) can reuse them.

## Context

- Plan T5 (#6), plan D1–D4, D12, D13; INTEGRATION-SPEC §3.1, §3.3, §3.4, §4.1, §5.2 implement; TECHNICAL-SPEC §7.3.
- `run.Main` guarantees a validated result; `claude.Run` runs the CLI; `prompt.Build` builds the prompt; `draft.Convert` turns the draft into a result and leaves `done.head` empty (#5 D2).
- `contract.ValidateResult` checks `done.head` is a SHA; the worker checks it is the remote head (§5.2).

## Approach

- `internal/gitx`: `Head`, `Branch`, `IsAncestor`, `Dirty`, `Push` (plain `git push origin <sha>:refs/heads/<branch>`, never `--force`, `GIT_TERMINAL_PROMPT=0`).
- `internal/stage`: `Implement(ctx, r, flags, env)`: check the workspace is on `workspace.branch` at `workspace.head`, make a private draft directory, build the prompt, run `claude` with `AGENT_LOOP_CLAUDE_DRAFT` set, convert the draft, and for `done` check for a new commit that builds on `workspace.head`, push it and set `head`. Usage from Claude's output goes on every result.
- `internal/run`: export `Logf` and `Log()` so stages write to the run log.
- `cmd/agent-loop-claude`: route `implement` to the stage.

## Steps

- [x] `internal/gitx` with tests against a local bare repository.
- [x] `internal/stage` `Implement`, `run` log accessors, and the CLI wiring, with end-to-end tests using the fake claude and a bare remote.

## Acceptance criteria

- With the fake making a commit and writing a `done` draft, the bare remote's branch head equals `done.head`, and the result is valid (INTEGRATION-SPEC §5.2 implement).
- A rerun on a branch that already has pushes from an interrupted run builds on it (§4.1).
- No new commit, a refused push (non-fast-forward), and a `claude` failure each give `failed`; nothing is force-pushed.
- `blocked` and `needs_owner` drafts give those results and push nothing.

## Decisions

- **D1** [gap] Plan D1, INTEGRATION-SPEC §12.4: the draft is written to `agent-draft.json` in a fresh private directory made with `os.MkdirTemp` under `TMPDIR` (`agent-loop-claude-*`), outside the workspace and `AGENT_LOOP_OUT`; its path is in the prompt and in `AGENT_LOOP_CLAUDE_DRAFT` in claude's environment, and the directory is removed when the stage ends. Why: in the workspace it could be committed; in `AGENT_LOOP_OUT` the worker could read it. T8 must make sure the sandbox lets claude write under `TMPDIR`.
- **D2** [gap] INTEGRATION-SPEC §3.3: before running claude, the workspace must be on `workspace.branch` with `HEAD` at `workspace.head`; otherwise `failed` naming what differs. Why: pushing from the wrong branch or base would put the wrong commits on the item's branch.
- **D3** [gap] Plan T5, §5.2 implement: for `done`, `HEAD` must differ from `workspace.head` (else `failed: no new commit since <head>`) and have `workspace.head` as an ancestor (else `failed`, nothing pushed). Only committed work is pushed; uncommitted changes are logged and left out. Why: the worker opens a change from the branch, so only commits count; an agent that reset the branch would otherwise replace the interrupted run's pushes.
- **D4** [gap] Plan D4: the push is `git push --porcelain origin <sha>:refs/heads/<workspace.branch>` with `GIT_TERMINAL_PROMPT=0`, never forced; `done.head` is that SHA. A refused or failed push is `failed: push to <branch> failed: <git's last error line>`. Why: pushing the SHA, not `HEAD`, means `head` is what was pushed even if the tree moves, and the prompt-off setting keeps git headless (§3.1).
- **D5** [gap] FR-18 AC3, AC4: a claude that stops at the time limit gives `failed: time limit reached`; any other claude error gives `failed: <error>` (for example `claude exited with code 3`), and a draft written before the failure is ignored. A prompt or workspace error gives `failed` with its reason. Why: a run that did not finish cleanly is not trusted to have finished its work.
- **D6** [gap] §5.1 `usage`: usage from claude's output is set on every result of a run in which claude started, including `failed`, `blocked` and `needs_owner`. Why: the spend is real whatever the outcome.
- **D7** [open] Plan D3, D13: the `--deadline-margin` and `--claude` flags the plan names are not added here; the margin is the default 2 minutes and claude is found through `AGENT_LOOP_CLAUDE_BIN` or `PATH`. Open: whether a later task adds the flags.

## Verification

Tests use the fake claude and a local bare repository as `origin`; no GitHub or Claude.

## Risks and open questions

None needing the owner.

## Outcome

Built as planned: `internal/gitx` (head, branch, ancestry, dirty check, plain push with the refusal reason from `--porcelain`), `internal/stage.Implement`, `run.Logf`/`run.Log` (the run log is now serialised, since claude's and git's stdout and stderr copiers share it), and `implement` routed in the CLI. `review` and `address` stay stubs. Tests cover every acceptance criterion against a local bare remote with the fake claude, plus the workspace check, a reset branch, a missing draft and the draft's location. No follow-up issues filed; D7 is open for a later task.
