# Work plan: Runner edge cases from the review of #18

**Issue:** [#19](https://github.com/mayusuk/agent-loop-handlers/issues/19) (bug)
**Branch:** `issue/19-runner-edge-cases-from-the-review-of-18`
**Base:** `origin/main @ ef6eb47` (checked before branching)
**Status:** Draft

## Summary

Closes four edge cases in `internal/claude.Run` and `ParseUsage` found in the review of #18: tests for a run killed by a signal and for usage kept on a timeout, saturating addition for input tokens so a huge count can't go negative, and a run that ends on its own before the stop time is no longer reported as a time limit when the stop time passes while its pipes drain.

## Context

- `internal/claude/claude.go` (work plan #3, D2, D4, D5, D6). INTEGRATION-SPEC §5.1 `usage`: `input_tokens` is an integer, minimum 0 (contract/v1/result.schema.json).
- R3 root cause: `usageFrom` adds three int64 fields with `+=`; values near `math.MaxInt64` wrap to a negative count, which fails the schema.
- R4 root cause: `Run` decides "time limit" from `ctx.Err() != nil` after `Wait`. When claude exits (0 or non-zero) before the stop time but a child keeps its output pipes open, `Wait` keeps draining for the grace period; if the stop time passes meanwhile, `ctx.Err()` is set and the run is reported as `ErrTimeLimit` even though it was never stopped.

## Approach

- `Run`: `cmd.Cancel` first checks with signal 0 whether claude was already waited for; if so it returns `os.ErrProcessDone` (so exec injects no error) and still TERMs the group's leftovers. Otherwise it records that claude was stopped and TERMs the group. The time-limit / caller-context result is chosen from that record, not from `ctx.Err()`.
- `usageFrom`: saturating addition of the input token fields.
- fakeclaude: `FAKECLAUDE_OUTPUT_FIRST=1` prints the output before sleeping, so a timed-out run has usage.
- Tests in `internal/claude/claude_test.go`.

## Steps

- [ ] Failing tests: R4 (exit 0 / exit 3 with a child holding stderr past the stop time), R3 (overflowing input tokens), plus R1 (killed by signal) and R2 (usage kept on timeout, with `FAKECLAUDE_OUTPUT_FIRST`).
- [ ] Fix `Run` and `usageFrom`.

## Acceptance criteria

- R1: a test covers a run killed by a signal and D6's message.
- R2: a test shows usage is kept when a run times out (D5).
- R3: input token sums never overflow to a negative count.
- R4: a run that exits on its own is reported by its exit status, not as a time limit, when the stop time passes while its pipes drain.

## Decisions

- **D1** [gap] §5.1 `usage.input_tokens` (work plan #3 D4): the sum of the input token fields saturates at 9223372036854775807 (max int64) instead of overflowing. Why: the schema needs a non-negative integer and dropping the field would lose spend that was reported.
- **D2** [gap] FR-18 AC4, work plan #3 D2/D6: a run is a time limit only when the stop time arrived while claude itself was still running; a claude that already exited is reported by its own exit status even if the stop time passes while its output drains. Why: the run was never stopped, so its result stands.

## Verification

Repository checks (gofmt, vet, build, tests with -race); also `GOOS=linux go build ./...`.

## Risks and open questions

Between claude exiting and Go reaping it (microseconds) a stop still counts as a time limit; this is the safe side.

## Outcome

_Filled in before shipping: what was done, deviations from this work plan's approach and why, follow-up issues filed. Details the specs should record go in Decisions, not here._
