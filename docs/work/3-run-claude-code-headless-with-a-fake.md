# Work plan: Run Claude Code headless, with a fake claude for tests

**Issue:** [#3](https://github.com/mayusuk/agent-loop-handlers/issues/3) (feature)
**Branch:** `issue/3-run-claude-code-headless-with-a-fake`
**Base:** `origin/main @ c3c001e` (checked before branching)
**Status:** Done

## Summary

Adds `internal/claude`, which runs Claude Code headless (`claude -p`) in the run's workspace: it builds the command line, finds the binary, closes stdin, runs it in its own process group, stops the group before the input deadline, streams its output to the run log, and reads cost and tokens into `contract.Usage`. Adds `internal/testing/fakeclaude`, a scriptable fake `claude` that tests build in `TestMain`, so no test calls the real CLI.

## Context

- Plan `docs/plans/2026-11-16-agent-loop-claude-first-release-v0-1.md` task T2, decisions D3 (deadline margin, default 2 minutes), D12 (`--settings` from `AGENT_LOOP_CLAUDE_SETTINGS`, `--permission-mode acceptEdits`, never `--dangerously-skip-permissions`), D13 (`--claude`, then `AGENT_LOOP_CLAUDE_BIN`, then `PATH`).
- INTEGRATION-SPEC §3.1: stdout and stderr are the run log and never parsed by the worker; the time limit stops the whole process group (FR-18 AC4). §5.1: `usage` is optional, every field optional.
- TECHNICAL-SPEC §7.1: Claude's final text is only logged, never the result.
- `contract.Usage` (pointers, all optional); `contract.Input.Deadline` is an RFC 3339 string; `Workspace.Path` is the run's worktree.
- `.wf.toml` notes: tests use the fake binary only; no `claude`, API or GitHub calls.

## Approach

`internal/claude`:
- `Find(flag, getenv)` resolves the binary (D13).
- `Args(prompt, settings)` builds `-p --output-format json --permission-mode acceptEdits [--settings <path>] -- <prompt>`.
- `StopAt(deadline, margin)` parses the input deadline and subtracts the margin.
- `Run(ctx, Options) (Outcome, error)` starts the binary with `exec.CommandContext` in `Options.Dir`, stdin from the null device, `Setpgid`. Cancel sends SIGTERM to the group; after a grace period (`WaitDelay`) Go kills the leader, and after `Wait` the handler sends SIGKILL to the group so no child outlives the run. stdout goes to the caller's writer and to a bounded tail buffer; stderr to the caller's writer. The tail is parsed leniently into `Outcome.Usage`. A time limit returns `ErrTimeLimit`; a non-zero exit returns `*ExitError` with the code. `Outcome` is returned in every case, so usage is reported for failed runs too.

`internal/testing/fakeclaude`: a `main` package scripted by `FAKECLAUDE_*` variables (record, child process, shell step, draft, stderr, sleep, ignore SIGTERM, output, exit code). `internal/testing/fakeclaude/fakebin.Build(dir)` builds it for `TestMain`.

Unix only (`Setpgid`, `syscall.Kill`), like the worker's `local` runner; CI runs Linux and macOS.

## Steps

- [x] Fake claude binary and its builder.
- [x] `internal/claude`: binary lookup, command line, deadline, usage parsing, run with process-group stop; tests against the fake for every acceptance criterion.
- [x] Outcome, verify, ship.

## Acceptance criteria

- The fake receives the prompt, runs in the workspace, and has closed stdin; its recorded arguments never contain `--dangerously-skip-permissions`.
- A fake that sleeps past the deadline is stopped with its child processes, and the caller gets a time-limit error (FR-18 AC4).
- Usage is read from the JSON output; missing or malformed output gives no `usage` and no crash.
- A non-zero exit is reported as an error with the exit code.

## Decisions

- **D1** [gap] Plan T2, D12: the prompt is passed as the last argument after `--` (`claude -p --output-format json --permission-mode acceptEdits [--settings <path>] -- <prompt>`), not on stdin, because stdin is closed. Why: `--` keeps a prompt that starts with `-` (for example a `---` front-matter override) from being read as a flag.
- **D2** [gap] Plan D3, FR-18 AC4: at the stop time `claude`'s process group gets SIGTERM, then SIGKILL after a 5-second grace period. After `claude` exits for any reason, the group gets SIGKILL, so no child it started outlives the run. Why: TERM lets Claude Code save its session; the plan's 2-minute margin covers the grace.
- **D3** [gap] Plan D3: a deadline already within the margin (or past) is a time-limit error without starting `claude`; an unparsable deadline is an error. Why: starting would only waste the remaining time and an attempt's spend.
- **D4** [ambiguity] §5.1 `usage`: from Claude Code's JSON output, `cost` is `total_cost_usd` with `currency` `USD`; `input_tokens` is `usage.input_tokens` plus `usage.cache_creation_input_tokens` plus `usage.cache_read_input_tokens`; `output_tokens` is `usage.output_tokens`. A field that is missing, of the wrong type or negative is left out; no usable field means no `usage`. Reading: §5.1's `input_tokens` counts every input token the run spent, cached or not.
- **D5** [gap] Plan T2: usage is parsed from the last 4 MiB of stdout: the whole tail as one JSON object, else the last line that is one. Usage is returned for failed and timed-out runs too, so spend is counted. Why: `--output-format json` prints one object at the end; the bound keeps memory fixed.
- **D6** [gap] Plan T2: a non-zero exit is reported as `claude exited with code N`; a death by signal outside the time limit as `claude was killed by signal S`. After `claude` exits, its output pipes are closed after the grace period even if a child still holds them.

## Verification

Standard checks; also `GOOS=linux go build ./...` and `GOOS=darwin go build ./...`.

## Risks and open questions

- Claude Code's JSON fields may change between versions; parsing is lenient (plan risk).
- After `Wait` reaps `claude`, its PID could in theory be reused as a new group leader before the cleanup SIGKILL; the window is microseconds, and while any child remains in the group the PID can't be reused.

## Outcome

Added `internal/claude` (`Find`, `Args`, `StopAt`, `Run`, `ParseUsage`, `ErrTimeLimit`, `ExitError`, `DefaultMargin`, `DefaultGrace`) and `internal/testing/fakeclaude` with `fakebin.Build`. Tests cover each acceptance criterion against the fake: prompt, workspace, closed stdin, own process group, no skip-permissions flag; a sleeping fake (one that exits on SIGTERM and one that ignores it) is stopped at the stop time with its child and gives `ErrTimeLimit`; usage from the JSON output, and none from empty, malformed or usage-less output; a non-zero exit gives `ExitError` with the code. Also covered: a stop time already passed (no start), a child left after a normal exit is killed, a child holding the output pipe doesn't hang the run, and the caller's own context ending is not reported as a time limit.

No deviations from the approach. Nothing calls `Run` yet: the stage subcommands wire it in T5–T7, and the `--claude` and `--deadline-margin` flags arrive with them. No follow-up issues.
