# Work plan: release-notes.sh: mismatched or nested code fences can cut release notes short (follow-up from #60 R4)

**Issue:** [#61](https://github.com/mayusuk/agent-loop-handlers/issues/61) (bug)
**Branch:** `issue/61-release-notes-sh-mismatched-or-nested`
**Base:** `origin/main @ 6eb8cb9` (checked before branching)
**Status:** Done

## Summary

`scripts/release-notes.sh` now tracks code fences the way CommonMark does: a fence opens on three or more backticks or tildes indented by at most three spaces, and closes only on a line of the same character, at least as long as the opener, with nothing after it but spaces. A `~~~` inside a ``` block, or a ``` inside a ```` block, no longer ends the fence early and cuts the release notes short.

## Context

- Work plan #58 D2 and D8: the release notes are the body of the version's `## <version>` section of CHANGELOG.md, and a `## ` line inside a code fence is section text.
- Root cause: the awk program flips `fence = !fence` on any line matching `^[ \t]*(```|~~~)`. It does not remember which character or length opened the fence, so any fence-like line inside a fence closes it. A following `## ` line then ends the section early (exit 0, notes silently short), or, when the flip leaves the state inverted, the section runs on into the previous version.
- The script must run with BSD awk (macOS) and mawk (Ubuntu CI), so it uses only POSIX awk: `substr`, `index`, simple regexes, no interval expressions, no gawk extensions.
- mayusuk/agent-loop#150 is the same bug in agent-loop's copy of the script; no PR for it exists yet, so there is nothing to align with beyond the shared rule.

## Approach

Replace the flip with a small fence scanner in the awk program: count up to three leading spaces, then the run of `` ` `` or `~`. Outside a fence, a run of three or more opens one (a backtick fence's info string may not contain a backtick) and records its character and length. Inside a fence, a run of the same character at least as long, followed only by spaces or tabs, closes it; every other line is fence content. Headings are only checked outside fences. Files: `scripts/release-notes.sh`, `cmd/agent-loop-claude/release_test.go`.

## Steps

- [x] Tests for nested and mismatched fences (fail on the current script), plus lines that are not fences under CommonMark.
- [x] Fence tracking by character and length in `scripts/release-notes.sh`.

## Acceptance criteria

- A fence closes only on the same character, at least as long as the opener (``` and ~~~), indented up to three spaces.
- Tests cover nested and mismatched fences.
- Works with BSD awk and mawk.

## Decisions

- **D1** [ambiguity] #58 D8: fences follow CommonMark at the top level only: an opener or closer may be indented by at most three spaces (a tab counts as four), so a fence indented four or more spaces, for example in a nested list, is not tracked. Why: the script does not parse list containers; the issue and the caller ask for the CommonMark rule of up to three spaces.
- **D2** [gap] #58 D8: a closing fence may be followed only by spaces or tabs; a ``` line with an info string inside a ``` block is content, as in CommonMark. A backtick line whose info string contains a backtick is not a fence opener. Why: CommonMark §4.5.

## Verification

The `TestI12_4_ReleaseNotes*` tests pass locally with macOS `/usr/bin/awk` (BSD awk) and with mawk 1.3.4 put first on `PATH` as `awk`; CI's Ubuntu runner uses mawk.

## Risks and open questions

None.

## Outcome

`scripts/release-notes.sh` tracks the opening fence's character and length and closes it only on a matching CommonMark closing fence; lines that are not fences under CommonMark (indented four spaces or by a tab, fewer than three characters, a backtick in a backtick info string) no longer open one. New tests: `TestI12_4_ReleaseNotesFencesCloseOnlyOnAMatchingFence` (nested and mismatched fences) and `TestI12_4_ReleaseNotesIgnoreLinesThatAreNotFences`. No deviation from the approach. mayusuk/agent-loop#150 has no PR yet; its fix should use the same rule (D1, D2).
