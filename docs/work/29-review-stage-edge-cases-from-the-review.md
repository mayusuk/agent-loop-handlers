# Work plan: Review stage edge cases from the review of #28

**Issue:** [#29](https://github.com/mayusuk/agent-loop-handlers/issues/29) (bug)
**Branch:** `issue/29-review-stage-edge-cases-from-the-review`
**Base:** `origin/main @ ef6eb47` (checked before branching)
**Status:** Done

## Summary

The review stage's location check now fails the run when git cannot read a file at `review.commit`, instead of treating every git error as "not a file". The `Location:` line it writes into a finding's body stays one valid Markdown code span whatever the path holds, and the tests cover every bad path form and the 64 KiB overflow the prefix can cause.

## Context

- Work plan #7 (`docs/work/7-implement-the-review-subcommand.md`) D4 and D5 define the behaviour: a path that is not a file at the commit moves to a ``Location: `<path>` `` first line of the body; any other read error fails the run with `cannot read <path> at <sha>: <error>`.
- Root cause of R1: `gitx.FileLines` runs `git cat-file -t <commit>:<path>` and returns "not a file" on any error. `cat-file` exits 128 both for a missing path and for a real failure (a broken repository, a cancelled or timed-out context), so the two cannot be told apart from its exit status.
- R4: `fixLocations` writes the path between single backticks, so a backtick in the path ends the code span early and a newline splits the `Location:` line. The contract schema allows both characters in a path.
- R2, R3: only `./src.go` of D4's bad forms is tested, and nothing tests the size limit after the prefix is added (`draft` checks `contract.MaxTextFile` when it writes the texts, after the fix runs).

## Approach

- `gitx.FileLines`: first resolve the commit (`rev-parse --verify <commit>^{commit}`); any error there is an error. Then look the path up with `git ls-tree -z --full-tree <commit> -- <path>` (ls-tree matches paths literally, and exits 0 with no entry for a missing path); an entry whose name is exactly the path and whose type is `blob` is a file, no entry or another type is "not a file", and a git error is an error. Then read the blob as today.
- `stage/review.go`: write the location as a CommonMark code span whose backtick fence is one longer than the longest backtick run in it (padded with a space when it starts or ends with a backtick), after escaping control characters (newline, tab and so on) Go-style, so the line stays one line.
- Tests in `gitx` and `stage`. Runner code is not touched (#19 is in parallel).

## Steps

- [x] Tests that fail because of the bug: `FileLines` on a broken repository and on a cancelled context returns an error; the review stage fails with `cannot read ...` when git cannot read the commit; a path with a backtick or newline gives a one-line, well-formed `Location:` line. Then the fix in `gitx.FileLines` and `fixLocations`.
- [x] Tests for every D4 bad path form (absolute, backslash, empty segment, `.`, `..`) and the 64 KiB overflow from the prefix (and a body that fits exactly).

## Acceptance criteria

- R1: a git failure other than "not found" while checking a location fails the run as D5 requires.
- R2: every D4 bad path form is tested.
- R3: the 64 KiB overflow caused by the `Location:` prefix is tested.
- R4: a backtick or newline in the path does not break the `Location:` line.

## Decisions

- **D1** [gap] Work plan #7 D5: a `review.commit` that git cannot resolve while checking a location is a read error (`failed: cannot read <path> at <sha>: ...`), not "not a file". Why: the stage has already checked HEAD is that commit, so failing to resolve it means git is broken, not that the path is absent.
- **D2** [gap] Work plan #7 D4: in the ``Location:`` line, control characters in the path (Unicode category Cc: newline, tab, the rest of C0, DEL and C1) are written as Go escapes (`\n`, `\t`, `\x01`), and the path is wrapped in a backtick fence one longer than its longest backtick run, with a space inside each end when it starts or ends with a backtick (CommonMark code span rules). Why: issue #29 R4 asks for the line to survive these characters, and escaping keeps the location visible where rejecting would lose it.
- **D3** [gap] Work plan #7 D1: a symlink at the commit (a blob of mode 120000) counts as a file and its lines are those of the link text; a submodule (a commit entry) is not a file. Why: this is what `cat-file -t` reported before and the worker's own check is not specified further.

## Verification

Standard checks.

## Risks and open questions

None.

## Outcome

- `gitx.FileLines` resolves the commit, then finds the path with `ls-tree`; only "no blob entry named exactly this path" is "not a file", every git failure is an error, so the review stage fails with D5's reason. Tests cover an unknown commit, a done context, a non-repository and a missing tree object in `gitx`, and a deleted object store in the stage.
- `fixLocations` writes the location with `codeSpan` (D2). Tests cover backticks, leading/trailing backticks and control characters.
- New stage tests cover every D4 bad path form and the 64 KiB boundary after the prefix (exactly 64 KiB is kept, one byte more is failed).
- No deviations. Runner code is untouched (#19). No follow-up issues.
