# Work plan: Address stage test gaps from the review of #31

**Issue:** [#32](https://github.com/mayusuk/agent-loop-handlers/issues/32) (bug)
**Branch:** `issue/32-address-stage-test-gaps-from-the-review`
**Base:** `origin/main @ 7a85584` (checked before branching)
**Status:** Done

## Summary

Adds the address-stage tests the review of #31 (PR for #8) found missing: a `base_update` merge with its parents reversed, a merge of three parents, `blocked` and `needs_owner` in `base_update` mode, and an empty `change.base_tip`. Renames a test that was named after the wrong decision. No production code changes.

## Context

`internal/stage/address.go` already handles every case (`checkBaseMerge` checks the parent count and the first parent; the base_tip check covers `""`; `agent` returns blocked/needs_owner before any push). The gap is test coverage only, so there is no failing test to write first: each new test pins existing behaviour that #8 work plan D2 and D3 describe. `TestD2_AddressWithoutAddressObjectIsFailed` tests the INTEGRATION-SPEC §5.2 rule that an address run carries an `address` object, not D2.

## Approach

Only `internal/stage/address_test.go`: add two cases to `TestFR5AC6_WrongShapeIsFailedAndNotPushed` (reversed parents, three parents) built with `git commit-tree`; add a `base_update` test for blocked and needs_owner; turn the base_tip test into a table of no `change` and empty `base_tip`; rename the address-object test to `TestI5_2_...`.

## Steps

- [x] Add a `base_update` merge with its parents reversed (base, input head) and a merge of three parents, both built with `git commit-tree`, to `TestFR5AC6_WrongShapeIsFailedAndNotPushed`: each is failed and not pushed.
- [x] Add `TestD3_BaseUpdateBlockedAndNeedsOwnerPushNothing`: `blocked` and `needs_owner` in `base_update` mode push nothing.
- [x] Turn `TestD2_BaseUpdateWithoutBaseTipIsFailed` into a table of no `change` and an empty `change.base_tip`: each is failed before claude runs.
- [x] Rename `TestD2_AddressWithoutAddressObjectIsFailed` to `TestI5_2_AddressWithoutAddressObjectIsFailed`.

## Acceptance criteria

- A base_update merge with parents (base, input head) is failed and not pushed.
- A base_update merge with three parents is failed and not pushed.
- `blocked` and `needs_owner` in base_update mode push nothing (#8 D3).
- An empty `change.base_tip` with `change` present is failed before claude runs.
- `TestD2_AddressWithoutAddressObjectIsFailed` is renamed.

## Decisions

None.

## Verification

Standard checks only.

## Risks and open questions

None.

## Outcome

Added the four missing cases and the rename in `internal/stage/address_test.go`, as planned. All passed against the existing code, so no production change was needed. No follow-up issues.
