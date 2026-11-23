# Work plan: Pass the run's sandbox settings to Claude Code

**Issue:** [#9](https://github.com/mayusuk/agent-loop-handlers/issues/9) (feature)
**Branch:** `issue/9-pass-the-run-s-sandbox-settings-to`
**Base:** `origin/main @ b1d71f1` (checked before branching)
**Status:** Draft

## Summary

The handler now refuses to run Claude Code without the run's sandbox. When `AGENT_LOOP_CLAUDE_SETTINGS` is set it checks that the file turns the sandbox on and passes it with `--settings`; when it isn't, the stage ends `failed: no sandbox settings ...` without starting `claude`, unless the new `--unsandboxed` flag is given (for `runner = "local-unsandboxed"`), which runs and logs a warning.

## Context

- Plan T8 (#9), plan D12; TECHNICAL-SPEC §7.2 (the sandbox is required; `local-unsandboxed` opts out and warns). agent-loop's `internal/runners` writes the settings (`sandbox.enabled`, `sandbox.failIfUnavailable` both true) and sets `AGENT_LOOP_CLAUDE_SETTINGS` only for sandboxed runs.
- `internal/claude.Args` already adds `--settings` when given a path; `internal/stage.agent` builds the run for implement and review.

## Approach

- `internal/claude`: `CheckSettings(path)` reads the file and checks the sandbox is on (D1). `Sandbox(settings, unsandboxed)` returns the path to pass, "" for an unsandboxed run, or an error (D2, D3, D5).
- `internal/prompt.Flags`: add `Unsandboxed`, a bare flag (D3); update `Usage`.
- `internal/stage.agent`: resolve the sandbox before finding or starting claude; log the warning (D4) when unsandboxed.
- Tests: the stage fixture gets a valid settings file by default; new stage tests cover each acceptance criterion with the fake.

## Steps

- [ ] Add `CheckSettings`/`Sandbox` in `internal/claude` with unit tests.
- [ ] Add `--unsandboxed` to the flags, and use the sandbox check in `stage.agent`, with stage tests for the acceptance criteria.

## Acceptance criteria

- With the settings variable set, the fake receives `--settings <path>`.
- Without it, the run ends `failed` and `claude` is never started; with `--unsandboxed` it runs and logs a warning.
- A settings file that doesn't enable the sandbox gives `failed`.

## Decisions

- **D1** [gap] Plan T8, TECHNICAL-SPEC §7.2: "enables the sandbox" means the file is a JSON object whose `sandbox.enabled` and `sandbox.failIfUnavailable` are both `true`. A missing, unreadable or non-JSON file, or either field absent or not `true`, gives `failed`. Why: without `failIfUnavailable` Claude Code runs unsandboxed when the sandbox can't start, and §7.2 makes the sandbox required; agent-loop's runner writes both.
- **D2** [gap] Plan T8: the reasons are `no sandbox settings: AGENT_LOOP_CLAUDE_SETTINGS is not set (pass --unsandboxed only for runner = "local-unsandboxed")` and `sandbox settings <path>: <what is wrong>`. An empty variable counts as unset. Why: the issue gives only the prefix.
- **D3** [gap] Plan T8: `--unsandboxed` is a bare flag (no value; `--unsandboxed=x` and a repeat are errors). When the settings variable is also set, the settings are checked and used and the run is sandboxed, with no warning. Why: the flag opts out of requiring settings, not of a sandbox the runner provided.
- **D4** [gap] Plan T8, TECHNICAL-SPEC §7.2: the warning goes to the run log as `warning: running claude without the sandbox (--unsandboxed): it has the owner's full user access: it can read the owner's files, reach any network address, and open the local API socket.` Why: reuses §7.2's words.
- **D5** [gap] Plan D12: a relative settings path is made absolute against the handler's working directory before it is passed, because `claude` runs in the workspace. Why: the runner passes absolute paths, but a relative one would otherwise name a different file.

## Verification

Standard checks; the fake records claude's arguments.

## Risks and open questions

None.

## Outcome

_Filled in before shipping._
