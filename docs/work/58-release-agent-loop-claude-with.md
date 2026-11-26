# Work plan: Release agent-loop-claude with GoReleaser and a Homebrew formula in mayusuk/homebrew-tap

**Issue:** [#58](https://github.com/mayusuk/agent-loop-handlers/issues/58) (feature)
**Branch:** `issue/58-release-agent-loop-claude-with`
**Base:** `origin/main @ 0959001` (checked before branching)
**Status:** In progress

## Summary

Adds a GoReleaser config and a tag-triggered release workflow, mirroring mayusuk/agent-loop's (#124), so pushing a `v*` tag builds `agent-loop-claude` for macOS and Linux, publishes a GitHub release whose notes are the CHANGELOG section for that version, and writes `Formula/agent-loop-claude.rb` to mayusuk/homebrew-tap. The README now installs with `brew install mayusuk/tap/agent-loop-claude`.

## Context

- Resolves mayusuk/agent-loop#136 D5: the owner made this repository public and chose Homebrew (2026-11-25), replacing plan D16's `go install` from a private clone.
- mayusuk/agent-loop `.goreleaser.yaml` and `.github/workflows/release.yml` (#124) are the model: GoReleaser v2.20.3, actions pinned to full SHAs (checkout v4.4.0, setup-go v5.6.0, goreleaser-action v6.4.0), `brews` to mayusuk/homebrew-tap with `HOMEBREW_TAP_GITHUB_TOKEN`.
- `cmd/agent-loop-claude/main.go`: `Release` constant, `var version = Release` overridable with `-X main.version=...`. GoReleaser's `{{ .Version }}` is the tag without the `v`.
- `TestI12_4_ChangelogNamesTheRelease` already ties the newest CHANGELOG section to `Release`.
- agent-loop-claude has no cgo: no `import "C"` anywhere in the module or its dependencies (agent-loop/contract, jsonschema, x/text), and `CGO_ENABLED=0 GOOS=darwin go build ./...` passes.

## Approach

- `.goreleaser.yaml`: one pure-Go build (`CGO_ENABLED=0`) for darwin/linux × amd64/arm64, `-trimpath`, `-s -w -X main.version={{ .Version }}`; tar.gz archives `agent-loop-claude_<version>_<os>_<arch>` with README.md and CHANGELOG.md; `checksums.txt`; release to mayusuk/agent-loop-handlers; `brews` entry for `agent-loop-claude` with a version test and a caveat that Claude Code must be installed.
- `scripts/release-notes.sh <version>`: prints the `## <version>` section of CHANGELOG.md (without its heading), and fails if `Release` in main.go is not `<version>` or the section is missing or empty. The workflow runs it on `${GITHUB_REF_NAME#v}` and passes the file to `goreleaser release --release-notes`. So a tag that doesn't match the release fails before anything is published.
- `.github/workflows/release.yml`: `v*` tag push only, `ubuntu-latest` (no cgo needed), tests, notes, GoReleaser.
- `.gitignore`: `dist/`.
- README: Install via Homebrew (keep building from source as the alternative); Releasing section describes the tag-driven release.
- Go tests (`TestI12_4_ReleaseNotes...`) run the script: the current release prints the CHANGELOG section; a wrong version fails.

## Steps

- [x] Release notes script and its tests.
- [x] `.goreleaser.yaml`, `release.yml`, `.gitignore`; check with `goreleaser check` and `release --snapshot --clean`.
- [ ] README install and releasing sections.

## Acceptance criteria

- `.goreleaser.yaml` (GoReleaser v2.20.3, pinned) builds `./cmd/agent-loop-claude` for macOS arm64/amd64 and Linux amd64/arm64 with `-X main.version={{ .Version }}`, archives `agent-loop-claude_<version>_<os>_<arch>.tar.gz` plus `checksums.txt`, and a `brews` entry writes `Formula/agent-loop-claude.rb` to mayusuk/homebrew-tap (branch `main`, directory `Formula`) with `HOMEBREW_TAP_GITHUB_TOKEN`. Formula test: `agent-loop-claude version` prints the version. Release notes from CHANGELOG.md.
- `.github/workflows/release.yml`: triggered only by a `v*` tag push; runs the tests, then GoReleaser; every third-party action pinned to the same full SHAs as agent-loop's release.yml.
- `agent-loop-claude version` prints the tag without the `v` for a release build.
- README: install with `brew install mayusuk/tap/agent-loop-claude`.
- `goreleaser check` and `goreleaser release --snapshot --clean` pass locally.

## Decisions

- **D1** [gap] plan D16 / agent-loop#136 D5: macOS and Linux binaries are built with `CGO_ENABLED=0` on `ubuntu-latest`, unlike agent-loop's macOS runner. Why: agent-loop-claude and its dependencies use no cgo, so every target cross-compiles.
- **D2** [gap] issue #58 "Release notes from CHANGELOG.md": the GitHub release notes are the body of CHANGELOG.md's `## <version>` section (heading excluded), extracted by `scripts/release-notes.sh` and passed with `--release-notes`. The release fails before publishing if the tag (without `v`) is not `Release` in `main.go` or the section is missing or empty. Why: a tag that disagrees with the built-in release would ship a binary whose `version` lies, and notes must never be silently empty.
- **D3** [gap] issue #58: archives include README.md and CHANGELOG.md (agent-loop ships docs/GETTING-STARTED.md). The formula has no dependencies and a caveat that Claude Code (`claude`) must be installed and logged in. Why: Claude Code isn't a Homebrew formula dependency it can declare, and the handler fails without it.
- **D4** [gap] plan D2 (#11): the release is cut by pushing the `vX.Y.Z` tag; the workflow publishes the GitHub release, so the README's manual `gh release create` step is removed. Building from source stays documented as the alternative to Homebrew.

## Verification

- `go run github.com/goreleaser/goreleaser/v2@v2.20.3 check` and `release --snapshot --clean`, then run the snapshot binary's `version`.
- `GOOS=linux go build ./...` and `CGO_ENABLED=0 GOOS=darwin go build ./...`.

## Risks and open questions

- The release job can't be exercised without pushing a tag (not done here). The snapshot covers builds, archives and the formula; the tap push and release upload only run on a real tag.
- Needs the `HOMEBREW_TAP_GITHUB_TOKEN` repository secret before the first tag.

## Outcome

_Filled in before shipping._
