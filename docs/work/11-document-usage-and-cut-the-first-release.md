# Work plan: Document usage and cut the first release

**Issue:** [#11](https://github.com/mayusuk/agent-loop-handlers/issues/11) (feature)
**Branch:** `issue/11-document-usage-and-cut-the-first-release`
**Base:** `origin/main @ 9c15413` (checked before branching)
**Status:** Draft

## Summary

Document how to install and use `agent-loop-claude`, and prepare its first release, v0.1.0: the README gets install, profile snippets for every stage, prompt overrides and `--skill`, the draft format, the environment variables and the supported contract versions; `CHANGELOG.md` records v0.1.0; `version` reports `0.1.0` with no build flags. The tag and the GitHub release are left to the owner.

## Context

- INTEGRATION-SPEC §12.4 (profile snippet, prompt overrides from the base branch, `--skill`, a release names its contract versions) and TECHNICAL-SPEC §7.1 (the full stage entry: `capabilities`, `runner`, `time_limit`) and §7.2 (`local` vs `local-unsandboxed`).
- Plan D7 (version prints the release and contracts), D11 (`mint` stub), D12 (sandbox handoff through `AGENT_LOOP_CLAUDE_SETTINGS`), D13 (finding `claude`), D16 (install by `go install ./cmd/agent-loop-claude` from the clone; repository stays private).
- agent-loop's profile schema requires `stages.implement`, `.review` and `.address`; `mint` is optional.
- `cmd/agent-loop-claude/main.go`: `version` is set by `-ldflags`, else Go's module build info, else `dev`. A `go install` from a clone gets no ldflags, so today it does not report `0.1.0`.
- Bug #38: the live smoke test's sandbox settings lack §7.2's read denials and egress allowlist. The live test must not be run until it is fixed.

## Approach

- `main.go`: `version` defaults to `0.1.0` in source; `-ldflags -X main.version=...` still overrides it. The build-info fallback goes, since it can no longer be reached.
- A test asserts the default version and contracts, and that `CHANGELOG.md` and `README.md` name the same release and contract versions.
- README: rewrite the top half (install, profiles for every stage, prompts, environment, versions, releasing), and put a warning on the live smoke test section pointing at #38.
- `CHANGELOG.md`: v0.1.0 with what ships, the known issue #38, and the supported contract.

## Steps

- [ ] Make `version` report `0.1.0` by default, with a test.
- [ ] Add `CHANGELOG.md` and update the README; test that they name the release and contracts.

## Acceptance criteria

- `agent-loop-claude version` reports `0.1.0` and contracts `1.0`, and the CHANGELOG and README name the same (§12.4).
- The README profile snippets match INTEGRATION-SPEC §12.4 and TECHNICAL-SPEC §7.1.

## Decisions

- **D1** [gap] plan T10, D7: `version` prints the release without a leading `v` (`agent-loop-claude 0.1.0`, `{"version":"0.1.0",...}`), and the git tag is `v0.1.0`, as Go module tags need. The release is a constant in `main.go` that `-ldflags -X main.version=...` can still override; a build of `main` between releases reports the last release. Why: `go install` from a clone (D16) gets no ldflags, and the acceptance criterion says `0.1.0`.
- **D2** [gap] plan T10: the release is an annotated tag `v0.1.0` on the merge commit of this PR, pushed by the owner, plus an optional GitHub release whose notes are the CHANGELOG's v0.1.0 section. The README's "Releasing" section gives the commands. Why: publishing is the owner's call, and D16 keeps the repository private.
- **D3** [gap] plan T10, T9: the README and CHANGELOG say not to run the live smoke test (`AGENT_LOOP_LIVE=1`) until #38 is fixed, and list #38 as a known issue of v0.1.0. Why: its sandbox settings lack TECHNICAL-SPEC §7.2's read denials and egress allowlist.
- **D4** [gap] INTEGRATION-SPEC §12.4, TECHNICAL-SPEC §7.1: the README's profile shows all three required stages (`implement`, `review`, `address`) with `runner = "local"`, and leaves `mint` out, since it always returns `failed` in this release (D11).

## Verification

- `go run ./cmd/agent-loop-claude version` and `go install ./cmd/agent-loop-claude && agent-loop-claude version --json` print `0.1.0` and `1.0`.

## Risks and open questions

- None for the owner. The tag push and release are the owner's to do.

## Outcome

_Filled in before shipping._
