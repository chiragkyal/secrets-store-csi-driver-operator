# Implementation Report: SSCSI-254

**Feature:** Configurable Secret Rotation and Workload Identity Federation for the Secrets Store CSI Driver  
**Change:** sscsi-254 | **Completed:** 2026-07-01  
**Mode:** Working-folder (no fork/PR — changes directly in local checkout)

---

## Summary

All operator logic for SSCSI-254 has been implemented and unit-tested. The feature enables cluster administrators to configure secret rotation behavior and workload identity federation (WIF) token audiences via `ClusterCSIDriver.spec.driverConfig.secretsStore`, propagating settings to both the `CSIDriver` object and the DaemonSet node plugin.

---

## Files Changed

| File | Change |
|------|--------|
| `go.mod` / `go.sum` | Bumped openshift/{api,client-go,library-go} to June/July 2026 versions |
| `pkg/operator/starter.go` | Added 7 new functions (+~150 lines) |
| `pkg/operator/starter_test.go` | Added 5 test functions, 22 new cases (+~400 lines) |
| `config/manifests/stable/...clusterserviceversion.yaml` | Updated `spec.description` |
| `vendor/` | Regenerated for updated dependencies |

---

## Task Completion

| Task | Title | Status |
|------|-------|--------|
| T1_1 | go.mod — openshift/api bump | ✓ DONE |
| T2_1 | `getRotationConfig` | ✓ DONE |
| T2_2 | `getTokenRequests` + `liveCSIDriverTokenRequests` | ✓ DONE |
| T3_1 | `getClusterCSIDriver` + `enrichedCSIDriverAssetFunc` | ✓ DONE |
| T4_1 | `rotationArgsDaemonSetHook` + `setArg` | ✓ DONE |
| T4_2 | `RunOperator` wiring | ✓ DONE |
| T5_1 | Unit tests — helpers (18/18) | ✓ DONE |
| T5_2 | Unit tests — hooks (26/26) | ✓ DONE |
| T6_1 | OLM CSV description | ✓ DONE |
| T7_1 | E2E tests | ⏸ DEFERRED (live cluster) |

---

## New Functions in starter.go

| Function | Purpose |
|----------|---------|
| `getRotationConfig(*ClusterCSIDriverSpec)` | Returns `(requiresRepublish, enableRotation, pollInterval)` with defaults |
| `getTokenRequests(ctx, *ClusterCSIDriverSpec, dynamic.Interface)` | Returns desired `[]storagev1.TokenRequest` (Managed or live-read) |
| `liveCSIDriverTokenRequests(ctx, dynamic.Interface)` | Reads live `CSIDriver.spec.tokenRequests` for Unmanaged preservation |
| `getClusterCSIDriver(ctx, dynamic.Interface)` | Fetches full `*opv1.ClusterCSIDriver` via dynamic client |
| `enrichedCSIDriverAssetFunc(namespace, dynamic.Interface)` | Returns `resourceapply.AssetFunc` — enriches `csidriver.yaml` at reconcile time |
| `rotationArgsDaemonSetHook(dynamic.Interface)` | Returns `DaemonSetHookFunc` — sets rotation args on `csi-driver` container |
| `setArg(args, prefix, value)` | Replaces arg by prefix or appends |

---

## Key Design Deviations

| Deviation | Reason |
|-----------|--------|
| `getRotationConfig` / `getTokenRequests` accept `*ClusterCSIDriverSpec` (not `operatorClient`) | `GetOperatorState()` returns generic `*OperatorSpec` — no type assertion to `ClusterCSIDriverSpec` possible |
| `enrichedCSIDriverAssetFunc` uses `sigs.k8s.io/yaml` + `encoding/json` | `resourceread.ReadCSIDriverV1OrDie` not vendored |
| `DaemonSetHookFunc` has `(*OperatorSpec, *DaemonSet)` signature | First param ignored — CCD read fresh via `dynamicClient` |
| T6_1: description-only CSV update | `ClusterCSIDriver` is platform CRD — `specDescriptors` not applicable in this bundle |
| T7_1: deferred | Requires live OpenShift cluster |

---

## Test Coverage

| Suite | Cases |
|-------|-------|
| TestGetOperatorSyncState | 4 |
| TestGetRotationConfig | 7 |
| TestGetTokenRequests | 7 |
| TestEnrichedCSIDriverAssetFunc | 4 |
| TestRotationArgsDaemonSetHook | 4 |
| **Total** | **26/26 PASS** |

`make check` (verify + test-race): **PASSED**

---

## Remaining Actions

1. **T7_1 E2E tests** — run `hack/e2e.sh` on a live OpenShift cluster covering all 9 scenarios in `task-reports/T7_1.md`
2. **PR** — open against `github.com/chiragkyal/secrets-store-csi-driver-operator` targeting main; link to EP and SSCSI-254
3. **openshift/api dependency** — confirm the exact `openshift/api` version used here (`v0.0.0-20260626094904-39631f42b31b`) lands in the release stream; coordinate with the API PR review if needed
