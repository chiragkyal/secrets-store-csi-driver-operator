# Design Bundle — Task T7_2

**Change:** sscsi-254
**Task:** T7_2 — E2E rotation scenarios (enable/disable/custom-interval)
**Assigned Agent:** Testing_Agent
**Phase:** Phase 7: E2E Scenarios

## Task T7_2 Payload (from tasks.md §4)

- **Objective:** Implement the source EP's e2e rotation scenarios: no-`driverConfig` defaults; custom interval; `"None"` disables; toggle back to `"Custom"`.
- **Target file(s):** Per `T7_1`'s discovery output — new bash functions in `hack/e2e.sh`.
- **Implementation notes:** Verify via `oc get csidriver .../oc get ds ...` commands per `repo-assessment.md` §12.
- **Acceptance criteria:** `make test-e2e` (requires live cluster) passes these scenarios; traces to `specs.md` SC-001/SC-002, User Story 1.

## T7_1 discovery constraints (binding on this task)

> New scenarios must be added as bash functions following the `test_xxx() { ...; return $?; }` convention, wired into the sequential execution block — not Go test files.

## CORRECTED field names/formats (per T1_1's actual merged API finding — supersedes the EP's original assumptions)

- Field is `rotationPollIntervalSeconds` (not the EP's `minimumRefreshAge`).
- This repo's hook (`T4_1`) formats the interval arg as `"<N>s"` (e.g. `--rotation-poll-interval=300s`), not `"5m0s"` — e2e assertions must match the ACTUAL implementation's output format, not the EP's original assumption.
- `requiresRepublish` mirrors `secretRotation.type` exactly as implemented in `T3_1` (`false` only when explicitly `"None"`).

## Execution approach

Add 4 new bash functions to `hack/e2e.sh` (`test_rotation_defaults`, `test_rotation_custom_interval`, `test_rotation_disabled`, `test_rotation_toggle_back_to_custom`) plus a `test_rotation_cleanup` helper, each patching `ClusterCSIDriver` via `oc patch --type=merge` and verifying via the exact `oc get csidriver`/`oc get ds ... jsonpath` commands from `repo-assessment.md` §12. Use `oc rollout status daemonset/... --timeout=60s` to wait for the DaemonSet rollout robustly (rather than a fixed `sleep`). Wire into the sequential execution block after the existing `test_pod_with_secret`/`test_teardown` steps (rotation config changes trigger a DaemonSet rollout that could disrupt a concurrently-running pod-mount test if run earlier).
