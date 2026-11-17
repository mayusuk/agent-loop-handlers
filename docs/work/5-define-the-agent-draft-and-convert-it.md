# Work plan: Define the agent draft and convert it to `result.json`

**Issue:** [#5](https://github.com/mayusuk/agent-loop-handlers/issues/5) (feature)
**Branch:** `issue/5-define-the-agent-draft-and-convert-it`
**Base:** `origin/main @ e2bc54d` (checked before branching)
**Status:** In progress

## Summary

Add `internal/draft`: the agent's draft format per stage, as an embedded JSON Schema and Go types, and `Convert`, which turns a draft into a `result.json` result: text goes to files in `AGENT_LOOP_OUT`, the handler-owned fields come from the input, and every problem with the draft is a `failed` result whose reason names it. Document the format for skill authors in `docs/agent-draft.md`.

## Context

- Plan T4 and D1: the agent writes a draft JSON file; the handler owns the conversion to `*_file` and every field it can derive.
- #4 D2 (spec issue #20): the prompts in `internal/prompt/builtin/draft-*.md.tmpl` already describe the format: `{"result": "<outcome>", "<outcome>": {...}}` mirroring INTEGRATION-SPEC §5 with inline text and without `head`, `commit`, `kind`, `run_id`, `contract_version`. This work accepts that format unchanged.
- `contract.ValidateResult` (called by `run.Finish`) still checks the converted result: ID sets, reply kinds, SHAs. The draft layer checks shape and text; it does not repeat those rules.
- `run.Failed` already folds a reason to one line of at most 500 characters.

## Approach

- `internal/draft/agent-draft.schema.json` (embedded): `$defs` for each stage and outcome (`implement_done`, `review_done`, `address_done`, `blocked`, `needs_owner`, `failed`), and per-stage `oneOf` defs (`implement`, `review`, `address`) for skill authors.
- `Parse(stage, data)`: UTF-8 and size checks, JSON object, `result` is a string allowed for the stage (`contract.Allowed`), handler-owned keys rejected with a clear reason, then the document is validated against the stage-and-outcome def (so errors point at one field), then decoded into Go types and text sizes checked.
- `Convert(in, draftPath, outDir)`: read the draft (missing or not a regular file is `failed`), `Parse`, then write each text field to a file and build the `contract.Result`. `head` is left empty for implement and address; the stage sets it after the push (T5, T7). Review `commit` and `kind`, `run_id` and `contract_version` come from the input.
- A test decodes every JSON example in the prompt templates and checks it parses, so the prompts and the schema can't drift.

## Steps

- [ ] Schema, types and `Parse`, with tests for each shape and each rejection.
- [ ] `Convert`, with table tests that every allowed outcome per stage gives a result `contract.ValidateResult` accepts.
- [ ] Prompt-example consistency test and `docs/agent-draft.md`.

## Acceptance criteria

- For each stage and each allowed outcome, a draft converts to a result that `contract.ValidateResult` accepts (table tests).
- `blocked` with and without `dependency`, and `needs_owner`, produce `reason_file` and `question_file` (FR-18 AC1).
- A missing draft, malformed JSON, a disallowed outcome and an oversized text each give `failed` with a reason naming the problem (FR-18 AC3).
- Fields the agent sets that the handler owns (`run_id`, `commit`, `head`) are ignored or rejected, never copied.

## Decisions

- **D1** [gap] Plan T4, #4 D2: the draft format is the one #4's prompts describe, unchanged. Handler-owned fields (`run_id`, `contract_version` at the top; `head`, `commit`, `kind` in `done`) are rejected, not ignored, with a reason naming the field; `*_file` fields are rejected as unknown. Why: the prompts say not to add them, and §2 rejects unknown result fields so mistakes show early.
- **D2** [gap] Plan T4: `Convert` leaves `done.head` empty for implement and address; the stage sets it to the pushed SHA (or the unchanged head) after its push (T5, T7). Why: the head is only known after the push, which happens after the draft is read.
- **D3** [gap] Plan T4: the draft must be a regular file (a symlink is rejected), valid UTF-8, and at most 1 MiB. Why: each text is capped at 64 KiB, and a bounded read keeps a runaway agent from exhausting memory.
- **D4** [gap] Plan T4: required texts (`change.title`, `change.body`, `summary`, finding `title` and `body`, reply `body`, `blocked.reason`, `needs_owner.question`) must contain a non-space character, and titles must be one line. Optional fields (`note`, `dependency`, `deferred_to`, finding `path` and `line`) may be omitted or `null`; an optional string that is empty or blank counts as not given. Lists (`rulings`, `manual_rulings`, `new_findings`, `replies`, `manual_replies`) may be omitted, meaning empty. Why: an empty comment or question gives the owner nothing; an empty optional field is harmless.
- **D5** [gap] Plan T4: text files are written to `AGENT_LOOP_OUT` with fixed names: `change-body.md`, `summary.md`, `blocked-reason.md`, `needs-owner-question.md`, and `ruling-<n>-note.md`, `manual-ruling-<n>-note.md`, `finding-<n>-body.md`, `reply-<n>-body.md`, `manual-reply-<n>-body.md` with `n` the 1-based position in the list. An existing file of that name is replaced, never written through. Every text is checked against the 64 KiB limit before any file is written. Why: names from positions, not IDs, can't carry unsafe characters.
- **D6** [gap] FR-18 AC3: a draft problem's `failed` reason starts with `draft: ` and names the field (for example `draft: done.change.body is larger than 64 KiB`); an agent's own `failed` draft passes its reason through, folded to one line of at most 500 characters. Why: the log must tell an agent's failure from a conversion failure.

## Verification

Standard checks (`wf.py verify`).

## Risks and open questions

None.

## Outcome

_Filled in before shipping._
