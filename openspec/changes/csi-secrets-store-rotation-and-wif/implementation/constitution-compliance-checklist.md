# Constitution Compliance Checklist

**Change:** csi-secrets-store-rotation-and-wif  
**Task:** T1_3  
**Review date:** 2026-07-10  
**Reviewer:** ControllerLogic_Agent (automated review)  
**Primary file:** `pkg/operator/starter.go`  
**Supporting files:** `pkg/operator/rotation.go`, `pkg/operator/csidriver_asset.go`, `go.mod`, `vendor/`

**Sign-off:** All reviewed principles **PASS**. No open violations.

---

## Principle I — Single Controller Pattern (CSIControllerSet only)

| Check | Status | Evidence |
|-------|--------|----------|
| Single `CSIControllerSet` chain in `RunOperator` | **PASS** | `starter.go:83–159` — one `NewCSIControllerSet` with method chaining |
| No controller-runtime / separate reconciler | **PASS** | No `sigs.k8s.io/controller-runtime` imports in `pkg/operator/` |
| New capability via hooks/informers, not new loop | **PASS** | Rotation = `WithSecretRotationDaemonSetHook`; dynamic CSIDriver = second `WithConditionalStaticResourcesController` + `NewDynamicCSIDriverAssetFunc` |

---

## Principle III — No Custom CRD Types

| Check | Status | Evidence |
|-------|--------|----------|
| Uses standard `ClusterCSIDriver` GVR | **PASS** | `starter.go:54–55` — `clustercsidrivers` / `ClusterCSIDriver` |
| No repo-local `api/` types package | **PASS** | No top-level `api/` directory; types from `github.com/openshift/api` |
| Feature reads `driverConfig.secretsStore` from upstream API | **PASS** | `rotation.go`, `csidriver_asset.go` use `opv1.SecretsStore*` vendored types |

---

## Principle IV — Managed/Unmanaged/Removed Gating

| Check | Status | Evidence |
|-------|--------|----------|
| Operator marked removable | **PASS** | `starter.go:86–88` — `WithManagementStateController(..., true)` |
| Static resources gated on `getOperatorSyncState` | **PASS** | `starter.go:104–109`, `134–139` — Managed/Removed predicates |
| Split dynamic CSIDriver controller uses same gating | **PASS** | Comment at `starter.go:118–119` references Principle IV; identical predicates |
| Error fallback to Unmanaged | **PASS** | `getOperatorSyncState` at `starter.go:193–214` |

---

## Principle VIII — Trusted CA Bundle Hook Preserved

| Check | Status | Evidence |
|-------|--------|----------|
| `WithCABundleDaemonSetHook` registered | **PASS** | `starter.go:150–154` |
| Rotation hook coexists without removing CA hook | **PASS** | Both hooks passed to `WithCSIDriverNodeService` (`starter.go:150–158`) |
| Unit regression test exists | **PASS** | `rotation_test.go:316–364` — `TestCABundleAndRotationHooksCoexist` |
| Trusted CA ConfigMap asset unchanged | **PASS** | `cabundle_cm.yaml` in static asset list (`starter.go:97`) |

---

## Principle X — Vendor Mode (No Hand-Edited Vendor)

| Check | Status | Evidence |
|-------|--------|----------|
| `openshift/api` via go.mod pseudo-version | **PASS** | `go.mod`: `v0.0.0-20260709102940-580f1c1ba691` |
| Types consumed from vendor, not copied locally | **PASS** | `vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go` |
| No hand-edits detected in feature paths | **PASS** | Feature logic in `pkg/operator/` only; vendor is standard upstream tree |

---

## Task-Specific Implementation Notes (from T1_3 payload)

| Requirement | Status | Evidence |
|-------------|--------|----------|
| `WithCABundleDaemonSetHook` + `WithSecretRotationDaemonSetHook` both registered | **PASS** | `starter.go:150–158` |
| `csidriver.yaml` in separate controller call | **PASS** | `SecretsStoreDynamicCSIDriverController` at `starter.go:110–139` |
| `clusterCSIDriverInformer` in `optionalInformers` | **PASS** | `starter.go:149` — `[]factory.Informer{clusterCSIDriverInformer.Informer()}` |
| Informer scoped (no cluster-wide namespace watch) | **PASS** | `starter.go:46` — `NewKubeInformersForNamespaces(kubeClient, operatorNamespace, "")` |

---

## Violations

**None open.**

---

## Downstream Handoff (T5_2)

This checklist satisfies T1_3 acceptance criteria and may be cited in final PR review as constitution compliance evidence for Principles I, III, IV, VIII, and X.
