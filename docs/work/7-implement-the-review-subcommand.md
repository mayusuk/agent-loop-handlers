# Work plan: Implement the `review` subcommand

**Issue:** [#7](https://github.com/mayusuk/agent-loop-handlers/issues/7) (feature)
**Branch:** `issue/7-implement-the-review-subcommand`
**Base:** `origin/main @ bdd4f08` (checked before branching)
**Status:** Done

## Summary

Wire the runner (#3), the prompts (#4) and the draft conversion (#5) into the `review` subcommand, as `implement` (#6) does. The handler runs Claude Code in the detached workspace at `review.commit`, converts the draft with `commit` and `kind` from the input, adjusts new findings whose location is not in the commit (plan D8), and never pushes (plan D9).

## Context

- Plan T6 (#7), plan D1, D8, D9, D12, D13; INTEGRATION-SPEC §3.3 (review is detached at `review.commit`), §3.4 (no push token), §5.2 review.
- `stage.agent` (#6) builds the prompt, runs claude and converts the draft; `prompt.Build` already passes the review object (`rule_findings`, `rule_manual`, `blocking_threshold`, `resolved_paths`); `draft.Convert` already fills `commit` and `kind`; `draft.Parse` refuses outcomes `review` does not allow (`blocked`, `needs_owner`).
- `contract.ValidateResult` checks one ruling per ID; the worker fails the review on a path that is not a file at the commit or a line past its end (agent-loop `internal/workflows/review.go`, `workspace.Git.FileLines`).

## Approach

- `internal/draft`: `ConvertWith(in, draftPath, outDir, fix)`, where `fix` may change the parsed `done` before its texts are written; `Convert` is `ConvertWith` with no fix.
- `internal/gitx`: `FileLines(ctx, dir, commit, path)`, counting lines as the worker does, and reporting whether the path is a file at the commit.
- `internal/stage`: `Review(ctx, r, flags, env)`: check `HEAD` is `review.commit`, run the agent with a fix that adjusts new findings (D8), log a moved head or uncommitted changes (D9), never push. `agent` takes the fix.
- `cmd/agent-loop-claude`: route `review` to the stage.

## Steps

- [x] `gitx.FileLines` and `draft.ConvertWith`, with tests.
- [x] `stage.Review` and the CLI wiring, with end-to-end tests using the fake claude and a bare remote.

## Acceptance criteria

- A draft that rules on every ID gives a valid `done` with `commit` equal to `review.commit` (§5.2 review).
- A draft missing a ruling for an ID in `rule_findings` or `rule_manual` gives `failed` (FR-4 AC4, AC8).
- A finding with a path missing at the commit, or a line past the end of the file, is adjusted as D8 says.
- A `blocked` draft gives `failed` (FR-18 AC1); the remote is unchanged after every review run.

## Decisions

- **D1** [gap] Plan D8, §5.2 review: a path is "in the commit" when it is a file (a blob) at `review.commit`, and a file's lines are counted as the worker counts them (a last line without a newline counts; an empty file has 0 lines). Why: the handler must agree with the worker's own check, or a finding it keeps would still fail the review.
- **D2** [gap] INTEGRATION-SPEC §3.3 review: before running claude, `HEAD` must be `review.commit`; otherwise `failed: the workspace head is <sha>, not review.commit <sha>` and claude does not run. The branch (detached or not) is not checked. Why: reviewing another tree would rule on code the worker does not post against.
- **D3** [gap] Plan D9: after claude, a moved `HEAD` and uncommitted changes are logged to the run log and otherwise ignored; the result is the review of `review.commit`, and new findings are checked against `review.commit`, not the moved head. Nothing is ever pushed.
- **D4** [gap] Plan D8: the moved location is the body's first line, ``Location: `<path>` `` or ``Location: `<path>:<line>` `` (the path as the agent gave it), then a blank line, then the body. A path that is not repository-relative (absolute, with a backslash, or with an empty, `.` or `..` segment) is treated as not in the commit. A line past the end, or a line on a finding with no path, becomes `null` and the body is not changed. Each adjustment is logged. A body pushed over 64 KiB by the prefix is `failed` like any oversized text. Why: the worker would fail the whole review on any of these (§5.2 review), and plan D8 does not give the body format.
- **D5** [gap] Plan D8: an error reading a file at `review.commit` (other than the path not being a file there) gives `failed: cannot read <path> at <sha>: <error>`, without the `draft: ` prefix. Why: it is not a problem with the draft.

## Verification

Tests use the fake claude and a local bare repository as `origin`; no GitHub or Claude.

## Risks and open questions

None needing the owner.

## Outcome

Built as planned: `gitx.FileLines`, `draft.ConvertWith` (a fix applied to a done draft before its texts are written), `stage.Review`, and `review` routed in the CLI. `stage.agent` now takes the fix; implement passes none. The implement test fixture's input and run code moved into `fixture.input` and `fixture.runStage`, shared by the review tests. Every review test checks the remote's refs are unchanged. `address` stays a stub. No follow-up issues filed.
