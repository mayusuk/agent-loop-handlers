# Work plan: Live smoke test sandbox must use the full §7.2 settings (security)

**Issue:** [#38](https://github.com/mayusuk/agent-loop-handlers/issues/38) (bug)
**Branch:** `issue/38-live-smoke-test-sandbox-must-use-the`
**Base:** `origin/main @ 9c15413` (checked before branching)
**Status:** Done

## Summary

The live smoke test's default sandbox settings only turned the sandbox on, so a sandboxed agent could still read `~/.netrc` or run the real `gh` by its full path and reach GitHub. The harness now writes, for each stage, the full TECHNICAL-SPEC §7.2 settings in the shape agent-loop's `internal/runners/sandbox.go` generates: writes limited to the run, the credential stores denied, no GitHub hosts in egress, loopback denied, and no unsandboxed commands.

## Context

- Found in review R1 (score 4, security) of #37, unresolved after 3 cycles; issue #38.
- Root cause: `sandboxSettings` in `e2e/harness_test.go` wrote `{"sandbox":{"enabled":true,"failIfUnavailable":true}}` and nothing else. Claude Code's defaults then allow reads of the whole home directory and any network host, and allow the agent to retry a command outside the sandbox. The environment isolation of work plan 10 D8 (no `gh` on `PATH`, fresh `HOME`, no ssh agent, file-only git) hides credentials but is not a boundary: absolute paths still reach them.
- TECHNICAL-SPEC §7.2 lists what the `local` runner's settings contain. agent-loop generates them in `internal/runners/sandbox.go` (`Settings`, `DeniedReads`, `LoopbackDomains`). This repository must not import agent-loop's `internal/` packages, so the harness copies their shape.

## Approach

- `e2e/sandbox_test.go`: a copy of the settings types and generator (`liveSettings`), with the live test's paths: writes only to the stage's workspace, out directory, result path and a per-stage temp directory (`TMPDIR`); reads denied for the owner's credential stores and agent-loop's config and state, under both the owner's real home and the handler's `HOME`; egress the §7.2 default minus the code host; loopback denied; `allowUnsandboxedCommands: false`; bypass-permissions mode disabled; matching `Read(...)` deny and `WebFetch(domain:...)` allow rules.
- `e2e/harness_test.go`: replace `sandboxSettings` with a `sandbox` switch. With it on, `run` writes the stage's settings next to its input and passes them in `AGENT_LOOP_CLAUDE_SETTINGS`. The live test (by default) and the dry runs turn it on. `AGENT_LOOP_CLAUDE_SETTINGS` and `AGENT_LOOP_LIVE_UNSANDBOXED=1` behave as before.
- README and work plan 10 D4 and D8 corrected.

## Steps

- [x] Add `TestT7_2_LiveSandboxSettings`, which fails against the old settings, and the generator that makes it pass; use it in `run`.
- [x] Correct the README and work plan 10 D4 and D8.

## Acceptance criteria

- The live test's sandbox denies reads of `~/.netrc`, `~/.ssh`, `~/.config/gh` and the keychain paths.
- Its egress allowlist names no GitHub host.
- It sets `allowUnsandboxedCommands: false`.
- D8 describes the sandbox as the boundary, with these settings.
- A test asserts these settings.

## Decisions

- **D1** [gap] work plan 10 D4, TECHNICAL-SPEC §7.2: the live test's egress allowlist is §7.2's default allowlist without the code host's hosts (`api.anthropic.com` and the package registries). Why: the test's remote is a local bare repository and its `gh` is a fake, so it needs no GitHub host, and allowing one would let the real `gh` reach the owner's account.
- **D2** [gap] work plan 10 D8, TECHNICAL-SPEC §7.2: the denied reads are agent-loop's `DeniedReads` with no deployment, for both the owner's home and the handler's fresh `HOME` (whose `Library/Keychains` links the owner's). The live test has no credential helper, so `allowUnixSockets` is empty. Why: the harness has no deployment config, and the agent can reach the owner's home by absolute path.
- **D3** [gap] TECHNICAL-SPEC §7.2 "per-run temp directory": each stage gets `run-<stage>/tmp`, passed as `TMPDIR` and allowed for writes. Why: the harness set no temp directory before, and the settings allow writes only to named paths.

## Verification

`wf.py verify`. The live test is not run (the issue forbids it until this is merged); the dry runs exercise the generated settings through the handler's `CheckSettings`.

## Risks and open questions

- The stricter settings may stop the real claude from doing something the live test needs (for example writing outside the workspace). That shows up only in a live run, which is the owner's call after merge.

## Outcome

The harness writes each stage's §7.2 settings (`e2e/sandbox_test.go`, `liveSettings`, a copy of agent-loop's `runners.Settings` shape) and passes them to the handler; `TestT7_2_LiveSandboxSettings` asserts them, and the dry runs pass them through the handler's `CheckSettings`. README and work plan 10 D4, D8 corrected. Deviation: the step-1 test could not fail against the old code on its own, since the generator it calls is new; it fails on the old settings' content (no denials, unsandboxed commands allowed). The live test was not run. No follow-up issues.
