# The agent draft

At the end of a stage run, the agent that `agent-loop-claude` starts writes a **draft**: one JSON file with its outcome and its text inline. The handler turns the draft into the `result.json` that agent-loop reads (INTEGRATION-SPEC §5): it writes each text to a file in `AGENT_LOOP_OUT`, fills in the fields it owns, and turns any problem into a `failed` result. A skill that runs through `--skill` writes the same draft.

The built-in prompts tell the agent where to write it and which shapes to use. The JSON Schema is [`internal/draft/agent-draft.schema.json`](../internal/draft/agent-draft.schema.json): validate a draft against `$defs/implement`, `$defs/review` or `$defs/address`.

## Shape

```json
{ "result": "<outcome>", "<outcome>": { ... } }
```

`result` names the outcome, and the object of that name is the only other field. Text is inline markdown.

| Outcome | implement | review | address |
|---|---|---|---|
| `done` | yes | yes | yes |
| `blocked` | yes | no | yes |
| `needs_owner` | yes | yes | yes |
| `failed` | yes | yes | yes |

### implement `done`

```json
{ "result": "done", "done": { "change": { "title": "Add login", "body": "What changed, why, and how it was tested." } } }
```

### review `done`

```json
{ "result": "done", "done": {
    "summary": "The review in a few sentences.",
    "rulings": [ { "finding": "F3", "status": "addressed", "note": "optional" } ],
    "manual_rulings": [ { "comment": "MC-981", "ruling": "open" } ],
    "new_findings": [ { "score": 4, "path": "api/login.go", "line": 88, "title": "One line", "body": "The problem and a scenario." } ] } }
```

- `status` is `open`, `partially_addressed`, `addressed` or `withdrawn`; `ruling` is `addressed` or `open`.
- `score` is an integer from 1 to 5. `path` (repository-relative) and `line` (from 1) may be `null` or left out.
- One ruling for each ID in the input's `rule_findings` and `rule_manual`.

### address `done`

```json
{ "result": "done", "done": {
    "replies": [ { "finding": "F3", "kind": "fixed", "body": "The reply." },
                 { "finding": "F5", "kind": "deferred", "deferred_to": "61", "body": "The reply." } ],
    "manual_replies": [ { "comment": "MC-981", "kind": "explained", "body": "The reply." } ] } }
```

- `kind` is `fixed`, `explained`, `deferred`, `not_valid` or `needs_info`. A `deferred` reply names `deferred_to`, an open item.
- In `base_update` mode both lists are empty.

### `blocked`, `needs_owner`, `failed`

```json
{ "result": "blocked", "blocked": { "reason": "Why.", "dependency": "7" } }
{ "result": "needs_owner", "needs_owner": { "question": "What the owner must decide." } }
{ "result": "failed", "failed": { "reason": "One line." } }
```

`dependency` is optional. A `failed` reason is folded to one line of at most 500 characters.

## Rules

- **No other fields.** Unknown fields, and keys in a different case, make the run `failed`. So do the fields the handler fills in itself: `run_id` and `contract_version`, and `head`, `commit` and `kind` in `done`. The handler sets the pushed head, the reviewed commit and the review kind.
- **Text.** Titles are one line. `body`, `summary`, `reason` and `question` must not be blank. Each text is at most 64 KiB. Optional fields (`note`, `dependency`, `deferred_to`, `path`, `line`) may be left out or `null`; an empty or blank optional string counts as left out. Lists may be left out, meaning empty.
- **The file.** The draft is a regular file (not a symlink) of UTF-8 JSON, at most 1 MiB, at the path the prompt gives.

A draft that breaks a rule, or no draft at all, gives a `failed` result whose reason starts with `draft:` and names the field, for example `draft: done.change.body is larger than 64 KiB`. The converted result is then checked against the contract as usual, for example that every finding has a ruling.

## Files the handler writes

| Draft field | Result field | File in `AGENT_LOOP_OUT` |
|---|---|---|
| `done.change.body` | `done.change.body_file` | `change-body.md` |
| `done.summary` | `done.summary_file` | `summary.md` |
| `done.rulings[i].note` | `note_file` | `ruling-<i+1>-note.md` |
| `done.manual_rulings[i].note` | `note_file` | `manual-ruling-<i+1>-note.md` |
| `done.new_findings[i].body` | `body_file` | `finding-<i+1>-body.md` |
| `done.replies[i].body` | `body_file` | `reply-<i+1>-body.md` |
| `done.manual_replies[i].body` | `body_file` | `manual-reply-<i+1>-body.md` |
| `blocked.reason` | `blocked.reason_file` | `blocked-reason.md` |
| `needs_owner.question` | `needs_owner.question_file` | `needs-owner-question.md` |
