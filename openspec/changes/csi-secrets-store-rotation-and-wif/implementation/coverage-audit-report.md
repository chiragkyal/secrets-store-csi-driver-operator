# Test Coverage Audit Report

**Change:** csi-secrets-store-rotation-and-wif  
**Task:** T2_1  
**Date:** 2026-07-10  
**Sources compared:** `openspec/inputs/ep.md` §Test Plan, `specs.md` acceptance scenarios, `pkg/operator/rotation_test.go`, `pkg/operator/csidriver_asset_test.go`, `hack/e2e.sh`

**Verdict:** Unit coverage is **strong** with **1 recommended unit gap** for T2_2. E2E has **known gaps** routed to T4_1/T4_2/T4_3 (not T2_2). **T2_2 is NOT skipped.**

---

## T2_1 mandatory checks (from task payload)

| Check | Status | Evidence |
|-------|--------|----------|
| `TestDefaultPathMatchesPreFeatureBaseline` (FR-003/FR-012) | **COVERED** | `rotation_test.go:426–469` |
| Full `tokenRequests` preservation matrix | **COVERED** | `csidriver_asset_test.go:65–184` — 8 cases incl. Unmanaged, nil audiences, empty clear |
| Hook missing-container error path | **COVERED** | `rotation_test.go:295–314` — `TestWithSecretRotationDaemonSetHookMissingContainer` |

---

## Unit tests vs `ep.md` Test Plan

| ep.md unit scenario | Test function | Status |
|---------------------|---------------|--------|
| Rotation config extraction (nil/disabled/custom/default) | `TestGetSecretRotationConfig` | **COVERED** (7 cases) |
| CSIDriver `requiresRepublish` mapping | `TestGetRequiresRepublish` | **COVERED** |
| CSIDriver `tokenRequests` mapping | `TestGetTokenRequests` | **COVERED** |
| DaemonSet hook arg prefix replacement | `TestSetArg`, `TestWithSecretRotationDaemonSetHook` | **COVERED** |
| DaemonSet hook missing container error | `TestWithSecretRotationDaemonSetHookMissingContainer` | **COVERED** |
| Dynamic asset func (`requiresRepublish` + `tokenRequests`) | `TestNewDynamicCSIDriverAssetFunc` | **PARTIAL** — 4 cases; **missing Managed audiences end-to-end** |
| Namespace substitution (non-CSIDriver assets) | — | **NOT COVERED** in feature test files (pre-existing `replaceNamespaceFunc`; defer) |
| tokenRequests nil-path preservation matrix | `TestGetTokenRequests` | **COVERED** |
| Default-path byte-for-byte baseline (regression) | `TestDefaultPathMatchesPreFeatureBaseline` | **COVERED** |
| CA bundle + rotation hook coexistence | `TestCABundleAndRotationHooksCoexist` | **COVERED** (constitution regression) |

### Unit gap for T2_2

| ID | Gap | Priority | Recommendation |
|----|-----|----------|----------------|
| U1 | `TestNewDynamicCSIDriverAssetFunc` lacks a case where `tokenRequests.type: Managed` with audiences renders both entries on the CSIDriver manifest (with `expirationSeconds`) | **Medium** | Add table case in T2_2 — logic exists in `getTokenRequests` but ep.md expects integrated asset-func coverage |

---

## API integration tests (CRD validation)

| ep.md scenario | Status | Route |
|----------------|--------|-------|
| Managed→Unmanaged immutability | **N/A in operator repo** | `openshift/api` CRD CEL |
| Discriminated union validation | **N/A in operator repo** | CRD admission |
| Bounds rejection (interval/expiration) | **N/A in operator repo** | CRD validation |

No T2_2 action — document in T5_2 if cluster-level negative tests desired.

---

## E2E vs `ep.md` Test Plan

| ep.md E2E scenario | `hack/e2e.sh` function | Status |
|--------------------|------------------------|--------|
| Default rotation (no driverConfig) | — | **GAP** — no dedicated assert of default DS args + `requiresRepublish:true` without patch |
| Custom interval (300s → 5m poll) | `test_rotation_custom_interval` | **PARTIAL** — uses 30s interval; asserts DS arg only, not CSIDriver |
| Rotation disabled (`type: None`) | `test_rotation_toggle` | **PARTIAL** — DS `--enable-secret-rotation=false` only; no CSIDriver `requiresRepublish:false` assert |
| Toggle None → Custom re-enable | `test_rotation_toggle` | **COVERED** |
| Pre-existing tokenRequests preserved on upgrade | — | **GAP** → T4_3 |
| `tokenRequests.type: Unmanaged` preserve | — | **GAP** — no dedicated scenario |
| Managed audiences on CSIDriver | `test_wif_single_audience`, `test_wif_multi_audience` | **COVERED** |
| Managed empty audiences clear (FR-007) | `test_wif_clear_audiences` | **COVERED** (used in WIF cleanup) |
| Unmanaged → Managed transition | `test_wif_single_audience` | **COVERED** (implicit) |
| Multi-cloud audiences + expirationSeconds | `test_wif_multi_audience` | **COVERED** |
| Workload mount with WIF configured | `test_wif_mount_check` | **COVERED** |
| Upgrade scenarios (775–793) | — | **GAP** → T4_3 |
| Remove custom interval → default fallback (specs US3 #2) | — | **GAP** — no unit or e2e |

### E2E gaps (not T2_2 — route to Phase 4)

| ID | Gap | Route |
|----|-----|-------|
| E1 | Assert CSIDriver `requiresRepublish` in rotation e2e | T4_1 |
| E2 | Dedicated default-path e2e (no driverConfig patch) | T4_1 |
| E3 | Custom interval removal / fallback to default | T4_1 or defer |
| E4 | Unmanaged tokenRequests preservation | T4_2 |
| E5 | Full upgrade preservation matrix | T4_3 |

---

## `specs.md` acceptance scenarios

| Scenario | Unit | E2E | Notes |
|----------|------|-----|-------|
| US1 — disable rotation | Hook + config tests | `test_rotation_toggle` | Does not verify runtime secret refresh stop (operator scope) |
| US1 — re-enable rotation | Hook tests | `test_rotation_toggle` | **COVERED** (propagation) |
| US2 — add audience | `TestGetTokenRequests` | `test_wif_single_audience` | **COVERED** |
| US2 — clear via empty list | `TestGetTokenRequests` | `test_wif_clear_audiences` | **COVERED** |
| US3 — set custom interval | Config + hook | `test_rotation_custom_interval` | **COVERED** |
| US3 — remove custom → default | — | — | **GAP** (E3) |
| US4 — multi-cloud audiences | `TestGetTokenRequests` | `test_wif_multi_audience` | **COVERED** |
| Upgrade preserves config | Partial unit preserve tests | — | **GAP** (E5/T4_3) |
| Invalid bounds / Managed→Unmanaged | CRD N/A | — | Out of repo scope |
| Downgrade after Managed | — | — | Open question (T5_2) |

---

## T2_2 disposition

**Do NOT skip T2_2.**

| Action | Item |
|--------|------|
| **Implement in T2_2** | U1 — `TestNewDynamicCSIDriverAssetFunc` Managed audiences case |
| **Defer** | Namespace substitution unit test (pre-existing helper, not feature regression) |
| **Route to T4_1** | E1, E2, E3 |
| **Route to T4_2** | E4 |
| **Route to T4_3** | E5 (upgrade matrix) |

---

## Verification run

```
make test-unit → PASS (pkg/operator cached, race enabled)
```

All existing tests pass; audit identifies gaps without requiring production code changes.
