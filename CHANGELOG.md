# Changelog

All notable changes to `agent-loop-claude`. Versions follow semantic versioning; each release's
git tag is the version with a leading `v`.

## 0.1.0

The first release, for agent-loop v0.1 (INTEGRATION-SPEC §12.4, PRODUCT-SPEC D38).

Contract versions: 1.0

### Added

- `agent-loop-claude implement`: implements a work item with Claude Code headless, pushes the
  branch and returns the change's title and body.
- `agent-loop-claude review`: reviews a change and returns findings and rulings, without pushing.
- `agent-loop-claude address`: answers review findings and owner comments and pushes fixes
  (`findings` and `manual` modes), or merges the base into the branch (`base_update` mode).
- `agent-loop-claude mint`: a stub that always returns `failed`; minting comes with agent-loop v0.2.
- `agent-loop-claude version [--json]`: prints the release and the supported contract versions.
- Built-in stage prompts, project overrides in `.agent-loop/prompts/<stage>.md` read from the base
  branch, and `--skill <name>` to run a Claude Code skill in result-only mode.
- The agent draft format (`docs/agent-draft.md`), turned into a valid `result.json`; every problem
  becomes a `failed` result.
- Claude Code runs in its sandbox with the settings in `AGENT_LOOP_CLAUDE_SETTINGS`;
  `--unsandboxed` only for `runner = "local-unsandboxed"`.
- A live smoke test (`AGENT_LOOP_LIVE=1`) against a local bare repository.

### Known issues

- [#38](https://github.com/mayusuk/agent-loop-handlers/issues/38): the live smoke test's sandbox
  settings lack TECHNICAL-SPEC §7.2's read denials and egress allowlist. Do not run the live test
  until it is fixed. This affects only the test: under agent-loop, the `local` runner writes the
  sandbox settings.
