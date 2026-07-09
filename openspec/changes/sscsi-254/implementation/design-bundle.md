# Design Bundle — Task T3_4

**Change:** sscsi-254
**Task:** T3_4 — Unit tests: `CSIDriver` mapping + preservation cascade
**Assigned Agent:** Testing_Agent
**Phase:** Phase 3: Dynamic `CSIDriver` Object Generation (Rotation + WIF Fields)

## Task T3_4 Payload (from tasks.md §4)

- **Objective:** Table-driven coverage of `T3_1`/`T3_2`'s field mapping and all 5 preservation nil-check levels.
- **Target file(s):** New `_test.go` co-located with `T3_1`/`T3_2`'s file(s).
- **Implementation notes:** Cases per the source EP's own Unit Test Plan: each nil-check level with/without existing live `tokenRequests`; `Managed` with populated audiences; `Managed` with empty audiences (clear); `requiresRepublish` mirroring `secretRotation.type`.
- **Acceptance criteria:** `make test-unit` passes; traces to `specs.md` FR-003/004/006/008/011 and Edge Cases.

## Existing coverage (from T3_1/T3_2's mandatory verification tests)

13 tests already cover: pass-through, no-ClusterCSIDriver-yet, rotation mirroring (omitted/Custom/None), Managed with 1 audience, cluster-lister error, preservation on upgrade (no-driverConfig / explicit Unmanaged), live-object-not-found, Managed-overrides-preservation, live-lister-error.

## Gap identified for this task to close

Per the source EP's own Test Plan (`inputs/ep.md` "Multi-cloud WIF scenarios: Multiple audiences (e.g., AWS + Azure): verify CSIDriver has both tokenRequests entries" and "`tokenRequests.type: 'Managed'` with empty `managed.audiences`: verify CSIDriver.spec.tokenRequests is cleared"), two integration-level cases are not yet covered by the existing 13 tests:
1. **Multiple simultaneous audiences** (specs.md FR-004/FR-011) — only a single-audience case exists at the `withSecretsStoreCSIDriverAsset` integration level (the multi-audience case was only tested at `T2_1`'s `ResolveSecretsStoreConfig` unit level, not end-to-end through the `AssetFunc`).
2. **`Managed` with an explicit empty audience list clears tokenRequests even when the live object has pre-existing audiences** — the existing `ManagedOverridesLivePreservation` test only exercises a populated audience list overriding live data, not the explicit-clear case at this integration level.

## Execution approach

Extend `pkg/operator/csidriverasset_test.go` (from `T3_1`/`T3_2`) with 2 new test functions closing these gaps.
