# Work plan: Live test sandbox follow-ups from the review of #40

**Issue:** [#43](https://github.com/mayusuk/agent-loop-handlers/issues/43) (bug)
**Branch:** `issue/43-live-test-sandbox-follow-ups-from-the`
**Base:** `origin/main @ 8cda152` (checked before branching)
**Status:** Done

## Summary

The live smoke test's fake `gh` now logs its calls inside each stage's allowed temp directory, so a sandboxed live run keeps its gh-call log. The sandbox test asserts the exact egress lists, a new test proves each stage gets its own `TMPDIR`, and a checksum test catches drift between `e2e/sandbox_test.go` and agent-loop's `internal/runners/sandbox.go`.

## Context

- TECHNICAL-SPEC §7.2: writes are allowed only to the worktree, `AGENT_LOOP_OUT`, the result path and a per-run temp directory; the default egress allowlist is the Anthropic API, the code host and the package registries. Work plan 38 D1 (no GitHub host in the live test) and D3 (`run-<stage>/tmp` as `TMPDIR`).
- Root cause of R1: `ghScript` in `e2e/harness_test.go` appends to `$dir/calls.log`, where `$dir` is the fake gh's own directory (`<root>/gh`). That path is not in `allowWrite`, so under the real sandbox the append fails and `ghCalls` returns nothing. The dry run's fake claude runs no sandbox, so CI never saw it.
- R4: `e2e/sandbox_test.go` copies agent-loop's `runners.Settings` shape because the package is internal. The pinned agent-loop module (`9e4f006`) predates `internal/runners/sandbox.go`, so the module cache has nothing to compare against.

## Approach

- R1: the harness passes `AGENT_LOOP_SMOKE_GH_LOG=<run-stage>/tmp/gh-calls.log` to each stage; the fake gh appends there, and logs nothing when it is unset. `ghCalls` concatenates the stages' logs in stage order. A test asserts the log path is inside one of the stage's `allowWrite` paths.
- R2: `TestT7_2_LiveSandboxSettings` compares `allowedDomains`, `deniedDomains` and `permissions.allow` to literal lists.
- R3: a dry run where each stage's fake claude records `$TMPDIR`; each must be that stage's `run-<stage>/tmp`, all distinct.
- R4: bump the agent-loop module to current main (contract unchanged) so its module directory holds `internal/runners/sandbox.go`; a test pins that file's SHA-256 and fails, with a note to update both, when a bump changes it.

## Steps

- [x] Test that the fake gh's log is inside the stage's allowed writes (fails: it is under `<root>/gh`), then move the log.
- [x] Assert the exact egress and permission lists.
- [x] Test that each stage gets its own `TMPDIR`.
- [x] Bump agent-loop and add the drift checksum test; update the README note.

## Acceptance criteria

- R1: the fake gh's call log is under an allowed sandbox write path.
- R2: the egress check asserts the exact list: the Anthropic API and the registries, no GitHub host.
- R3: a test proves each stage gets its own `TMPDIR`.
- R4: a guard fails when agent-loop's `internal/runners/sandbox.go` changes, with a note to update both together.

## Decisions

- **D1** [gap] TECHNICAL-SPEC §7.2, work plan 38 D3: the fake gh logs to the stage's per-run temp directory (`run-<stage>/tmp/gh-calls.log`, named by `AGENT_LOOP_SMOKE_GH_LOG`) rather than widening `allowWrite`. Why: §7.2 allows writes only to the four named paths, so the log goes in one of them instead of adding a fifth.
- **D2** [gap] issue #43 R4: the drift guard is a SHA-256 of agent-loop's `internal/runners/sandbox.go` as found in the required agent-loop module, which this change bumps to agent-loop main (`7f40ba3`; `contract/` is unchanged since `9e4f006`). Why: the old module version has no `sandbox.go`, and a checksum of a vendored fixture would only catch edits to the fixture, not upstream drift.

## Verification

The repository's four checks. The live test is not run (it costs money and needs the real `claude`).

## Risks and open questions

- Claude Code's sandbox might override `TMPDIR` for the agent's commands; the log path is passed in its own variable, so that does not move it.

## Outcome

All four findings fixed in `e2e/` and the README. R1: the fake gh logs to `AGENT_LOOP_SMOKE_GH_LOG`, which the harness sets to `run-<stage>/tmp/gh-calls.log`; `TestT7_2_FakeGhLogIsWritable` failed on the old location and passes now. R2: `TestT7_2_LiveSandboxSettings` asserts the exact `allowedDomains`, `deniedDomains` and `permissions.allow`. R3: `TestT7_2_EachStageGetsItsOwnTempDir`. R4: `TestT7_2_SandboxCopyInStep` pins the SHA-256 of agent-loop's `sandbox.go`; agent-loop is bumped to `7f40ba3` for it, and `go mod tidy` also moved `go 1.25` to `go 1.25.0` and `golang.org/x/text` to v0.33.0. The README's "empty gh-call log" warning is removed. The live test was not run. No follow-up issues.
