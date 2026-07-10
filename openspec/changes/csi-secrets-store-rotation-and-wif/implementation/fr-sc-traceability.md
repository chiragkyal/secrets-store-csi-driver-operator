# FR/SC Traceability Matrix

**Change:** csi-secrets-store-rotation-and-wif  
**Branch:** `openspec-cursor-agent-sonnet5` @ `0b6b5b3a`  
**Audit task:** T1_1  
**Date:** 2026-07-10

## Implementation anchors (verified present)

| Component | File | Function / wiring |
|-----------|------|-------------------|
| Rotation config | `pkg/operator/rotation.go` | `getSecretRotationConfig`, `formatRotationInterval`, `setArg` |
| Rotation DaemonSet hook | `pkg/operator/rotation.go` | `WithSecretRotationDaemonSetHook` |
| CSIDriver dynamic fields | `pkg/operator/csidriver_asset.go` | `getRequiresRepublish`, `getTokenRequests`, `NewDynamicCSIDriverAssetFunc` |
| Controller wiring | `pkg/operator/starter.go` | Split `SecretsStoreDynamicCSIDriverController`; `optionalInformers` includes `clusterCSIDriverInformer`; rotation hook registered |
| API types | `vendor/.../openshift/api` | `SecretsStoreDriverType`, `SecretsStoreCSIDriverConfigSpec` @ `580f1c1ba691` |

## Functional requirements

| FR | Requirement summary | Code | Unit test | E2E / manual | Status |
|----|---------------------|------|-----------|--------------|--------|
| FR-001 | Enable/disable rotation via cluster config | `getSecretRotationConfig`, `WithSecretRotationDaemonSetHook` | `TestGetSecretRotationConfig` ("type None"), `TestWithSecretRotationDaemonSetHook` | `test_rotation_toggle` | **Covered** |
| FR-002 | Custom rotation interval (bounded) | `getSecretRotationConfig` (Custom branch), `formatRotationInterval` | `TestGetSecretRotationConfig` ("type Custom with explicit…"), hook tests | `test_rotation_custom_interval` | **Covered** (bounds enforced by CRD, not operator) |
| FR-003 | Default rotation when unconfigured | `getSecretRotationConfig` (default branch) | `TestGetSecretRotationConfig` (nil paths), `TestDefaultPathMatchesPreFeatureBaseline` | `test_driver_config_restore` baseline in script comments | **Covered** |
| FR-004 | Configure WIF token audiences | `getTokenRequests` (Managed), `NewDynamicCSIDriverAssetFunc` | `TestGetTokenRequests`, `TestNewDynamicCSIDriverAssetFunc` | `test_wif_single_audience`, `test_wif_multi_audience` | **Covered** |
| FR-005 | Preserve existing tokenRequests when omitted | `getTokenRequests` (default/Unmanaged/nil paths) | `TestGetTokenRequests` (preserve cases), asset func tests | E2E restore helpers; upgrade comments in `hack/e2e.sh` | **Covered** |
| FR-006 | Reject revert Managed→Unmanaged | — (CRD CEL only) | — | — | **N/A at operator** — upstream CRD; no operator re-validation (by design) |
| FR-007 | Clear via empty `managed.audiences` | `getTokenRequests` (empty slice) | `TestGetTokenRequests` ("explicit empty audiences list clears") | `test_wif_clear_audiences` | **Covered** |
| FR-008 | Reject out-of-bounds values at submission | — (CRD validation) | — | — | **N/A at operator** — CRD admission in `openshift/api` |
| FR-009 | Propagate without manual restart | `starter.go` informer + library-go resync | Hook/asset integration tests | `test_wait_ds_arg`, `test_wait_csidriver_audiences` | **Covered** |
| FR-010 | Retain config across upgrades/restarts | Defaults + preservation logic | `TestDefaultPathMatchesPreFeatureBaseline` | Documented upgrade scenarios in e2e comments | **Partial** — no automated full upgrade e2e (T4_3) |
| FR-011 | Disabled rotation stops periodic refresh | `getRequiresRepublish` mirrors rotation off | `TestGetRequiresRepublish`, asset func "type None" | `test_rotation_toggle` (disable path) | **Covered** |
| FR-012 | No change for non-opted-in clusters | Default paths in rotation + asset func | `TestDefaultPathMatchesPreFeatureBaseline`, nil-path tests | E2E baseline comments | **Covered** |

## Success criteria

| SC | Summary | Verification | Status |
|----|---------|--------------|--------|
| SC-001 | Disable rotation, confirm stop within reconciliation cycle | `test_rotation_toggle` | **Covered** (DaemonSet arg assertion) |
| SC-002 | Custom interval observed | `test_rotation_custom_interval` | **Covered** |
| SC-003 | WIF auth to cloud provider | E2E mount continuity only | **Partial** — propagation + mount, not full IAM |
| SC-004 | Multi-cloud audiences independently usable | `test_wif_multi_audience` | **Partial** — CSIDriver audiences + mount, not full IAM |
| SC-005 | Upgrade retains cadence + manual tokenRequests | E2E comments / manual runbook | **Gap** — no dedicated automated upgrade test (T4_3) |
| SC-006 | 100% invalid submissions rejected at admission | CRD only | **N/A in this repo** — requires API integration tests in `openshift/api` or cluster admission tests |
| SC-007 | 100% Managed→Unmanaged revert rejected | CRD CEL only | **N/A in this repo** — no negative admission e2e in `hack/e2e.sh` |

## User stories (P1/P2)

| Story | Code + tests | Status |
|-------|--------------|--------|
| US1 (P1) rotation on/off | FR-001/011 mapping | **Covered** |
| US2 (P1) WIF audiences | FR-004/005 mapping | **Covered** (propagation scope) |
| US3 (P2) custom interval | FR-002 mapping | **Covered** |
| US4 (P2) multi-cloud audiences | FR-004 + `test_wif_multi_audience` | **Covered** (propagation scope) |

## Gaps for T2_1 (downstream)

1. **SC-005 / FR-010 upgrade path** — No automated upgrade-preservation e2e; manual runbook only (Plan Phase 4 / T4_3).
2. **SC-006 / SC-007 negative admission** — Out of operator scope; document as CRD-owned, not operator test gap unless cluster-level tests added.
3. **SC-003 / SC-004 full cloud federation** — Explicitly out of scope per repo-assessment; e2e verifies propagation + secret mount only.
4. **Downgrade after Managed** — No spec, code, or test (Plan §8 #1 open question).
5. **FR-006 operator-side** — Confirm no duplicate immutability check was added (verified: comments in `csidriver_asset.go` only).

## Constitution compliance snapshot (feeds T1_3)

| Principle | Evidence |
|-----------|----------|
| I — CSIControllerSet only | Hooks in `starter.go`, no new reconciler |
| IV — Management state gating | Both static controllers use `getOperatorSyncState` |
| VIII — CA bundle hook | `TestCABundleAndRotationHooksCoexist`; both hooks in `starter.go` |
| X — Vendored API | Types from `openshift/api@580f1c1ba691`, not hand-edited |
