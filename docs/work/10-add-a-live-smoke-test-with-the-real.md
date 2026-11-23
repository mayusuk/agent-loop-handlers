# Work plan: Add a live smoke test with the real Claude Code

**Issue:** [#10](https://github.com/mayusuk/agent-loop-handlers/issues/10) (feature)
**Branch:** `issue/10-add-a-live-smoke-test-with-the-real`
**Base:** `origin/main @ 0f14a37` (checked before branching)
**Status:** Draft

## Summary

Add `e2e/`, a smoke test that runs `implement`, then `review` on the pushed head, then `address` on the findings, with the real `claude`, against a small Go repository with a bare local remote. It is skipped unless `AGENT_LOOP_LIVE=1` (plan D15). The same harness also runs in every `go test` with the fake `claude`, so the harness itself stays tested in CI.

## Context

- Plan T9 and D15: live only with `AGENT_LOOP_LIVE=1`, local bare repository, no GitHub. CLAUDE.md / `.wf.toml` notes: tests never call the real `claude` or GitHub otherwise.
- INTEGRATION-SPEC §3.1 (handler environment), §3.3 (workspace), §5.2 (the worker numbers new findings `F1`, `F2`, ... in array order), §6.3.
- TECHNICAL-SPEC §7.2 and plan D12, T8 (#9, PR #34): the handler needs `AGENT_LOOP_CLAUDE_SETTINGS` with the sandbox on and required, or `--unsandboxed`.
- The built-in prompts tell the agent to read the item and change with `gh issue view` / `gh pr view`. Offline, these must come from somewhere other than GitHub.
- `contract.ValidateResult` checks each result, and `internal/testing/fakeclaude` is the fake `claude`.

## Approach

- `e2e/harness_test.go`: the test plays the worker. It builds `agent-loop-claude` once in `TestMain`, seeds a bare remote with a small Go module (`calc.go`, a test, `CLAUDE.md`, `.gitignore`), clones a fresh workspace for each stage, writes `input.json` and runs the binary as a subprocess with the handler environment, then reads `result.json` and checks it with `contract.ValidateResult`. A fake `gh` script on `PATH` serves the item and the change (description, numbered findings, one owner comment) from files the harness writes, and fails every other call, so nothing reaches GitHub (D1).
- `e2e/live_test.go`: `TestPlanT9_LiveSmoke`, skipped unless `AGENT_LOOP_LIVE=1`. It uses the real `claude` (from `AGENT_LOOP_CLAUDE_BIN` or `PATH`), sandboxed by default (D4).
- `e2e/dryrun_test.go`: the same flow with the fake `claude` scripted per stage. It always runs.
- README: how to run the live test, what it costs, and the knobs.

## Steps

- [x] Write this work plan.
- [x] Add the harness and the dry run with the fake `claude`.
- [x] Add the live test, skipped unless `AGENT_LOOP_LIVE=1`.
- [ ] Document how to run it in the README.

## Acceptance criteria

- Without `AGENT_LOOP_LIVE=1` the test is skipped and CI stays green.
- With it, the three stages each write a valid result, and the implement head is on the bare remote.

## Decisions

- **D1** [gap] plan T9, D15: the item, the change and the review threads reach the agent through a fake `gh` on `PATH` that answers `gh issue view <item>` and `gh pr view <change>` (any flags) from files the test writes, and exits 1 with a message for any other call. Why: the built-in prompts read them with `gh`, and the test must not call GitHub.
- **D2** [gap] plan T9, INTEGRATION-SPEC §3.3, §5.2: the test plays the worker. Each stage gets a fresh clone of the bare remote: implement on a new `agent/1` from `origin/main`; review detached at the implement head; address on `agent/1` at that head. New findings get IDs `F1`, `F2`, ... in array order, and are written into the `gh pr view` text with score, location, title and body.
- **D3** [gap] plan T9: address runs in `findings` mode answering every review finding plus one canned owner comment `MC-1`. Why: the review may find nothing, and address must still have something to answer.
- **D4** [gap] plan T9, TECHNICAL-SPEC §7.2: the live test runs sandboxed by default, with a settings file it writes (`sandbox.enabled` and `sandbox.failIfUnavailable` true), or the file in `AGENT_LOOP_CLAUDE_SETTINGS` when that is set. `AGENT_LOOP_LIVE_UNSANDBOXED=1` passes `--unsandboxed` instead, for machines without the sandbox. `GOCACHE` and `GOTMPDIR` point into a git-ignored `.cache/` inside each workspace, so a sandboxed agent can run `go test`.
- **D5** [gap] plan T9: the live test passes only when every stage's result is `done`, not just valid: a `failed` review or address would mean the stage does not work. After implement, `done.head` must be the remote `agent/1` and differ from the base; after review the remote is unchanged; after address the remote `agent/1` is `done.head`.
- **D6** [gap] plan T9: each stage gets a 20-minute deadline. The README tells the owner to run with `-timeout 90m`, since `go test` stops at 10 minutes by default.
- **D7** [gap] plan T9: the harness also runs in CI with the fake `claude` (`TestPlanT9_HarnessDryRun`). Why: the live test never runs in CI, so without this its harness would be untested.

## Verification

- `wf.py verify` (gofmt, vet, build, `go test -race`). The live test is not run here (it calls the real Claude); it is confirmed skipped without `AGENT_LOOP_LIVE=1`.

## Risks and open questions

- The live test has not been run by the author. Its first run may show a real gap, such as the sandbox refusing writes to the draft file in `$TMPDIR`. That is the point of the test; a failure there is a handler bug to file, not a test to loosen.

## Outcome

_Filled in before shipping._
