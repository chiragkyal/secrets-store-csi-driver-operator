# Design Bundle — Task T7_3

**Change:** sscsi-254
**Task:** T7_3 — E2E WIF scenarios (single/multi-audience)
**Assigned Agent:** Testing_Agent
**Phase:** Phase 7: E2E Scenarios

## Task T7_3 Payload (from tasks.md §4)

- **Objective:** Implement the source EP's e2e WIF scenarios: single audience; multiple audiences (AWS + Azure); custom `expirationSeconds`; clearing via empty list; Unmanaged→Managed transition.
- **Target file(s):** Per `T7_1`'s discovery output — new bash functions in `hack/e2e.sh`.
- **Acceptance criteria:** `make test-e2e` (requires live cluster) passes these scenarios; traces to `specs.md` SC-003/SC-004/SC-007, User Story 2.

## ⚠️ Critical sequencing constraint discovered during design (worth flagging as a deviation)

`tokenRequests.type` is a **one-way, CEL-enforced transition**: once set to `"Managed"` on the singleton `ClusterCSIDriver`, it can **never** revert to `"Unmanaged"` — confirmed both in the source EP and in the actual merged API's CEL rule (`T1_2`'s vendored types). Since there is only **one** `ClusterCSIDriver` object per cluster (singleton, `secrets-store.csi.k8s.io`), **any e2e scenario that needs `Unmanaged` behavior (preservation of pre-existing/externally-configured audiences — this task payload's own "Unmanaged→Managed transition" case, and all of `T7_4`'s preservation scenarios) MUST run and complete before this task's `Managed`-audience scenarios execute**, or they become permanently untestable for the remainder of the e2e run.

`plan.md`/`tasks.md` marked `T7_2`/`T7_3`/`T7_4` `Parallel OK: Yes` based on the assumption they were independent — this is **not actually true** for `T7_3` vs. `T7_4` specifically, due to this shared-singleton, one-way-transition constraint. This was not visible until actually implementing the e2e scenarios against the real CEL semantics.

**Resolution for this task**: implement `T7_3`'s `Managed`-audience functions now (single/multiple/clear/custom-expiration), but insert their execution-block wiring **after** a clearly-commented placeholder marker, so that `T7_4` (next task) can insert its `Unmanaged`-preservation scenario calls **before** this block when it lands — `T7_4` will need to reorder the execution block, not just append to it. This is flagged explicitly in this task's Deviations for `T7_4` to act on.

## API shape (confirmed unchanged from EP assumptions — per T1_1)

`tokenRequests` API shape matches the EP's original proposal exactly (unlike `secretRotation`'s field-name difference) — `type: Managed|Unmanaged`, `managed.audiences: [{audience, expirationSeconds}]`.

## Execution approach

Add `test_wif_managed_single_audience`, `test_wif_managed_multiple_audiences`, `test_wif_managed_clear_audiences` to `hack/e2e.sh`, using `oc get csidriver ... -o jsonpath='{.spec.tokenRequests[*].audience}'` (and per-audience `expirationSeconds` lookups) for verification, following `repo-assessment.md` §12's command style. Wire into the execution block after `T7_2`'s rotation block, with an explicit comment marking this as the **last** safe insertion point for any future `Unmanaged`-dependent scenario.
