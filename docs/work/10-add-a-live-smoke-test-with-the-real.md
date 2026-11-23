# Work plan: Add a live smoke test with the real Claude Code

**Issue:** [#10](https://github.com/mayusuk/agent-loop-handlers/issues/10) (feature)
**Branch:** `issue/10-add-a-live-smoke-test-with-the-real`
**Base:** `origin/main @ 0f14a37` (checked before branching)
**Status:** Done

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
- [x] Document how to run it in the README.

## Acceptance criteria

- Without `AGENT_LOOP_LIVE=1` the test is skipped and CI stays green.
- With it, the three stages each write a valid result, and the implement head is on the bare remote.

## Decisions

- **D1** [gap] plan T9, D15: the item, the change and the review threads reach the agent through a fake `gh` on `PATH` that answers `gh issue view <item>` and `gh pr view <change>` (any flags) from files the test writes, and exits 1 with a message for any other call. Why: the built-in prompts read them with `gh`, and the test must not call GitHub.
- **D2** [gap] plan T9, INTEGRATION-SPEC §3.3, §5.2: the test plays the worker. Each stage gets a fresh clone of the bare remote: implement on a new `agent/1` from `origin/main`; review detached at the implement head; address on `agent/1` at that head. New findings get IDs `F1`, `F2`, ... in array order, and are written into the `gh pr view` text with score, location, title and body.
- **D3** [gap] plan T9: address runs in `findings` mode answering every review finding plus one canned owner comment `MC-1`. Why: the review may find nothing, and address must still have something to answer.
- **D4** [gap] plan T9, TECHNICAL-SPEC §7.2: the live test runs sandboxed by default, with the TECHNICAL-SPEC §7.2 settings it writes for each stage (corrected by #38: earlier only `sandbox.enabled` and `sandbox.failIfUnavailable`; see work plan 38), or the file in `AGENT_LOOP_CLAUDE_SETTINGS` when that is set. `AGENT_LOOP_LIVE_UNSANDBOXED=1` passes `--unsandboxed` instead, for machines without the sandbox. `GOCACHE` and `GOTMPDIR` point into a git-ignored `.cache/` inside each workspace, so a sandboxed agent can run `go test`.
- **D5** [gap] plan T9: the live test passes only when every stage's result is `done`, not just valid: a `failed` review or address would mean the stage does not work. After implement, `done.head` must be the remote `agent/1` and differ from the base; after review the remote is unchanged; after address the remote `agent/1` is `done.head`.
- **D6** [gap] plan T9: each stage gets a 20-minute deadline. The README tells the owner to run with `-timeout 90m`, since `go test` stops at 10 minutes by default.
- **D7** [gap] plan T9: the harness also runs in CI with the fake `claude` (`TestPlanT9_HarnessDryRun`). Why: the live test never runs in CI, so without this its harness would be untested.
- **D8** [gap] plan T9, D15, INTEGRATION-SPEC §3.1 (review R1, rounds 1 and 2): the handler environment carries no GitHub credentials, as the worker's does not. The harness drops every `GH_*`, `GITHUB_*`, `GIT_*`, `SSH_*` and `XDG_*` variable, `HOME`, `NETRC` and `CURL_HOME`, then sets: a `PATH` with the fake `gh` first and no directory that holds the real `gh` (such a directory is replaced by a directory of symlinks to everything else in it, so `git`, `go` and `claude` still run); a fresh `HOME` that links only `~/.claude`, `~/.claude.json` and `~/Library/Keychains` (the real `claude` keeps its login in the macOS login keychain, which `security` finds through `HOME`), so `~/.netrc`, `~/.ssh`, `~/.gitconfig`, `~/.git-credentials` and `~/.config/gh` are absent; an empty `GH_CONFIG_DIR`; `GIT_CONFIG_GLOBAL` an empty file with `GIT_CONFIG_NOSYSTEM=1` (no credential helper); `GIT_ALLOW_PROTOCOL=file` (git never runs curl, which reads `~/.netrc`, or ssh); `GIT_SSH_COMMAND` a script that refuses; and `SSH_AUTH_SOCK` unset. Limits, accepted: this is isolation by environment, not by filesystem. An agent that names the real `gh` by its absolute path (for example `/opt/homebrew/bin/gh`) can still read gh's token from the login keychain, which stays reachable because claude needs it, and an agent can read the owner's real home by its absolute path. The Claude Code sandbox, on by default in the live test, is the boundary for those, with the full TECHNICAL-SPEC §7.2 settings (corrected by #38): reads of `~/.netrc`, `~/.ssh`, `~/.config/gh`, `~/Library/Keychains` and the other credential stores denied under both the owner's home and the handler's `HOME`, no GitHub host in the egress allowlist (so the real `gh` can't reach GitHub), loopback denied, and `allowUnsandboxedCommands: false`; `AGENT_LOOP_LIVE_UNSANDBOXED=1` gives the agent the owner's full user access, as the README says.

## Verification

- `wf.py verify` (gofmt, vet, build, `go test -race`). The live test is not run here (it calls the real Claude); it is confirmed skipped without `AGENT_LOOP_LIVE=1`.

## Risks and open questions

- The live test has not been run by the author. Its first run may show a real gap, such as the sandbox refusing writes to the draft file in `$TMPDIR`. That is the point of the test; a failure there is a handler bug to file, not a test to loosen.

## Outcome

Added `e2e/` with the harness, `TestPlanT9_LiveSmoke` (skipped unless `AGENT_LOOP_LIVE=1`), `TestPlanT9_HarnessDryRun` and `TestPlanT9_GhRefusesOtherCalls`, and a README section on running the live test. The approach is as planned. The live test was not run by the author (it calls the real Claude); without `AGENT_LOOP_LIVE=1` it reports SKIP. No follow-up issues filed.

Review round 1: R1 (security) fixed. The handler environment no longer carries the owner's GitHub tokens, gh login or git credential helper (D8), proven by `TestPlanT9_HandlerGetsNoGitHubCredentials`. No pushbacks.

Review round 2: R1 (security) fixed in full. The real `gh` is off the agent's `PATH`, `HOME` is a fresh directory that links only claude's config and keychain (no `~/.netrc` or `~/.ssh`), `SSH_AUTH_SOCK` is unset, git speaks only the file protocol and `GIT_SSH_COMMAND` refuses (D8 rewritten, with its limits). `TestPlanT9_HandlerGetsNoGitHubCredentials` now also fails if a second `gh` is on `PATH`, `SSH_AUTH_SOCK` is set, `~/.netrc` or `~/.ssh` is reachable, or git can use https or ssh. No pushbacks.
