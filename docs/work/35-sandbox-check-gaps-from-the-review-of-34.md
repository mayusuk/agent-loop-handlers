# Work plan: Sandbox check gaps from the review of #34

**Issue:** [#35](https://github.com/mayusuk/agent-loop-handlers/issues/35) (bug)
**Branch:** `issue/35-sandbox-check-gaps-from-the-review-of-34`
**Base:** `origin/main @ 4e5abf3` (checked before branching)
**Status:** Done

## Summary

The sandbox settings check now refuses a file that lets commands run outside the sandbox: `sandbox.allowUnsandboxedCommands` must be `false`. The address stage gets the same sandbox tests as implement and review.

## Context

- Review of #34, findings R1 and R2. TECHNICAL-SPEC §7.2: the sandbox is required; `runner = "local-unsandboxed"` is the only opt-out.
- Root cause (R2): `claude.CheckSettings` reads only `sandbox.enabled` and `sandbox.failIfUnavailable` (work plan #9 D1). Claude Code's default for `allowUnsandboxedCommands` is to let the agent retry a command outside the sandbox (work plan #38 Context), so a file with it `true` or absent passes the check but does not keep every command sandboxed. agent-loop's runner (`internal/runners/sandbox.go`) always writes `allowUnsandboxedCommands: false`.
- Root cause (R1): `internal/stage/sandbox_test.go` runs implement and review only; the address stage's sandbox path is untested.

## Approach

- `internal/claude/sandbox.go`: `CheckSettings` also requires `sandbox.allowUnsandboxedCommands` to be present and `false`; error `sandbox settings <path>: sandbox.allowUnsandboxedCommands is not false`.
- Test fixtures that write minimal settings add `"allowUnsandboxedCommands":false`.
- `internal/stage`: `fixture.address` takes extra env pairs like implement and review; sandbox tests cover address.
- README: document the new condition.

## Steps

- [x] _Each step small enough to commit on its own._
- [x] _For a bug, the first step is a test that fails because of it._

## Acceptance criteria

- R1: the address stage has a sandbox test matching implement and review.
- R2: a settings file with `allowUnsandboxedCommands: true` fails the check.

## Decisions

- **D1** [gap] TECHNICAL-SPEC §7.2, work plan #9 D1: the settings check also requires `sandbox.allowUnsandboxedCommands` to be present and `false`; `true` or absent gives `failed` (`sandbox settings <path>: sandbox.allowUnsandboxedCommands is not false`), even with `--unsandboxed`, like the other two fields. Why: absent means Claude Code's default, which lets commands retry outside the sandbox; the issue allowed reject or warn, and rejecting matches "the sandbox is required". agent-loop's runner always writes it as `false`, so its files still pass.

## Verification

The repository's standard checks only.

## Risks and open questions

A runner other than agent-loop's that writes minimal settings without the field will now be refused; agent-loop's own runner writes it.

## Outcome

Done as planned. `CheckSettings` now refuses settings whose `sandbox.allowUnsandboxedCommands` is `true` or absent (D1); the minimal settings fixtures write it as `false`; the README says so (no CHANGELOG entry: `TestI12_4_ChangelogNamesTheRelease` requires the newest section to be a released version, so it waits for the next release). The address stage has a sandbox test: `--settings` passed, no settings or bad settings fail before claude starts and nothing is pushed, `--unsandboxed` runs without `--settings` and warns. The test steps and the fix went in one commit, since the fixture change would otherwise have been a no-op. No follow-up issues.
