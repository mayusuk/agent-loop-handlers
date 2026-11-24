# Work plan: Write a failed result when Finish itself panics

**Issue:** [#16](https://github.com/mayusuk/agent-loop-handlers/issues/16) (bug)
**Branch:** `issue/16-write-a-failed-result-when-finish`
**Base:** `origin/main @ f463d09` (checked before branching)
**Status:** Draft

## Summary

A panic inside `Run.Finish` itself (encoding, `contract.ValidateResult`, or a log write) used to exit 1 with no `result.json`, because `Main`'s recover saw the run as finished and returned the provisional `ExitNoResult`. `Finish` now recovers its own panic and writes a minimal failed result, unless a result was already written.

## Context

- Plan D2 (`docs/plans/2026-11-16-agent-loop-claude-first-release-v0-1.md`) and FR-18 AC3: the handler always tries to write a result; the exit code is non-zero only when none could be written.
- Root cause: `Finish` sets `r.finished = true` and `r.code = ExitNoResult` before calling `finish`. If `finish` panics, the panic reaches `Main`'s deferred recover, which sees `r.finished` and returns `r.code` (1) without writing anything. Reproduced with a run-log writer that panics on the `result rejected` line.

## Approach

In `internal/run/run.go`:
- `finish` records `r.written` once `writeAtomic` succeeds.
- `Finish` defers a recover. On a panic: if the result was written, keep it and return `ExitResultWritten`; otherwise write a minimal failed result (`internal error: cannot finish the result: <panic>`) through `writeFallback`, which builds it with `Failed`, encodes it and writes it atomically, skipping `ValidateResult` (it may be what panicked), under its own recover. The panic and stack are then logged through a log call that cannot itself panic (`safeLogf`).
- `Main`'s recover logs through `safeLogf` too, so a panicking run log cannot turn a body panic into a crash.

## Steps

- [x] Tests that fail because of the bug: a run-log writer that panics on the rejection path leaves no result; one that panics on the post-write log line must keep the written result and exit 0; a body panic whose Finish also panics still writes failed.
- [ ] Recover in `Finish`, write the fallback result, log safely.

## Acceptance criteria

- A panic inside `Finish` before the result is written ends with a failed `result.json` that passes `ValidateResult`, and exit 0.
- A panic inside `Finish` after the result is written keeps that result and exits 0.
- If even the fallback cannot be written, the exit code is non-zero (`ExitNoResult`).

## Decisions

- **D1** [gap] Plan D2, FR-18 AC3: the fallback failed result written after a panic in `Finish` has the reason `internal error: cannot finish the result: <panic value>` (folded to one line and cut by `Failed`), and is not passed through `contract.ValidateResult`, because the validator may be what panicked; it is schema-valid by construction (contract_version, run_id, `failed`, one-line reason). Why: the plan says an internal error becomes `failed` but not how a panic in the finishing path itself is handled.
- **D2** [gap] Plan D2: a panic in `Finish` after `result.json` was written (for example in the closing log line) keeps that result and exits 0; it is not replaced by a failed result. Why: the written result already passed validation and the worker may have read it; Finish writes at most once per run.

## Verification

Standard checks only; the new tests inject panics through the run-log writer.

## Risks and open questions

None.

## Outcome

_Filled in before shipping._
