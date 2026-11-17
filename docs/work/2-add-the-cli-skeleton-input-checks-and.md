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

- [x] `internal/run`: environment, input and contract checks, `Finish` with validation and atomic write, panic recovery; tests. Remove `internal/compat`.
- [x] `cmd/agent-loop-claude`: subcommands, stage stubs, `mint` stub, `version`; tests.
- [ ] Outcome, final verify.

## Acceptance criteria

- `version --json` prints `{"version": ..., "contracts": ["1.0"]}`.
- A missing env variable, an unreadable input, a stage that doesn't match the subcommand, and contract `2.0` each end in a `failed` result with a one-line reason, or a non-zero exit when no result path is known (FR-18 AC3).
- `mint` writes a valid `failed` result (D11).
- A result that `ValidateResult` rejects is replaced by its `failed` result before it is written; a panic in a stage still writes `failed`.

## Decisions

- **D1** [gap] INTEGRATION-SPEC §3.1, plan D2: the checks run in this order and the first failure is the result: `AGENT_LOOP_RESULT` set (else exit 1, nothing written), `AGENT_LOOP_INPUT` set and readable, `AGENT_LOOP_CONTRACT` set with major 1, the input parses (`contract.ParseInput`) and has a `run_id`, the input's `stage` matches the subcommand, `AGENT_LOOP_OUT` set and an existing directory. Why: the result path is needed to report anything; the contract is checked before parsing so a `2.0` worker gets a reason about the contract, not about the input.
- **D2** [gap] INTEGRATION-SPEC §5.1 (`run_id` is required and non-empty): a `failed` result written when the input gives no `run_id` (unset, unreadable, not JSON, or no string `run_id`) uses `run_id` `unknown`. When the input is JSON with a string `run_id` but fails a later check, that `run_id` is used. Why: the worker treats a mismatched `run_id` as `failed` anyway, and the reason in the log still helps the owner.
- **D3** [ambiguity] INTEGRATION-SPEC §2, plan D7: "a major other than 1" is read as: any `1.x` in `AGENT_LOOP_CONTRACT` or the input is accepted, including minors above `1.0`, because a 1.0 handler ignores fields a later minor adds; a value that isn't `MAJOR.MINOR` is `failed`. The result is always written as `1.0`.
- **D4** [gap] INTEGRATION-SPEC §3.1: `AGENT_LOOP_OUT` must name an existing directory before the stage runs; otherwise `failed`. Why: every `*_file` result needs it, so failing early costs less than failing after the agent ran.
- **D5** [gap] plan D2, plan Approach (`internal/draft`): `Finish` always sets `contract_version` to `1.0` and `run_id` to the input's, whatever the stage set. Why: the handler owns these fields; the agent never supplies them.
- **D6** [gap] INTEGRATION-SPEC §5.1: a `failed.reason` the handler builds is folded to one line (runs of whitespace, including newlines, become one space), cut to 500 characters ending in `…`, and an empty reason becomes `unknown error`. A panic is `internal error: <panic value>`, with the stack written to stderr (the run log). Why: §5.1 limits the reason to one line of 500 characters but doesn't say how a handler gets there.
- **D7** [gap] INTEGRATION-SPEC §3.1: `result.json` is written through a temporary file in the same directory, synced and renamed over the path (mode 0644), replacing any file already there. The result is written once per run; a second `Finish` is logged and ignored. Why: the worker must never read a partial result, and the first result is the one the stage meant.
- **D8** [gap] INTEGRATION-SPEC §3.1, plan D2: exit codes are 0 when a result was written (whatever the outcome), 1 when none could be (`AGENT_LOOP_RESULT` unset, or the write failed), and 2 for a command line outside a stage run that the program doesn't understand (no command, an unknown command, a bad `version` argument), which writes nothing. `help`, `-h` and `--help` print the usage to stdout and exit 0. Why: §3.1 makes the exit code informational; 2 is the usual usage-error code.
- **D9** [gap] INTEGRATION-SPEC §12.4, plan D7: `version` prints `agent-loop-claude <release>` and `contract versions: 1.0` on two lines; `version --json` prints one compact line, `{"version":"<release>","contracts":["1.0"]}`. The release is the `-ldflags "-X main.version=..."` value, else the module version Go stamps into the build, else `dev`. Why: plan D7 fixes the JSON keys but not the text form or where the release string comes from.
- **D10** [gap] plan T1: until their tasks land, `implement`, `review` and `address` return `failed` with reason `the <stage> stage is not implemented in this release`.
- **D11** [gap] INTEGRATION-SPEC §12.4: in this release the stage subcommands take no arguments. Any argument is `failed` with reason `<stage> takes no arguments in this release, got "<args>"`, after the input checks. T3 (#4) adds `--skill`. Why: a profile written for a later release then fails with a reason instead of the option being silently ignored.

## Verification

Standard checks (gofmt, vet, build, race tests). The binary test runs the built program to check exit codes as the worker sees them.

## Risks and open questions

None.

## Outcome

_Filled in before shipping._
