# Work plan: Add the CLI skeleton, input checks and the always-write-a-result guarantee

**Issue:** [#2](https://github.com/mayusuk/agent-loop-handlers/issues/2) (feature)
**Branch:** `issue/2-add-the-cli-skeleton-input-checks-and`
**Base:** `origin/main @ f4b1cb8` (checked before branching)
**Status:** In progress

## Summary

Adds the `agent-loop-claude` program with the subcommands `implement`, `review`, `address`, `mint` and `version`, and `internal/run`, which reads the handler environment (INTEGRATION-SPEC §3.1), checks the input and the contract version, and guarantees that every run ends with a `result.json` that has passed `contract.ValidateResult`, or a `failed` one (FR-18 AC3, plan D2). The stage subcommands are stubs that return `failed` until their tasks land; `mint` returns the D11 stub.

## Context

- Plan `docs/plans/2026-11-16-agent-loop-claude-first-release-v0-1.md` task T1, decisions D2, D7, D11.
- INTEGRATION-SPEC §2 (versioning: a 1.0 handler reads any 1.x input), §3.1 (the four env variables, exit code informational, a missing or invalid result is `failed`), §5.1 (`failed.reason` one line, at most 500 characters), §12.4. PRODUCT-SPEC FR-18 AC1 and AC3.
- agent-loop `contract`: `ParseInput` (rejects another major), `ValidateResult(in, data, outDir)` (returns a `failed` result on any violation), `ParseVersion`, `Version`, `MaxFailedReason`. The result schema requires `run_id` with at least one character and `contract_version` `1.0`.
- `internal/compat` only proved the dependency; it is removed.

## Approach

- `internal/run`:
  - `Main(stage, args, getenv, stderr, body) int` is the whole life of a stage run. It reads `AGENT_LOOP_RESULT` first (without it no result can be written: exit 1). It then reads the input bytes, recovers the `run_id` leniently, checks `AGENT_LOOP_CONTRACT`, parses the input with `contract.ParseInput`, checks the stage and `AGENT_LOOP_OUT`, and calls `body`. Each failed check ends in `failed` with a one-line reason. A deferred `recover` turns a panic anywhere after that into `failed` and logs the stack to stderr (the run log).
  - `(*Run).Finish(res)` sets `contract_version` and `run_id` (handler-owned fields), validates with `contract.ValidateResult` against the input and `AGENT_LOOP_OUT`, substitutes the returned `failed` result when it is rejected, and writes `result.json` atomically (temp file in the same directory, fsync, rename). It is called once; `Main` calls it with what `body` returns.
  - `Failed(reason)` builds a `failed` result with the reason folded to one line and cut to 500 characters.
  - `Contracts` lists the supported contract versions (`1.0`).
- `cmd/agent-loop-claude`: `main` dispatches subcommands. `implement`, `review`, `address` are stubs; `mint` is D11; `version [--json]` prints the release and `run.Contracts`. The release comes from `-ldflags -X main.version=...`, then the module version Go stamps into the build, then `dev`.
- Tests: `internal/run` in-process with a fake environment and temp dirs; `cmd/agent-loop-claude` in-process through `realMain`, plus one test that builds the binary and checks the real exit codes.

## Steps

- [ ] `internal/run`: environment, input and contract checks, `Finish` with validation and atomic write, panic recovery; tests. Remove `internal/compat`.
- [ ] `cmd/agent-loop-claude`: subcommands, stage stubs, `mint` stub, `version`; tests.
- [ ] Outcome, final verify.

## Acceptance criteria

- `version --json` prints `{"version": ..., "contracts": ["1.0"]}`.
- A missing env variable, an unreadable input, a stage that doesn't match the subcommand, and contract `2.0` each end in a `failed` result with a one-line reason, or a non-zero exit when no result path is known (FR-18 AC3).
- `mint` writes a valid `failed` result (D11).
- A result that `ValidateResult` rejects is replaced by its `failed` result before it is written; a panic in a stage still writes `failed`.

## Decisions

None.

## Verification

Standard checks (gofmt, vet, build, race tests). The binary test runs the built program to check exit codes as the worker sees them.

## Risks and open questions

None.

## Outcome

_Filled in before shipping._
