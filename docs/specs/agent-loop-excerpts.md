# agent-loop spec excerpts (vendored)

Verbatim excerpts from the agent-loop specs that this repository implements, copied from
[mayusuk/agent-loop@9e4f006](https://github.com/mayusuk/agent-loop/tree/9e4f006520535514f561969df78351c57320fe20/specs).

**agent-loop's specs win.** This file is a convenience copy so agents can read the contract
offline. If it disagrees with the specs on agent-loop's `main`, the agent-loop specs are right
and this copy is stale: refresh it (copy the same sections again and update the commit above).
Section numbers and cross-references (§n, FR-n, Dn, Tn) refer to the agent-loop specs.

Contents:

- INTEGRATION-SPEC §2–§5 (versioning, invocation, input, result), §12.1, §12.4, §13
- PRODUCT-SPEC FR-18, D34, D38
- TECHNICAL-SPEC T7, §7.1–§7.3

---

# From INTEGRATION-SPEC.md

## 2. Versioning

- `contract_version` is `MAJOR.MINOR`. The core supports exactly one major version and every minor up to its own.
- A minor version only adds optional fields. Handlers must ignore unknown input fields. The core rejects unknown result fields, so mistakes show up early.
- A handler declares the version it was written for in its profile entry (`contract = "1.0"`). `project validate` fails when the core can't support it.
- The JSON Schemas are in `contract/v1/` in the agent-loop repository: `input.schema.json`, `result.schema.json`, `tasks.schema.json` and `records.schema.json`. This spec is the source of truth for them.

## 3. Handler invocation

### 3.1 Starting a handler

The worker starts the stage's `command` from the profile:

| Aspect | Contract |
|---|---|
| Working directory | The run's workspace: a fresh git worktree on the item's branch (§3.3) |
| `AGENT_LOOP_INPUT` | Absolute path of `input.json` (read-only) |
| `AGENT_LOOP_RESULT` | Absolute path where the handler writes `result.json` |
| `AGENT_LOOP_OUT` | A directory for files the result points to (text bodies, §5.1). Only files in this directory are read |
| `AGENT_LOOP_CONTRACT` | The contract version the worker speaks, for example `1.0` |
| Git credentials | A git credential helper is configured in the worktree. It serves the stage's push token, or nothing for read-only stages (§3.4) |
| `GH_TOKEN` (GitHub) | A **read-only** token for the stage's identity, so tools like `gh` can read issues, pull requests and comments. It is valid for at least 55 minutes. For longer runs the handler gets a fresh one from `agent-loop-token read`, which asks the worker over the same socket as the credential helper |
| stdin | Closed |
| stdout, stderr | Captured, redacted (TECHNICAL-SPEC §7.3) and stored as the run log. Never parsed |
| Exit code | Informational. The result file decides the outcome. A missing, unreadable or invalid result is `failed` (FR-18 AC3), whatever the exit code |
| Time limit | The stage's `time_limit`. When it is reached the whole process group or container is stopped (FR-18 AC4) |

The handler must run headless: it must never wait for terminal input. A handler that needs a human returns `needs_owner` (FR-10 AC4).

### 3.2 Sessions

Every run is a new process and a new agent session (NFR-8). The input never holds a session reference, a transcript, or anything from another stage's session. A handler that needs to keep something for a later stage writes it to the branch (for example `.agent-loop/notes/<item>.md`) or returns it in its result as text that the worker posts.

### 3.3 Workspace and branches

- The worker prepares a worktree from a cached clone, fetched just before the run.
- The item's branch is `<branch_prefix><item-key>`, for example `agent/42` or `agent/PROJ-42`. After the owner closes an item's change without merging, its next change gets a new branch, `<branch_prefix><item-key>-<n>`, where *n* is one more than the number of the item's changes that were closed without merging (`agent/42-2`, then `agent/42-3`, product spec D44). `branch_prefix` comes from the profile (default `agent/`). The input names the branch, so a handler never builds the name itself.
- **implement:** if the branch doesn't exist, the worktree is created on a new branch from the base tip. Otherwise it checks out the branch head (for example after a crash, before the change was opened). After the owner closed the change without merging (FR-12 AC4), implement runs on the next branch name, from the base tip, so it never builds on the rejected commits. The old branch stays as it is (product spec §8 item 3, D44).
- **review:** the worktree is checked out at `review.commit`, detached, so the handler can't push.
- **Base update (D41):** the worker first tries the merge itself, with no handler and no agent run (§5.3). Only when the merge has conflicts does it start an address run in base-update mode.
- **address, base-update mode:** the handler resolves the conflicts by **merging** the base into the branch. It never rebases, and never force-pushes (§8.3 of the product spec).
- **mint:** the worktree is on the plan branch `<branch_prefix>plan/<plan-key>`, from the base tip.
- After the run, the worker deletes the worktree. The branch stays.

### 3.4 Credentials by stage

| Stage | Identity | Push token (credential helper) | Read token (`GH_TOKEN`) | Writes to the tracker |
|---|---|---|---|---|
| implement | coder | Yes: contents write on the project repository only | Yes | None. The worker opens the change |
| address | coder | Yes | Yes | None. The worker posts replies |
| review | reviewer | **None** | Yes | None. The worker posts the review |
| mint | coder | Yes. The same coder token: it isn't limited to the plan branch, and the push ruleset covers the plan branch like every branch under the prefix | Yes | None. The worker opens the proposal |

The loop identity's credentials never reach a handler (product spec §8.8).

## 4. Input (`input.json`)

### 4.1 Common fields

```json
{
  "contract_version": "1.0",
  "run_id": "item/eagain/42#1:implement:1:a",
  "stage": "implement",
  "attempt": 1,
  "deadline": "2026-11-14T16:00:00Z",
  "identity": "coder",
  "project": { "name": "eagain", "tracker": "github", "code_host": "github" },
  "repo": { "url": "https://github.com/mayusuk/agentMaster", "base_branch": "main" },
  "workspace": { "path": "/var/agent-loop/ws/eagain-42-1", "branch": "agent/42", "head": "9f2c…" },
  "item": { "ref": "42", "url": "https://github.com/mayusuk/agentMaster/issues/42", "type": "feature" },
  "change": null
}
```

- `run_id` is unique for each handler start: workflow ID, claim number (TECHNICAL-SPEC §6.1), stage, attempt, and a start suffix. A rerun after a worker crash is a new start and gets a new `run_id`, but keeps the same `attempt`, because a crash doesn't use an attempt (FR-16 AC1, D42, TECHNICAL-SPEC §6.5). `attempt` is the counted attempt. A handler can see a rerun only by its new `run_id`, and must expect the branch to hold pushes from the interrupted run.
- `item` and `change` are **references only** (NFR-8 AC1). The handler reads the title, body and comments itself, and treats them as data (product spec §8.5). The input never copies tracker text.
- `change` (when there is one): `{ "ref": "57", "url": "…", "head": "…", "base_tip": "…" }`.

### 4.2 Stage fields

| Stage | Extra fields |
|---|---|
| implement | None. An existing open change is adopted, and implement isn't run (FR-3 AC3) |
| review | `review`: `{ "kind": "round" \| "base_update" \| "manual_reply" \| "owner_push", "commit": "<sha>", "round": 2 \| null, "blocking_threshold": 4, "rule_findings": ["F3","F5"], "rule_manual": ["MC-981"], "resolved_paths": ["api/login.go"] }`. `resolved_paths` is set only for `base_update` (§5.2, address) |
| address | `address`: `{ "mode": "findings" \| "manual" \| "base_update", "answer_findings": ["F3"], "answer_manual": ["MC-981"] }` |
| mint | `mint`: `{ "plan_key": "mint-7", "source_of_truth": ["specs/001.md","specs/002.md","specs/003.md"], "plans_dir": "plans/" }`. `plan_key` is provisional (§5.2, mint) |

- `rule_findings` lists every finding that is unresolved before this review (FR-4 AC4). `rule_manual` lists every manual comment that an address run replied to and no review has ruled on yet (FR-4 AC8).
- `answer_findings` lists every unresolved finding. `answer_manual` lists every manual comment that isn't cleared, except empty "changes requested" reviews, which have nothing to answer (FR-5 AC1, §9.6). Both are empty in `base_update` mode (FR-5 AC6). The address handler sees `base_update` mode only when the base merge has conflicts (§5.3).
- IDs refer to records the handler can read: finding IDs in reviewer records (§6.3), and manual comment IDs in the code host (`MC-<id>` is the code host's comment or review ID).

## 5. Result (`result.json`)

### 5.1 Shape

```json
{ "contract_version": "1.0", "run_id": "…", "result": "done", "done": { … } }
```

`result` is one of `done`, `complete`, `blocked`, `failed` or `needs_owner`. Only the object named by `result` may be present, plus the optional `usage` object. The results allowed for each stage are in FR-18 AC1.

`usage` (optional, any result) reports what the run spent, for FR-16 AC3's spend limit: `{ "cost": 1.84, "currency": "USD", "input_tokens": 120000, "output_tokens": 9000 }`. Every field is optional. The loop adds `cost` to the project's daily spend when the profile sets `limits.daily_spend` (TECHNICAL-SPEC §13). A run with no `usage` adds nothing, so a spend limit only holds for handlers that report it (FR-16 AC3: "when handlers report it").

**Text fields** (bodies, replies, questions) are given as `*_file` paths relative to `AGENT_LOOP_OUT`, never inline. Each file is UTF-8 markdown up to 64 KiB. The worker redacts it, adds the record marker (§6.1) and posts it. A file outside `AGENT_LOOP_OUT`, or one that is too big, makes the result invalid.

| Result | Object | Fields |
|---|---|---|
| `blocked` | `blocked` | `reason_file`, optional `dependency` (an item ref) |
| `failed` | `failed` | `reason` (one line, at most 500 characters, stored in the log only) |
| `needs_owner` | `needs_owner` | `question_file` |
| `complete` | `complete` | none (mint only) |

### 5.2 `done` per stage

**implement**

```json
"done": {
  "head": "<sha pushed to the item's branch>",
  "change": { "title": "Add login endpoint", "body_file": "change.md" }
}
```

The worker checks that `head` is the remote head of the item's branch. It then opens the change as the coder identity, from the branch to the base, linked to the item (§6.2). If a change already exists for the branch, it adopts it instead of opening a second one.

**review**

```json
"done": {
  "commit": "<sha reviewed, must equal input review.commit>",
  "kind": "round",
  "summary_file": "summary.md",
  "rulings": [
    { "finding": "F3", "status": "addressed" },
    { "finding": "F5", "status": "partially_addressed", "note_file": "f5.md" }
  ],
  "manual_rulings": [ { "comment": "MC-981", "ruling": "open", "note_file": "mc981.md" } ],
  "new_findings": [
    { "score": 4, "path": "api/login.go", "line": 88, "title": "Password compared in non-constant time", "body_file": "n1.md" },
    { "score": 2, "path": null, "line": null, "title": "Missing changelog entry", "body_file": "n2.md" }
  ]
}
```

The worker checks these rules. Breaking any of them makes the result `failed`:

- `commit` and `kind` equal the input.
- `rulings` has exactly one entry for each ID in `rule_findings` (FR-4 AC4), and `manual_rulings` has exactly one for each ID in `rule_manual` (FR-4 AC8).
- `status` is one of `open`, `partially_addressed`, `addressed` or `withdrawn`. `ruling` is `addressed` or `open`.
- `score` is an integer from 1 to 5. A `path` must exist at `commit`, and a `line` must be in the file. `line` needs a `path`.
- How each new finding is posted (I8, I16): with `path` and a `line` that is in the change's diff, as an inline comment. With a `path` that is in the diff but no line, or a line outside the diff, as a file-level comment whose text starts with `path:line`. With a `path` that isn't in the diff, or neither, as a section of the review text that starts with the location. GitHub accepts review comments only on files and lines in the diff, so this is the nearest thread it allows. Handlers should give a `path` whenever the finding is about a file.

The worker gives each new finding the next ID on the change (`F1`, `F2`, … in array order). It then posts one review as the reviewer identity (§6.3). The review event is `APPROVE` when branch protection requires an approval and no unresolved finding is at or above the threshold (FR-4 AC7). Otherwise it is `COMMENT`. The handler never chooses the event.

**address**

```json
"done": {
  "head": "<sha, same as input head if nothing was pushed>",
  "replies": [
    { "finding": "F3", "kind": "fixed", "body_file": "r3.md" },
    { "finding": "F5", "kind": "deferred", "deferred_to": "61", "body_file": "r5.md" }
  ],
  "manual_replies": [ { "comment": "MC-981", "kind": "explained", "body_file": "m981.md" } ]
}
```

- `kind` is one of `fixed`, `explained`, `deferred`, `not_valid` or `needs_info` (FR-5 AC1). `deferred` needs `deferred_to`, an item that exists and is open (FR-5 AC5). Otherwise that reply doesn't count as a reply.
- The worker applies FR-5 AC2: the head moved or every finding in `answer_findings` has a valid reply, and every comment in `answer_manual` has a reply. Otherwise the result is `failed`.
- In `base_update` mode, `replies` and `manual_replies` must be empty, and `head` must be **one new merge commit** whose first parent is the input head and whose second parent is a base commit that contains the input `base_tip` (FR-5 AC6). Anything else (several commits, a rebase, a commit with one parent) makes the result `failed`. The worker can't check conflict resolutions mechanically. Instead it merges the two parents itself without a working tree (`git merge-tree`). The paths that conflicted, and any path whose content differs from the clean parts of that merge, are passed to the `base_update` review as `review.resolved_paths`, and that review checks them (FR-6 AC6).
- The worker posts each reply as the coder identity in the finding's or comment's thread (§6.4).

**mint**

```json
"done": {
  "plan_key": "003-auth",
  "head": "<sha on the plan branch>",
  "proposal": { "title": "Plan: 003 auth", "body_file": "proposal.md" }
}
```

The loop doesn't know the next plan's key before the minter decides what to plan. So the input's `plan_key` is a provisional mint ID (`mint-<n>`), and the branch is `<branch_prefix>plan/mint-<n>`. `done.plan_key` is the key the minter chose. It must match `[a-z0-9-]{1,64}`, and no `plans/<plan_key>/` may exist on the base branch. The worker checks that the plan branch holds `plans/<plan_key>/plan.md` and a valid `plans/<plan_key>/tasks.yaml` (§7), and that the branch changes nothing outside `plans/<plan_key>/`. It then opens the proposal as the coder identity, and the loop identity labels it `plan-proposal`. An invalid task list makes the result `failed`, so a bad plan is caught before the owner sees it (materialise still validates again, FR-17 AC6).

### 5.3 Base update without an agent (D41)

When an item needs a base update (TECHNICAL-SPEC §8.1 cause 3), the worker does this first, as a deterministic activity with no handler:

1. It fetches the item's branch and the base, and merges the base tip into the branch head without a working tree (`git merge-tree --write-tree`).
2. If the merge has **no conflicts**, it makes one merge commit (first parent the branch head, second parent the base tip) with the coder identity as author and committer, and pushes it to the item's branch as the coder identity. The push is a plain fast-forward. If the branch head moved in between, the push is refused, nothing is retried, and the merge conditions are checked again (cause 2). It never force-pushes.
3. If the merge has **conflicts**, it pushes nothing and starts an address run in `base_update` mode (§5.2, address). Only this case is an agent run, and only this case counts toward the daily agent-run limit (FR-16 AC3).


### 12.1 Integration levels

| Level | The project | Writes | Typical effort |
|---|---|---|---|
| 0. Starter | Runs `agent-loop init` and picks the starter handlers (§12.4) | Nothing | Minutes |
| 1. Custom prompts | Keeps the starter handlers, and points them at its own prompt files or skills | Markdown prompts | An hour |
| 2. Custom handler | Writes its own handler program that meets §3–§5 | A program in any language | A day |
| 3. New tracker or code host | Writes a Go adapter for §8 that passes the conformance suite (§11) | Go code in agent-loop | Days. Rare, done once per system |

Every level uses the same contracts. Moving up a level never needs a change to the core.

### 12.4 Starter handlers (D38)

Official starter handlers live in a separate repository, `agent-loop-handlers`, not in the core (D34). They are one program, `agent-loop-claude`, with one subcommand per stage:

```toml
[stages.implement]
command  = ["agent-loop-claude", "implement"]
contract = "1.0"

[stages.review]
command  = ["agent-loop-claude", "review"]
contract = "1.0"
```

- Each subcommand runs Claude Code headless in the workspace, with a stage prompt, and turns the agent's output into a `result.json` that meets §5. It handles text files, the result shape and the `failed` cases, so a project never deals with the contract at levels 0 and 1.
- **Prompts** (level 1): the default prompt for each stage is built in. A project overrides one by adding `.agent-loop/prompts/<stage>.md` to its repository, or by naming a skill (`--skill work-issue`). Prompts are read from the base branch at the start of the run. They are project files the owner controls, never tracker text (product spec §8.5).
- The starter handlers follow the contract's major version. A release of `agent-loop-handlers` names the contract versions it supports, and `project validate` checks it like any other handler (§2).
- The first adopter's skills (`work-issue`, `review-pr`, `address-pr`, `plan-work`) run through the starter handlers with `--skill` (owner decision N12, 2026-11-15). Each skill gets a result-only mode, which writes its result for the starter handler to turn into `result.json` and never posts to GitHub itself (I1, I2). `plan-work` in that mode creates no issues (FR-9 AC3).

## 13. Profile reference (contract-relevant keys)

| Key | Type | Default | Used by |
|---|---|---|---|
| `tracker.adapter`, `code_host.adapter` | string | `github` | §8 |
| `branch_prefix` | string | `agent/` | §3.3, §9.8 |
| `plans_dir` | string | `plans/` | §7 |
| `markers.<marker>` | `{label}` or `{status}` | labels with the §5.1 names | §9.2, §10 |
| `identities.coder`, `.reviewer`, `.loop` | secret reference | required | §9.1 |
| `identities.owners` | list of `{ login, id }`. `id` is the stable account ID and is what counts. `login` is for people reading the file, and `validate` warns when it no longer matches the ID | required | §9.8, FR-13, I13 |
| `stages.<stage>.command` | list of strings | required for each stage used | §3.1 |
| `stages.<stage>.contract` | `MAJOR.MINOR` | required | §2 |
| `stages.<stage>.capabilities`, `.runner`, `.time_limit`, `.attempts` | | `["agent"]`, `local`, `2h`, `2` | TECHNICAL-SPEC §6–§7 |
| `gates` | list of strings | the base branch's required checks | §9.7 |
| `manual_comment_bots` | list of bot logins | `[]` | §9.6 (I7) |
| `merge_method` | `squash` \| `merge` \| `rebase` | `squash` | §9.7 |
| `egress.allow`, `egress.replace` | list of domains, bool | the defaults in TECHNICAL-SPEC §7.2, `false` | TECHNICAL-SPEC §7.2 |
| `release_apps` | list of app IDs | `[]` | §9.8 step 1 (tag ruleset bypass) |

The other profile keys (limits, rounds, intervals, follow-up policy, time zone, priority) are in TECHNICAL-SPEC §13.

---

# From PRODUCT-SPEC.md

### FR-18 Stage results

- **AC1:** Every stage returns exactly one structured result. The allowed results, and what the loop does with each one, are:

| Result | Allowed from | What the loop does |
|---|---|---|
| `done` | implement, review, address, mint, materialise | Records the result and moves to the next stage (§6) |
| `complete` | mint | Reports that the project is complete (FR-9 AC5) |
| `blocked` (reason, optional dependency) | implement, address | If it names an open dependency: the item goes to `blocked`. When the dependency is closed as done, the next stage is chosen as in FR-10 AC2. If the dependency is closed any other way, the item is escalated. If it names no dependency, the item is escalated with the reason |
| `failed` (reason) | implement, review, address, mint, materialise | Runs the same stage again in a new session while attempts remain (FR-16 AC1). Then escalates |
| `needs_owner` (question) | implement, review, address, mint | Escalates with the question (FR-10 AC4) |

- **AC2:** What `done` carries: implement, the change reference; review, the review result (FR-4 AC2); address, the replies and the new head if it moved; mint, the plan proposal reference; materialise, the created item references.
- **AC3:** A result that is missing, malformed, or not allowed for the stage is treated as `failed`.
- **AC4:** A handler that goes over its time limit is stopped, and the item is escalated (FR-16 AC1).

- **D34** Owner ruling 2026-11-14 (D22 rejected): the core ships no stage handlers. It defines only the stage contracts, what a handler receives and returns. Each project supplies its handlers, and the first adopter's skills are one such set (§3, §9, FR-1 AC3).
- **D38** Owner ruling 2026-11-14: official starter handlers, based on Claude Code, are maintained in a separate repository (`agent-loop-handlers`), so a project can start without writing a handler. They are not part of the core, so D34 holds: the core still ships no handlers, and a project can use, fork or ignore them. They meet the stage contracts like any other handler.

---

# From TECHNICAL-SPEC.md

## 2. Technology decisions (T7 only)

| # | Area | Decision | Why |
|---|---|---|---|
| T7 | First handlers (outside the core) | The starter handlers `agent-loop-claude` (D38, INTEGRATION-SPEC §12.4), which run Claude Code headless and can use the adopter's skills through `--skill`. Their first release ships with v0.1 (owner decision X7, §15) | The v0.1 adopter's skills already exist. A Claude Agent SDK handler was considered and is not planned: D38 names Claude Code |

## 7. Stage execution

### 7.1 Handler contract (summary)

A handler is a command in the profile:

```toml
[stages.implement]
command      = ["agent-loop-claude", "implement", "--skill", "work-issue"]
contract     = "1.0"
capabilities = ["agent"]
runner       = "local"          # "docker" from v0.2
time_limit   = "2h"
```

The handler's stdout is only a log and is never parsed (INTEGRATION-SPEC §3.1). Its result goes to `AGENT_LOOP_RESULT`. A command such as `claude -p … --output-format json` on its own therefore always ends as `failed`: it needs a wrapper, such as the starter handlers, that writes `result.json`.

The runner gives the handler:

- `AGENT_LOOP_INPUT`: the path to `input.json`, with `contract_version`, stage, review kind, references to the item, change and plan, the workspace path and the identity it acts as. It holds **references only**, never tracker text and never a session reference (NFR-8 AC1, §8.5). The handler reads the tracker itself, as data.
- `AGENT_LOOP_RESULT`: the path where it writes `result.json`, with one result from FR-18. The result carries the content the loop needs (change title and body, review findings and rulings, replies, plan proposal), and the worker posts it (T21).
- Credentials for its identity only (§7.3).

A missing, malformed or disallowed result, or an unsupported `contract_version`, is `failed` (FR-18 AC3). The full contract is in `INTEGRATION-SPEC.md` §3–§5, and the schemas live in `contract/v1/`.

### 7.2 Runners and sandboxing

| Runner | Release | Isolation | Egress |
|---|---|---|---|
| `local` | v0.1 | Fresh git worktree from a cached clone. The process runs in its own process group. Claude handlers run inside Claude Code's OS sandbox, limited to the worktree | The Claude sandbox's domain allowlist, generated from the profile |
| `docker` | v0.2 | A container per run from the project image (T10). Only the worktree and the input and result directory are mounted. Agents may use skip-permissions only here | A container network that allows only the profile's allowlist |
| `remote` | later | A microVM or cloud sandbox | Provider controls |

The v0.1 sandbox only covers Claude-based handlers. The sandbox is required (owner decision, 2026-11-15): `project validate` fails, and the worker refuses to start the stage, when an agent stage uses `runner = "local"` with a handler that the Claude sandbox doesn't wrap. A profile can opt out for a stage with `runner = "local-unsandboxed"`. Such a handler runs with the owner's full user access: it can read the owner's files, reach any network address, and open the local API socket (§11). `validate` then warns, in those words, on every run.

The `local` runner generates the sandbox settings for each run:

- **Writes:** the worktree, the run's `AGENT_LOOP_OUT` and result path, and a per-run temp directory. Nothing else.
- **Reads denied:** agent-loop's config and state directory (`~/.config/agent-loop`, which holds the API socket and any env/file secrets), the projection database, the server's Temporal data, `~/.ssh`, `~/.aws`, `~/.config/gh`, `~/.netrc`, `~/.docker`, `~/.kube`, the git credential stores, and macOS Keychain files (`~/Library/Keychains`). The Keychain backend (T12) also gives each item an access list that names only the agent-loop binary, so another process gets a prompt, not the key.
- **Network:** only the egress allowlist. Loopback and the local network are denied, so a handler can't reach the server's gateway or other local services.
- **Default egress allowlist (T9):** the Anthropic API (`api.anthropic.com`), the code host (on GitHub: `github.com`, `api.github.com`, `codeload.github.com`, `objects.githubusercontent.com`), and common package registries (`registry.npmjs.org`, `proxy.golang.org`, `sum.golang.org`, `pypi.org`, `files.pythonhosted.org`, `crates.io`, `static.crates.io`). The profile's `egress.allow` adds to it. `egress.replace = true` replaces it.

A host with Docker advertises the capability `docker`. A stage with `runner = "docker"` requires it automatically.

### 7.3 Credentials in a run

- The worker gets a short-lived token from `SecretStore` for the stage's identity only: coder for implement, address and mint, and reviewer for review. Handlers get a narrowed token: read access for every stage, plus push for the coder stages. The review handler gets no push token at all (FR-13 AC2). Write access to issues and pull requests stays with the worker (T21, INTEGRATION-SPEC §3.4, §9.1).
- The loop identity's credentials never reach a runner (§8.8). Only adapter activities in the worker use them.
- Git gets the token through a credential helper served by the worker over a per-run socket in the run's temp directory (the only agent-loop socket the sandbox allows), not an environment variable, so it doesn't show in process listings, the environment, or handler output. This is not a secret from the handler: any process in the run can ask the helper (`git credential fill`) and use the token directly. The helper keeps the token out of logs and lets the worker issue a fresh token when the old one is near its one-hour expiry. What the token can do is limited by its scope and by the code host's rulesets, not by the helper.
- The coder handler token is limited to the project's repository with `contents: write`, `metadata: read` and read access to issues and pull requests (INTEGRATION-SPEC §9.1). It can't open changes or reply: the worker does that with a separate token (T21, I2). Because `contents: write` on GitHub also allows pushing to any branch, creating tags and releases, and merging pull requests, rulesets limit it: the loop's branch prefix accepts pushes only from the coder and owners, and the base branch and tags refuse it (INTEGRATION-SPEC §9.8, I10). It has no `workflows` permission, so it can't change `.github/workflows/` (T18, T19).
- The **coder worker token** (never given to a handler) has `pull requests: write`, `issues: read` and `contents: write`. It opens changes, posts replies, and pushes the worker's own conflict-free base merges (§8.1 cause 3, D41, INTEGRATION-SPEC §5.3, §9.1). It is issued for each of those activities and revoked when the activity ends. The loop identity is never used to push, even for a base merge (§8.8).
- At the end of every run, the worker revokes every token it issued for that run (`SecretStore.Revoke`). So a process the handler left behind can't keep pushing, and a head move seen after the run can't come from that run's token (INTEGRATION-SPEC §9.8, I11).
- Every log line and stored handler output passes through a redactor that knows the run's tokens (§8.7, FR-13 AC3).
