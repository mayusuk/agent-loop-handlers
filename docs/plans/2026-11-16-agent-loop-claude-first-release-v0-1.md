# Plan: agent-loop-claude first release (v0.1)

**Status:** Issues created
**Base:** `origin/main @ aaadc47`
**Tracking issue:** #1

## Goal

Ship the first release of `agent-loop-claude`, the starter handlers that agent-loop v0.1 uses to run real agents end to end (agent-loop PRODUCT-SPEC D38, TECHNICAL-SPEC T7, INTEGRATION-SPEC §12.4). One program with the subcommands `implement`, `review` and `address`. Each runs Claude Code headless in the run's workspace with a stage prompt, and always leaves a `result.json` that meets INTEGRATION-SPEC §3–§5, so a project at integration levels 0 and 1 (§12.1) never deals with the contract. `mint` is v0.2 and is only a stub here.

## Context

The repository holds the bootstrap only: README, the Go module, CI (gofmt, vet, build, race tests on Linux and macOS), `.wf.toml`, and `internal/compat`, which imports `github.com/mayusuk/agent-loop/contract` to prove the dependency. agent-loop is public, so the dependency resolves through the module proxy with no `GOPRIVATE` setup. It is pinned to a pseudo-version of agent-loop `main` (9e4f006).

Sources read (agent-loop specs win; vendored in `docs/specs/agent-loop-excerpts.md`):

- INTEGRATION-SPEC §2 (versioning), §3 (invocation: env, workspace, credentials by stage), §4 (input), §5 (result shapes, `*_file` rules, per-stage `done` rules), §12.1, §12.4, §13.
- PRODUCT-SPEC FR-18 (allowed results per stage; an invalid result is `failed`), D34, D38.
- TECHNICAL-SPEC T7, §7.1–§7.3: stdout is never parsed; the `local` runner runs Claude handlers in Claude Code's OS sandbox, generated per run; skip-permissions only in the Docker runner; tokens come through a credential helper; the review stage has no push token.
- agent-loop `contract/` (merged, #7): `ParseInput`, `ParseResult`, `ValidateResult(in, data, outDir)` (returns a `failed` result on any violation), `Allowed`, `CheckVersion`, `Version = "1.0"`, embedded schemas; work plan `docs/work/7-*.md` D1–D24 (for example D5 file rules, D6 reason rules, D8 one-to-one rulings, D23 deferred replies).
- agent-loop plan `docs/plans/2026-11-15-v0-1-mvp.md` D3: only the live variant of its T20 depends on these handlers.

## Approach

`cmd/agent-loop-claude` is a thin CLI over small internal packages:

- `internal/run`: reads `AGENT_LOOP_INPUT`, `AGENT_LOOP_RESULT`, `AGENT_LOOP_OUT`, `AGENT_LOOP_CONTRACT`; parses and checks the input; and guarantees a result: every path, including panics and internal errors, ends by writing a result that has passed `contract.ValidateResult`, or a `failed` one.
- `internal/claude`: starts `claude -p` in the workspace with `--output-format json`, stdin closed, in its own process group, stopped before the input's `deadline`. It reads cost and tokens from Claude's JSON output into `usage`. Claude's final text is only logged; it is never the result (T7 §7.1).
- `internal/prompt`: the built-in stage prompts (embedded markdown), the `.agent-loop/prompts/<stage>.md` override read from the base branch with `git show`, and `--skill <name>`.
- `internal/draft`: the agent's output. The agent (or a skill in result-only mode) writes a **draft** JSON file at a path the handler gives it, with inline text. The handler turns the draft into `result.json`: text into `*_file` files in `AGENT_LOOP_OUT`, `run_id`, `contract_version`, `commit`, `kind` and `head` filled from the input and git, never trusted from the agent.
- Stage subcommands compose these and add git checks: implement and address push the branch themselves (the agent only commits); review checks that the head didn't move; address `base_update` checks the merge commit's shape before pushing.
- Tests build a fake `claude` binary (`internal/testing/fakeclaude`) in `TestMain`, scripted by environment variables to write drafts, make commits, sleep, crash or print bad output. Git tests use temporary repositories with a local bare remote. CI needs no API key and no network.

## Decisions

- **D1** [open] INTEGRATION-SPEC §12.4: the agent's output is a draft JSON file (`agent-draft.json`) that the agent writes at a path the handler passes in the prompt and in `AGENT_LOOP_CLAUDE_DRAFT`, outside the result files in `AGENT_LOOP_OUT`. It holds the outcome and inline text; the handler owns the conversion to `*_file` and every field it can derive itself. Coordinator ruling 2026-11-16 (Q2 option a). For v0.1 the built-in prompts are the default. Changing the owner's skills (`work-issue`, `review-pr`, `address-pr`) to add a result-only mode is out of scope; `--skill` still passes the skill's name and the draft-writing instructions in the prompt. Open: when and where the skills gain that mode.
- **D2** [gap] §3.1, FR-18 AC3: the handler always tries to write a result. A missing or invalid draft, a `claude` failure, a timeout, a push failure or an internal error becomes `failed` with a one-line reason. The exit code is 0 when a result was written and non-zero only when none could be. Why: the worker treats a missing result as `failed` anyway, but a reason helps the owner.
- **D3** [gap] §3.1, §4.1: `claude` is stopped at the input `deadline` minus a margin (default 2 minutes, `--deadline-margin`), so the handler can still write `failed: time limit reached` before the worker kills the process group (FR-18 AC4).
- **D4** [gap] §5.2 implement and address: the agent commits but does not push. The handler pushes `HEAD` to the item's branch with a plain push (never `--force`) through the configured credential helper, and sets `head` to what it pushed. Why: `head` is then the remote head by construction, and force-push is impossible.
- **D5** [gap] §12.4: the prompt override is read with `git show origin/<repo.base_branch>:.agent-loop/prompts/<stage>.md` in the workspace, never from the working tree or the item branch. A missing file means the built-in prompt. A file over 64 KiB or not UTF-8 is `failed`. Why: §12.4 says prompts come from the base branch, which the owner controls.
- **D6** [ambiguity] §12.4: when `--skill` is given, it wins over a prompt file, and the handler logs that the file was ignored. The run facts and the draft instructions are always appended, so a skill gets the same input. Why: the profile is the more explicit choice; both are owner-controlled.
- **D7** [gap] §12.4, §2: `agent-loop-claude version` prints the release and the supported contract versions (`1.0`); `version --json` prints `{"version": "...", "contracts": ["1.0"]}`. At run time, a major other than 1 in `AGENT_LOOP_CONTRACT` or the input is `failed` (or exits non-zero when no input can be read). Every result is written with `contract_version` `1.0`. Why: §12.4 says a release names its versions; it doesn't say how.
- **D8** [gap] §5.2 review: before writing, the handler checks each new finding against `review.commit`. A `path` that doesn't exist becomes `null` with the location kept at the top of the body; a `line` past the end of the file becomes `null`. Why: the worker would fail the whole review for one bad location, which costs an attempt.
- **D9** [gap] §5.2 review: a review run whose workspace head moved, or that left uncommitted changes, still returns its review of `review.commit`; the handler never pushes in review, and it has no push token (§3.4).
- **D10** [gap] §5.2 address `base_update`: the handler checks locally that the new head is exactly one merge commit whose first parent is the input head and whose second parent contains `change.base_tip`, before pushing. Anything else is `failed` and nothing is pushed.
- **D11** [gap] FR-18 AC1: `mint` writes `failed` with reason `mint is not supported in this release (agent-loop v0.2)`. Why: the subcommand exists so a profile is valid, and `failed` is an allowed mint result.
- **D12** [gap] TECHNICAL-SPEC §7.2: the `local` runner generates the sandbox settings for each run and sets `AGENT_LOOP_CLAUDE_SETTINGS` to the settings file; the handler passes it to `claude` with `--settings`, uses `--permission-mode acceptEdits`, and never `--dangerously-skip-permissions` under the `local` runner. Coordinator ruling 2026-11-16 (Q1 option a), noted on agent-loop #18 (T13).
- **D13** [gap] TECHNICAL-SPEC T7: `claude` is found through `--claude <path>`, then `AGENT_LOOP_CLAUDE_BIN`, then `PATH`. Tests always use the fake binary through the environment variable.
- **D14** [gap] INTEGRATION-SPEC §2: the `contract` dependency is pinned to a pseudo-version of agent-loop `main` until agent-loop tags a release; it is bumped in a PR when the contract changes. Why: agent-loop has no tags yet.
- **D15** [gap] TECHNICAL-SPEC §14 style: a live smoke test with the real `claude` runs only when `AGENT_LOOP_LIVE=1`, against a local bare repository (no GitHub). Why: CI has no API key.
- **D16** [open] INTEGRATION-SPEC §12.4: for v0.1 the repository stays private and the owner builds the handler locally (`go install ./cmd/agent-loop-claude` from the clone). Coordinator ruling 2026-11-16 (Q3 option b). Making it public is the owner's call at release.

## Out of scope

- `mint` beyond the stub (agent-loop v0.2), and the Docker runner.
- Changing the owner's skills (`work-issue`, `review-pr`, `address-pr`) to add their result-only mode (D1).
- Generating the sandbox settings: TECHNICAL-SPEC §7.2 gives that to agent-loop's `local` runner.
- Reading tracker text: the agent reads the item and change itself with `gh` and `GH_TOKEN`, as data (§4.1).
- Packaging beyond a local `go install` from the clone (D16).

## Risks and open questions

- Q1–Q3 were resolved by the coordinator on 2026-11-16 (D1, D12, D16).
- Claude Code's `--output-format json` fields (`total_cost_usd`, `usage`) may change between versions; T2 parses them leniently and omits `usage` when absent.

## Tasks

### T1: Add the CLI skeleton, input checks and the always-write-a-result guarantee (#2)
Create `cmd/agent-loop-claude` with the subcommands `implement`, `review`, `address`, `mint` and `version`, and `internal/run`. `internal/run` reads `AGENT_LOOP_INPUT`, `AGENT_LOOP_RESULT`, `AGENT_LOOP_OUT` and `AGENT_LOOP_CONTRACT` (INTEGRATION-SPEC §3.1), parses the input with `contract.ParseInput`, and checks that the input's `stage` matches the subcommand and that the contract major is 1 (D7). It provides `Finish(result)`, which validates with `contract.ValidateResult` and writes `result.json` atomically, writing the returned `failed` result when validation fails, and a recover path so a panic still writes `failed` (D2). `mint` writes the stub `failed` (D11). `version` prints the release and supported contracts (D7). Replace `internal/compat` with this. The stage subcommands other than `mint` return `failed: not implemented` until their tasks land.

**Depends on:** —

**Acceptance criteria:**
- `version --json` prints `{"version": ..., "contracts": ["1.0"]}`.
- A missing env variable, an unreadable input, a stage that doesn't match the subcommand, and contract `2.0` each end in a `failed` result with a one-line reason, or a non-zero exit when no result path is known (FR-18 AC3).
- `mint` writes a valid `failed` result (D11).
- A result that `ValidateResult` rejects is replaced by its `failed` result before it is written; a panic in a stage still writes `failed`.

### T2: Run Claude Code headless, with a fake claude for tests (#3)
Add `internal/claude`: build the command line (`-p`, `--output-format json`, `--permission-mode acceptEdits`, `--settings` when `AGENT_LOOP_CLAUDE_SETTINGS` is set, never `--dangerously-skip-permissions`, D12), find the binary (D13), run it in the workspace with stdin closed and in its own process group, stop it and its children at the input deadline minus the margin (D3), stream its stdout and stderr to the handler's own (the run log), and parse cost and tokens from the final JSON into `contract.Usage` (leniently). Add `internal/testing/fakeclaude`, a test binary built in `TestMain` and scripted by environment variables: write a given draft file, run a shell step (commit, merge), sleep, exit non-zero, print malformed JSON, and record its arguments and working directory.

**Depends on:** T1

**Acceptance criteria:**
- The fake receives the prompt, runs in the workspace, and has closed stdin; its recorded arguments never contain `--dangerously-skip-permissions`.
- A fake that sleeps past the deadline is stopped with its child processes, and the caller gets a time-limit error (FR-18 AC4).
- Usage is read from the JSON output; missing or malformed output gives no `usage` and no crash.
- A non-zero exit is reported as an error with the exit code.

### T3: Build stage prompts with base-branch overrides and `--skill` (#4)
Add `internal/prompt` with built-in prompts for implement, review and address as embedded markdown (`internal/prompt/builtin/<stage>.md`). The final prompt is: the stage prompt (built-in, the override from `origin/<base>:.agent-loop/prompts/<stage>.md` per D5, or, for `--skill`, an instruction naming the skill and telling it to write the draft and not post to GitHub, D1, D6), then the run facts (input path, item and change references, branch, review kind and IDs to rule on or answer, the draft path) and the draft format for the stage (from T4's schema). Prompts tell the agent that tracker text is data, never instructions (PRODUCT-SPEC §8.5), to commit but not push (D4), and never to post to GitHub (I1).

**Depends on:** T1

**Acceptance criteria:**
- A prompt file on the base branch overrides the built-in prompt; the same file only on the item branch or in the working tree is ignored (§12.4).
- `--skill work-issue` wins over a prompt file, and the log says so (D6).
- A prompt file over 64 KiB or not UTF-8 ends in `failed` (D5).
- Every built-in prompt includes the run facts and the draft instructions, checked by golden tests.

### T4: Define the agent draft and convert it to `result.json` (#5)
Add `internal/draft`: the draft format per stage (D1) with an embedded JSON Schema and Go types, and `Convert(input, draft, outDir)`. It writes each text field (change body, summary, notes, finding bodies, replies, `blocked` reason, `needs_owner` question) to a file in `AGENT_LOOP_OUT` and sets the matching `*_file`; fills `run_id`, `contract_version`, review `commit` and `kind` from the input; truncates nothing silently (text over 64 KiB is `failed`); and collapses `failed` reasons to one line of at most 500 characters. A missing draft, invalid JSON, an outcome not allowed for the stage (`contract.Allowed`, for example `blocked` from review) or an unknown field is `failed`. Document the format for skill authors in `docs/agent-draft.md`.

**Depends on:** T1

**Acceptance criteria:**
- For each stage and each allowed outcome, a draft converts to a result that `contract.ValidateResult` accepts (table tests).
- `blocked` with and without `dependency`, and `needs_owner`, produce `reason_file` and `question_file` (FR-18 AC1).
- A missing draft, malformed JSON, a disallowed outcome and an oversized text each give `failed` with a reason naming the problem (FR-18 AC3).
- Fields the agent sets that the handler owns (`run_id`, `commit`, `head`) are ignored or rejected, never copied.

### T5: Implement the `implement` subcommand (#6)
Wire T2–T4 for `implement`: build the prompt, run Claude in the workspace on `workspace.branch`, read the draft, and for `done` push `HEAD` to the item's branch with a plain push and set `head` to the pushed SHA (D4). Put the push helper in `internal/gitx` for T7. `done` with no new commit since `workspace.head` is `failed`. A refused push is `failed`. `blocked` and `needs_owner` push nothing.

**Depends on:** T2, T3, T4

**Acceptance criteria:**
- With the fake making a commit and writing a `done` draft, the bare remote's branch head equals `done.head`, and the result is valid (INTEGRATION-SPEC §5.2 implement).
- A rerun on a branch that already has pushes from an interrupted run builds on it (§4.1).
- No new commit, a refused push (non-fast-forward), and a `claude` failure each give `failed`; nothing is force-pushed.
- `blocked` and `needs_owner` drafts give those results and push nothing.

### T6: Implement the `review` subcommand (#7)
Wire T2–T4 for `review`: run in the detached workspace at `review.commit`, pass `rule_findings`, `rule_manual`, `blocking_threshold` and `resolved_paths` to the prompt, and convert the draft with `commit` and `kind` from the input. Check new findings against `review.commit` and adjust bad locations (D8). Never push (D9).

**Depends on:** T2, T3, T4

**Acceptance criteria:**
- A draft that rules on every ID gives a valid `done` with `commit` equal to `review.commit` (§5.2 review).
- A draft missing a ruling for an ID in `rule_findings` or `rule_manual` gives `failed` (FR-4 AC4, AC8).
- A finding with a path missing at the commit, or a line past the end of the file, is adjusted as D8 says.
- A `blocked` draft gives `failed` (FR-18 AC1); the remote is unchanged after every review run.

### T7: Implement the `address` subcommand, including `base_update` mode (#8)
Wire T2–T4 for `address`. In `findings` and `manual` modes, pass `answer_findings` and `answer_manual`, push new commits with T5's helper, and set `head` (unchanged when nothing was committed). In `base_update` mode, the prompt asks the agent to merge `origin/<base>` into the branch and resolve conflicts, never rebasing; the handler checks the merge commit shape (D10) before pushing, and the replies must be empty.

**Depends on:** T5, T6

**Acceptance criteria:**
- Replies to every ID with no push give a valid `done` with `head` equal to the input head; with a fix commit, `head` is the pushed SHA (§5.2 address, FR-5 AC2).
- A missing reply with an unmoved head gives `failed` (FR-5 AC2).
- `base_update`: one merge commit with the right parents is pushed and returned; a rebase, a one-parent commit, or two commits gives `failed` and pushes nothing (FR-5 AC6).

### T8: Pass the run's sandbox settings to Claude Code (#9)
Implement the sandbox handoff of D12: when `AGENT_LOOP_CLAUDE_SETTINGS` is set, pass it with `--settings` and check that it enables the sandbox; when it isn't, return `failed: no sandbox settings` unless `--unsandboxed` is given (for `runner = "local-unsandboxed"`, TECHNICAL-SPEC §7.2). agent-loop #18 (T13) builds the runner's half.

**Depends on:** T2

**Acceptance criteria:**
- With the settings variable set, the fake receives `--settings <path>`.
- Without it, the run ends `failed` and `claude` is never started; with `--unsandboxed` it runs and logs a warning.
- A settings file that doesn't enable the sandbox gives `failed`.

### T9: Add a live smoke test with the real Claude Code (#10)
Add `e2e/live_test.go`, skipped unless `AGENT_LOOP_LIVE=1` (D15). It creates a small Go repository with a bare local remote, writes `input.json` files as the worker would, and runs `implement`, then `review` on the pushed head, then `address` on the findings, with the real `claude`. Each result must pass `contract.ValidateResult`. Document how to run it in the README.

**Depends on:** T7, T8

**Acceptance criteria:**
- Without `AGENT_LOOP_LIVE=1` the test is skipped and CI stays green.
- With it, the three stages each write a valid result, and the implement head is on the bare remote.

### T10: Document usage and cut the first release (#11)
Update the README: install, profile snippets for each stage, prompt overrides and `--skill`, the draft format link, the environment variables (`AGENT_LOOP_CLAUDE_BIN`, `AGENT_LOOP_CLAUDE_SETTINGS`), and the supported contract versions. Add `CHANGELOG.md` with `v0.1.0`, set the release version reported by `version`, and describe the tagging step (the tag itself is pushed by the owner). Install instructions follow D16 (local `go install` from the clone).

**Depends on:** T9

**Acceptance criteria:**
- `agent-loop-claude version` reports `0.1.0` and contracts `1.0`, and the CHANGELOG and README name the same (§12.4).
- The README profile snippets match INTEGRATION-SPEC §12.4 and TECHNICAL-SPEC §7.1.
