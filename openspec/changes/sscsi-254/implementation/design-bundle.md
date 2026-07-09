# Design Bundle — Task T7_4

**Change:** sscsi-254
**Task:** T7_4 — E2E upgrade-preservation + no-`driverConfig` default-parity scenarios
**Assigned Agent:** Testing_Agent
**Phase:** Phase 7: E2E Scenarios (highest-risk e2e set per `plan.md` §7)

## Task T7_4 Payload (from tasks.md §4)

- **Objective:** Implement the source EP's e2e upgrade scenarios: minimal CR with no existing tokenRequests; minimal CR with pre-existing manually-patched Azure WIF audiences (verify preservation, no spec-hash change, no delete+recreate); DaemonSet args unchanged; post-upgrade opt-in.
- **Target file(s):** Per `T7_1`'s discovery output — `hack/e2e.sh`.
- **Non-goals / forbidden edits:** None beyond staying within upgrade-scenario scope.
- **Implementation notes:** This is the highest-risk e2e set per `plan.md` §7 — pay particular attention to confirming the `CSIDriver` object's spec-hash does **not** change (i.e. no unnecessary delete+recreate) when nothing should have changed.
- **Acceptance criteria:** `make test-e2e` passes these scenarios; traces to `specs.md` SC-005, User Story 3, FR-010.
- **Downstream handoff:** Upgrade-safety behavior e2e-verified — this closes the feature's highest-priority risk.

## Critical carry-over from T7_3's design finding — MUST resolve in this task

`T7_3`'s task report flagged: `tokenRequests.type` is a **one-way, CEL-enforced transition** on the singleton `ClusterCSIDriver` — once `"Managed"`, it can never revert to `"Unmanaged"`. `T7_3` already wired its `Managed`-audience scenario calls at the very bottom of `hack/e2e.sh`'s execution block, behind an explicit comment marker stating that is the *last safe insertion point* for anything depending on `"Unmanaged"` behavior.

**This task's own scenarios are exactly that "Unmanaged"-dependent case** (no-`driverConfig` default parity + preservation of pre-existing/manually-set audiences while `Unmanaged`/omitted). Per `T7_3`'s explicit handoff instruction, this task MUST:
1. Insert its new function calls **before** `T7_3`'s WIF-block marker/calls in the execution block at the bottom of `hack/e2e.sh` (a reorder, not an append).
2. Its own final scenario — "post-upgrade opt-in" (adopting a previously-preserved audience under `Managed`) — is itself a transition to `Managed`, so it must be the *last* Unmanaged-dependent call, immediately before `T7_3`'s block.

## Scenario design

1. `test_upgrade_no_driverconfig_default_parity` — with no `driverConfig` at all, `CSIDriver.spec.tokenRequests` must stay unset (FR-010, byte-for-byte parity with pre-feature behavior).
2. `test_upgrade_preserves_manually_patched_tokenrequests` — manually `oc patch csidriver` with an externally-set Azure audience (simulating a pre-existing/out-of-band config), confirm the operator's periodic reconciliation preserves it verbatim (FR-006) and does **not** delete+recreate the object (compare `metadata.uid` before/after a resync wait, per this task's explicit acceptance criterion).
3. `test_upgrade_daemonset_args_unchanged` — confirm the unrelated tokenRequests-only patch above does not perturb the DaemonSet's rotation args (still at built-in defaults).
4. `test_upgrade_post_opt_in_to_managed` — explicit administrator opt-in, adopting the previously-preserved audience under `Managed` (User Story 3's concluding acceptance scenario). This is the last `Unmanaged`-dependent call in the whole script.

## Verification commands (repo-assessment.md §12 style)

`oc get csidriver ${PROVISIONER_NAME} -o jsonpath='{.spec.tokenRequests}'`, `-o jsonpath='{.metadata.uid}'`, `oc get ds -n ${E2E_PROVIDER_NAMESPACE} ${SECRETS_STORE_NODE_DAEMONSET} -o jsonpath='{...containers[?(@.name=="csi-driver")].args}'`.
