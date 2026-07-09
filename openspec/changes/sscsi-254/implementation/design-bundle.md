# Design Bundle — Task T4_2

**Change:** sscsi-254
**Task:** T4_2 — Register hook alongside existing CA-bundle hook
**Assigned Agent:** ControllerLogic_Agent
**Phase:** Phase 4: DaemonSet Rotation Hook

## Constitution excerpts (binding)

> **Principle VIII — Trusted CA Bundle Propagation Is Mandatory for DaemonSet:** The DaemonSet MUST always be deployed with the CA bundle hook. Any change to the DaemonSet configuration must preserve this hook.

## Task T4_2 Payload (from tasks.md §4)

- **Objective:** Add `T4_1`'s hook as an additional variadic argument to `WithCSIDriverNodeService(...)`.
- **Target file(s):** `pkg/operator/starter.go:104-116` (this is the pre-T2_2 line reference from `repo-assessment.md`; the actual call has since shifted down due to `T2_2`'s informer wiring).
- **Non-goals / forbidden edits:** Must not remove or reorder the existing `csidrivernodeservicecontroller.WithCABundleDaemonSetHook(...)` argument (Constitution Principle VIII).
- **Implementation notes:** Simple additive change — append the new hook to the existing call's variadic hook list.
- **Acceptance criteria:** `make verify && make test-unit` passes; manual verification: `oc get ds ... -o jsonpath='{...args}'`. Traces to `plan.md` Phase 4.
- **Downstream handoff:** Feature-complete DaemonSet reconciliation ready for `T7_2` e2e coverage.

## Current `starter.go` state (post `T2_2`, `T3_1`, `T3_2`, `T4_1`)

The `WithCSIDriverNodeService(...)` call currently registers only `csidrivernodeservicecontroller.WithCABundleDaemonSetHook(operatorNamespace, trustedCAConfigMap, configMapInformer)`. `T2_2` already constructed `clusterCSIDriverInformer` (typed `ClusterCSIDriver` informer) in the same function — `T4_1`'s `withSecretsStoreRotationDaemonSetHook` needs `clusterCSIDriverInformer.Lister()` as its argument.

## Execution approach

Add `withSecretsStoreRotationDaemonSetHook(clusterCSIDriverInformer.Lister())` as an additional variadic argument to the existing `WithCSIDriverNodeService(...)` call, after the CA-bundle hook (order doesn't matter functionally — both hooks mutate different, non-overlapping parts of the container spec — but preserving the CA-bundle hook first, unmodified, satisfies Principle VIII literally).
