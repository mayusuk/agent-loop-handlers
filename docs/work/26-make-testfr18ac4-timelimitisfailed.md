# Work plan: Make TestFR18AC4_TimeLimitIsFailed robust on slow runners

**Issue:** [#26](https://github.com/mayusuk/agent-loop-handlers/issues/26) (bug)
**Branch:** `issue/26-make-testfr18ac4-timelimitisfailed`
**Base:** `origin/main @ 7a85584` (checked before branching)
**Status:** Done

## Summary

TestFR18AC4_TimeLimitIsFailed gave the fake claude as little as about one second to commit before the stop time, so it could flake on a slow CI runner. The test now sets the deadline about 8 seconds out, giving the fake about 5 seconds, while still proving the same things.

## Context

- FR-18 AC4: a run still going at the deadline fails with exactly "time limit reached". Plan D3: the stage stops claude at the deadline minus `--deadline-margin` (default 2m).
- Root cause: the fixture writes `input.json`'s deadline as RFC 3339 in whole seconds (`deadline.UTC().Format(time.RFC3339)`, `implement_test.go`), which drops up to 1s. With `now+4s` and a 2s margin the stop time is 1–2s after the test starts, minus the time spent writing input and starting the process, so the fake may be stopped before it commits. Then `HEAD == f.head` and the test reports "claude did not run" although the stage behaved correctly.
- A timing flake cannot be made to fail deterministically on a fast machine, so there is no failing-first test; the fix is to widen the window.

## Approach

In `internal/stage/implement_test.go`, set `f.deadline = time.Now().Add(8 * time.Second)` and explain the rounding in a comment. Everything the test proves is kept:
- the reason is exactly "time limit reached";
- claude ran and committed, which shows the 2s margin was used: with the 2m default an 8s deadline is already past the stop time, so claude would never start (as `TestPlanD3_DefaultMarginStopsBeforeStarting` shows);
- the run ends well before the fake's 30s sleep (bound stays at 15s; the stop is at about 6s and the fake exits on SIGTERM);
- nothing is pushed.

## Steps

- [x] Widen the deadline in TestFR18AC4_TimeLimitIsFailed to now+8s, with a comment on the whole-second rounding.

## Acceptance criteria

- The fake claude has several seconds (not about one) to commit before the stop time.
- The test still proves FR-18 AC4: exact reason, claude ran under the 2s margin, stopped long before its 30s sleep, nothing pushed.

## Decisions

None.

## Verification

Standard checks; also ran the test repeatedly (`-count=5`) under `-race`.

## Risks and open questions

The test takes about 4s longer.

## Outcome

Done as planned: the deadline is now+8s, the 15s upper bound and every assertion are unchanged. No deviations, no follow-up issues.
