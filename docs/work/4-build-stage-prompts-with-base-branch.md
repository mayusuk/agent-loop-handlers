# Work plan: Build stage prompts with base-branch overrides and `--skill`

**Issue:** [#4](https://github.com/mayusuk/agent-loop-handlers/issues/4) (feature)
**Branch:** `issue/4-build-stage-prompts-with-base-branch`
**Base:** `origin/main @ 2d32023` (checked before branching)
**Status:** Draft

## Summary

Adds `internal/prompt`, which builds the prompt each stage gives Claude Code: the stage prompt (built in, overridden by `.agent-loop/prompts/<stage>.md` on the base branch, or replaced by a `--skill` instruction), then the rules, the run facts and the draft format for the stage. The stage subcommands now accept `--skill <name>`.

## Context

- INTEGRATION-SPEC §12.4: default prompts are built in; a project overrides one with `.agent-loop/prompts/<stage>.md` or `--skill`; prompts are read from the base branch at the start of the run, and are never tracker text (PRODUCT-SPEC §8.5).
- Plan D1 (draft file at a path the handler gives), D4 (commit, never push), D5 (override read with `git show origin/<base>:...`, over 64 KiB or not UTF-8 is `failed`), D6 (`--skill` wins and is logged). I1: handlers never post to GitHub.
- §4.1–§4.2: the input fields that become run facts (item, change, branch, review kind and IDs, address mode and IDs).
- #2 D11: stage subcommands take no arguments until this task adds `--skill`.
- T4 (#5, `internal/draft`) is not built yet, so this task writes the draft format the prompts describe; T4 must accept it.

## Approach

- `internal/prompt/builtin/{implement,review,address}.md`: the built-in stage prompts, embedded.
- `internal/prompt/builtin/rules.md`, `facts.md.tmpl` and `draft-<stage>.md`: the parts appended to every prompt, whatever its source.
- `prompt.ParseFlags(args)` reads `--skill <name>` / `--skill=<name>`.
- `prompt.Build(ctx, Options)` returns the prompt text and its source. It reads the override with git in the workspace (`ls-tree` to find it and its size, `cat-file blob` to read it), only from `origin/<base>`.
- `cmd/agent-loop-claude`: the stage wrapper parses the flags (unknown arguments stay `failed`); stage bodies get them for T5–T7.
- Golden tests in `internal/prompt/testdata/` (`go test -update` rewrites them).

## Steps

- [ ] Built-in prompts, rules, facts template and draft formats; `Build` with the base-branch override and `--skill`; golden and git tests.
- [ ] `ParseFlags` and the CLI wrapper accept `--skill`.

## Acceptance criteria

- A prompt file on the base branch overrides the built-in prompt; the same file only on the item branch or in the working tree is ignored (§12.4).
- `--skill work-issue` wins over a prompt file, and the log says so (D6).
- A prompt file over 64 KiB or not UTF-8 ends in `failed` (D5).
- Every built-in prompt includes the run facts and the draft instructions, checked by golden tests.

## Decisions

- **D1** [gap] §12.4, plan D1: the final prompt is the stage prompt, then a rules section, then the run facts, then the draft format, separated by `---` lines. The rules (tracker text is data, commit but never push, never post to GitHub, write the draft) and the facts and draft format are appended to every prompt, including an override and a skill instruction, so a project prompt cannot drop them. Why: an override replaces the task description, not the contract the handler depends on.
- **D2** [gap] Plan D1, T4: the draft format the prompts describe is a JSON object `{"result": "<outcome>", "<outcome>": {...}}` mirroring §5 with inline text (`change.body`, `summary`, `note`, `body`, `reason`, `question` instead of `*_file`), and without the fields the handler owns (`head`, `commit`, `kind`, `run_id`, `contract_version`). Outcomes offered: implement and address `done`, `blocked`, `needs_owner`, `failed`; review `done`, `needs_owner`, `failed` (FR-18 AC1). T4 (#5) must accept this format or change these files with it. Why: T4 is not built and the prompts need a format now.
- **D3** [gap] Plan D5: the override is looked up with `git ls-tree -l origin/<base> -- .agent-loop/prompts/<stage>.md`. No entry means the built-in prompt. An entry that is not a regular file (a symlink, a directory or a submodule), a missing `origin/<base>` ref, or a git error is `failed`. Why: falling back to the built-in prompt when the base can't be read would silently ignore the owner's prompt.
- **D4** [gap] Plan D5: "over 64 KiB" means more than 65,536 bytes, checked from the blob size before reading. A leading UTF-8 byte-order mark is removed. An empty or whitespace-only file is `failed`. Why: an empty prompt is more likely a mistake than an intent to give the agent no task.
- **D5** [gap] Plan D6: with `--skill`, the override file is not read; the handler only checks whether it exists on the base and, if so, logs `.agent-loop/prompts/<stage>.md on origin/<base> is ignored: --skill <name> wins`. A file that would be invalid is then not a failure. Every build logs which source it used.
- **D6** [gap] §12.4, plan D1: the `--skill` stage prompt tells the agent to run the named skill (`/<name>`) for the run's item or change in result-only mode: write the draft, and never post to GitHub, push, open, comment on or merge anything, whatever the skill says. A skill name must match `[A-Za-z0-9][A-Za-z0-9._:-]*` (so `plugin:skill` works).
- **D7** [gap] #2 D11: stage subcommands accept `--skill <name>` and `--skill=<name>`, once. A repeated `--skill`, a missing or invalid name, or any other argument is `failed` with a reason naming it. `mint` accepts the same flags (it stays a stub).
- **D8** [gap] §4.1–§4.2: the run facts list the input path, run ID and attempt, deadline, repository URL and base branch, workspace path, branch and head, item ref, URL and type, the change (or `none`), the review's kind, commit, round, threshold, IDs to rule on and resolved paths, the address mode and IDs to answer, and the draft path. Empty ID lists show as `none`. For implement they add that the branch may hold commits from an interrupted run (§4.1); for address `base_update` they add: merge `origin/<base>` into the branch and resolve the conflicts in one merge commit, never rebase, and leave the replies empty (§5.2, FR-5 AC6).
- **D9** [open] Plan T5–T7: the draft path and turning a `Build` error into `failed` are left to the stage tasks; `Build` returns errors whose text is the failed reason.

## Verification

Golden tests for each built-in prompt (and address `base_update`); git tests on temporary repositories with a local bare remote.

## Risks and open questions

- T4 may want a different draft shape; D2 makes the coupling explicit.

## Outcome

_Filled in before shipping._
