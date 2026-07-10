# Technical Implementation Plan
**Feature:** Configurable Secret Rotation and Workload Identity Federation (SSCSI-254)

## 0. Inputs acknowledged

| Input | Status |
|-------|--------|
| Spec source | SSCSI-254 — validated specs.md (PASS, 93%) |
| Repo assessment pin | `openshift/secrets-store-csi-driver-operator` (working-folder), branch `openspec-cursor-agent-sonnet5`, commit `0b6b5b3a` (tooling_status: FULL) |
| `agents.md` | PROVIDED — AGENTS.md/CLAUDE.md in repo root |
| `spec_validator_results.json` | PROVIDED — validation.json (PASS, 93%, 0 blockers) |
| `constitution.md` | PROVIDED — `openspec/inputs/constitution.md` v1.0.0 (10 core principles + additional constraints) |
| AgentRoutingMode | PROVIDED (per constitution.md metadata) |

## 1. Architectural strategy

### Feature integration approach

This feature extends the existing Secrets Store CSI Driver Operator in two dimensions:

1. **Dynamic CSIDriver object management** — replacing the static `csidriver.yaml` reconciliation with a dynamic `AssetFunc` that programmatically sets `spec.requiresRepublish` and `spec.tokenRequests` on the `CSIDriver` object based on `ClusterCSIDriver` configuration. Per Constitution Principle I, this is expressed as a new hook within the existing `CSIControllerSet` chain — specifically a second `ConditionalStaticResourcesController` instance with a custom `AssetFunc`.

2. **Dynamic DaemonSet argument injection** — adding a `DaemonSetHookFunc` that sets `--enable-secret-rotation` and `--rotation-poll-interval` container arguments based on `ClusterCSIDriver` configuration. Per Constitution Principle I, this is a new hook registered alongside the existing `WithCABundleDaemonSetHook` (Principle VIII: CA bundle hook must be preserved).

Both dimensions read the `ClusterCSIDriver` via the existing dynamic informer (no new informer factory), convert from unstructured to typed `opv1.ClusterCSIDriver`, and extract the relevant `driverConfig.secretsStore` fields. The API types (`SecretsStoreCSIDriverConfigSpec`, `SecretsStoreSecretRotation`, `SecretsStoreTokenRequests`, etc.) are defined in `openshift/api` and vendored into this repo.

### Repo-grounded reality check

**The feature is FULLY IMPLEMENTED on the pinned branch** (`openspec-cursor-agent-sonnet5`, commit `0b6b5b3a`). The repo-assessment §0 and §11.1 confirm:

- `pkg/operator/rotation.go` — `getSecretRotationConfig`, `WithSecretRotationDaemonSetHook`, `setArg`, `formatRotationInterval` are present and tested.
- `pkg/operator/csidriver_asset.go` — `NewDynamicCSIDriverAssetFunc`, `getRequiresRepublish`, `getTokenRequests` are present and tested.
- `pkg/operator/starter.go` — controller wiring includes the dynamic CSIDriver controller and rotation hook.
- Unit tests exist in `rotation_test.go` (17+ cases) and `csidriver_asset_test.go` (15+ cases).

**This plan documents the architectural approach that WAS followed** and identifies remaining verification/integration gaps (E2E tests, OLM CSV alignment) that downstream tasks should address. Phases are framed as verification and gap-closure rather than greenfield implementation.

### Constitution compliance

| Principle | Compliance |
|-----------|------------|
| I. Single Controller Pattern | Compliant — new capability expressed as CSIControllerSet hooks (AssetFunc + DaemonSetHookFunc), not separate reconciler |
| II. Static Assets Are Embedded YAML | Compliant — `csidriver.yaml` remains a static YAML asset; dynamic fields are set programmatically at reconcile time via AssetFunc |
| III. No Custom CRD Types | Compliant — uses existing `ClusterCSIDriver` API extended in `openshift/api` |
| IV. Managed/Unmanaged/Removed | Compliant — dynamic CSIDriver controller uses same `getOperatorSyncState` gating |
| V. Verification-First | Requires verification — unit tests exist; E2E coverage to be confirmed |
| VI. RBAC Least-Privilege | No new RBAC required — existing roles sufficient |
| VII. Namespace Isolation | Compliant — `${NAMESPACE}` used in all assets |
| VIII. CA Bundle Propagation | Compliant — `WithCABundleDaemonSetHook` preserved; coexistence test verifies no clobber |
| IX. OLM Bundle Conventions | Requires verification — CSV alignment not confirmed |
| X. Vendor Mode | Compliant — `openshift/api` vendored at `v0.0.0-20260709102940` |

## 2. Persistence & state

### Kubernetes objects

| Object | Kind | Source of Truth | Role |
|--------|------|----------------|------|
| `ClusterCSIDriver` (`secrets-store.csi.k8s.io`) | `operator.openshift.io/v1` | Administrator input | Carries `driverConfig.secretsStore` with rotation and tokenRequests config |
| `CSIDriver` (`secrets-store.csi.k8s.io`) | `storage.k8s.io/v1` | Operator-reconciled (derived) | Receives `spec.requiresRepublish` and `spec.tokenRequests` from operator |
| `DaemonSet` (`secrets-store-csi-driver-node`) | `apps/v1` | Operator-reconciled (derived) | Receives `--enable-secret-rotation` and `--rotation-poll-interval` container args |

### Operand config/state

- **Rotation flags**: `--enable-secret-rotation=true|false` and `--rotation-poll-interval=Nm` on the `csi-driver` container in `node.yaml`. Defaults: `true` and `2m` (matching pre-feature baseline).
- **requiresRepublish**: Boolean on `CSIDriver.spec`. Mirrors rotation enabled state — `true` when rotation is enabled (default or Custom), `false` when None.
- **tokenRequests**: Array on `CSIDriver.spec`. Managed by operator when `tokenRequests.type == Managed`; preserved from live object otherwise.

### External/platform-injected state

- **CA bundle**: Injected by CNO into `secrets-store-csi-driver-trusted-ca-bundle` ConfigMap via label. Propagated to DaemonSet by `WithCABundleDaemonSetHook`. Unrelated to this feature but must be preserved (Constitution Principle VIII).

## 3. Interfaces & contracts (operator-native)

### 3.1 Kubernetes APIs (CRDs/CRs)

**ClusterCSIDriver extension** (in `openshift/api`, vendored):

| Type | Discriminator | Branches | Immutability |
|------|--------------|----------|-------------|
| `SecretsStoreSecretRotation` | `type` | `None` (disabled), `Custom` (enabled with config) | None |
| `SecretsStoreTokenRequests` | `type` | `Managed` (operator-owned), `Unmanaged` (preserve existing) | `Managed` → `Unmanaged` blocked by CEL rule |
| `CustomSecretRotation` | — | `minimumRefreshAge` (1–31560000s, 0 = omitted) | None |
| `ManagedTokenRequests` | — | `audiences` (`*[]SecretsStoreTokenRequest`, nil = omitted, `&[]{}` = clear) | None |

**CSIDriver** (`storage.k8s.io/v1`):
- `spec.requiresRepublish` — `*bool`, set by `getRequiresRepublish`
- `spec.tokenRequests` — `[]TokenRequest`, set by `getTokenRequests`
- Spec is effectively immutable — changes require delete+recreate (handled by `resourceapply.ApplyCSIDriver` via spec-hash annotation)

### 3.2 Controller/runtime interfaces (internal)

| Interface | File | Purpose |
|-----------|------|---------|
| `getSecretRotationConfig(CSIDriverConfigSpec) → (bool, Duration)` | `rotation.go` | Extract rotation enable/interval from ClusterCSIDriver config |
| `getRequiresRepublish(CSIDriverConfigSpec) → *bool` | `csidriver_asset.go` | Mirror rotation enabled → requiresRepublish |
| `getTokenRequests(CSIDriverConfigSpec, []TokenRequest) → []TokenRequest` | `csidriver_asset.go` | Compute desired tokenRequests with preservation logic |
| `WithSecretRotationDaemonSetHook(lister, name) → DaemonSetHookFunc` | `rotation.go` | DaemonSet hook for rotation args |
| `NewDynamicCSIDriverAssetFunc(...) → AssetFunc` | `csidriver_asset.go` | Dynamic CSIDriver asset rendering |
| `formatRotationInterval(Duration) → string` | `rotation.go` | Render duration preserving "Nm" format for whole minutes |
| `setArg([]string, prefix, value) → []string` | `rotation.go` | Replace or append a flag arg by prefix |

### 3.3 Webhooks / admission (if applicable)

N/A — no webhooks. Validation is enforced by CRD-level CEL rules defined in `openshift/api`.

### 3.4 RBAC / security boundaries (if applicable)

No new RBAC required. The operator already has permissions to read `ClusterCSIDriver` (via the dynamic informer) and manage `CSIDriver` objects (via `ConditionalStaticResourcesController`). The CSIDriver lister uses the existing `kubeInformersForNamespaces.InformersFor("").Storage().V1().CSIDrivers()` — no new cluster-scoped informer.

### 3.5 Packaging / OLM (if applicable)

- The CSV at `config/manifests/stable/` may need updated RBAC if the dynamic CSIDriver controller requires permissions not already granted. This is UNVERIFIED (repo-assessment §11.1 GAP-2).
- No new image references required — the feature uses existing operator and operand images.
- No feature gates — Constitution confirms no feature gate framework exists.

## 4. Dependencies & sequencing graph

### Critical path summary

1. **API types in `openshift/api`** → must be vendored before any operator code can compile against `SecretsStoreDriverType` and related types.
2. **Config extraction logic** (`rotation.go`, `csidriver_asset.go`) → must be in place before controller wiring.
3. **Controller wiring** (`starter.go`) → depends on config extraction + hook functions.
4. **Unit tests** → should co-develop with each code file.
5. **E2E tests** → depend on controller wiring being complete and a live cluster.
6. **OLM / CSV alignment** → can proceed in parallel with E2E once code is stable.

### Parallelizable workstreams

- **Rotation config + DaemonSet hook** (rotation.go) and **CSIDriver asset func + tokenRequests** (csidriver_asset.go) are logically independent and can be developed/tested in parallel.
- **E2E test development** and **OLM CSV review** can proceed in parallel once controller wiring is complete.

### Explicit blockers / external dependencies

- **openshift/api vendor**: The `SecretsStoreDriverType` enum, `SecretsStoreCSIDriverConfigSpec`, and related types must exist in the vendored `openshift/api`. Currently vendored at `v0.0.0-20260709102940` which includes these types.
- **library-go**: `resourceapply.ApplyCSIDriver` with hash-based recreate must be available. Currently vendored at `v0.0.0-20260303171201`.

## 5. Implementation phases (logical sequence; NOT tasks)

### Phase 1: API Vendor Update

- **Goal:** Ensure `openshift/api` is vendored at a commit containing the `SecretsStore` driver config types (`SecretsStoreCSIDriverConfigSpec`, `SecretsStoreSecretRotation`, `SecretsStoreTokenRequests`, etc.).
- **Dependencies:** openshift/api PR defining the types must be merged.
- **Target files:** `go.mod`, `go.sum`, `vendor/github.com/openshift/api/operator/v1/types_csi_driver.go`
- **Required capabilities:** API (vendor management)
- **Verification hooks:** `make verify` (includes verify-deps); confirm `opv1.SecretsStoreDriverType` compiles.

### Phase 2: Secret Rotation Config Extraction + DaemonSet Hook

- **Goal:** Implement `getSecretRotationConfig` to extract rotation enable/interval from `ClusterCSIDriver`, and `WithSecretRotationDaemonSetHook` to set DaemonSet args dynamically. Includes helper functions `setArg` and `formatRotationInterval`.
- **Dependencies:** Phase 1 (API types available).
- **Target files:** `pkg/operator/rotation.go`, `pkg/operator/rotation_test.go`
- **Required capabilities:** OperatorController, Testing
- **Verification hooks:** `make test-unit` — table-driven tests covering nil driverConfig, non-SecretsStore type, type None, type Custom with/without interval, missing container error, hook coexistence with CA bundle hook, pre-feature baseline regression.

### Phase 3: Dynamic CSIDriver AssetFunc + tokenRequests Logic

- **Goal:** Implement `NewDynamicCSIDriverAssetFunc` to render CSIDriver with dynamic `requiresRepublish` and `tokenRequests`, and `getTokenRequests` with the full preservation matrix. Includes `getRequiresRepublish` and `stringValue` helpers.
- **Dependencies:** Phase 1 (API types available). Can proceed in parallel with Phase 2.
- **Target files:** `pkg/operator/csidriver_asset.go`, `pkg/operator/csidriver_asset_test.go`
- **Required capabilities:** OperatorController, Testing
- **Verification hooks:** `make test-unit` — table-driven tests covering requiresRepublish mapping, tokenRequests preservation at every nil level, Managed with audiences, Managed with explicit empty list, full AssetFunc round-trip with base manifest preservation.

### Phase 4: Controller Wiring in starter.go

- **Goal:** Wire the dynamic CSIDriver controller and rotation hook into the CSIControllerSet chain. Split `csidriver.yaml` into its own `ConditionalStaticResourcesController` instance. Pass `clusterCSIDriverInformer` as optional informer to `WithCSIDriverNodeService` for immediate DaemonSet re-sync on config changes.
- **Dependencies:** Phase 2 and Phase 3 (hook functions and AssetFunc available).
- **Target files:** `pkg/operator/starter.go`
- **Required capabilities:** OperatorController
- **Verification hooks:** `make check` (verify + test-unit); manual review of controller chain for Constitution Principle I, IV, VIII compliance.

### Phase 5: E2E Test Development and Verification

- **Goal:** Develop E2E tests covering rotation configuration scenarios (default, Custom, None, toggle), tokenRequests scenarios (Unmanaged, Managed, empty, multi-audience), and upgrade scenarios (no driverConfig, pre-existing tokenRequests preservation).
- **Dependencies:** Phase 4 (fully wired operator). Live OpenShift cluster.
- **Target files:** `hack/e2e.sh` or E2E test files (UNVERIFIED — E2E test structure for this feature not confirmed on branch; discovery step needed)
- **Required capabilities:** Testing
- **Verification hooks:** `make test-e2e` (requires cluster); CI Prow jobs.

### Phase 6: OLM and Release Integration

- **Goal:** Verify CSV RBAC alignment, image references, and OLM bundle consistency. Ensure no new RBAC is needed for the dynamic CSIDriver controller. Run `hack/update-metadata.sh` if OCP version bump is required.
- **Dependencies:** Phase 4 (code stable).
- **Target files:** `config/manifests/stable/*.clusterserviceversion.yaml`, `config/manifests/stable/image-references` (UNVERIFIED — not read in repo-assessment)
- **Required capabilities:** OLMRelease
- **Verification hooks:** Manual CSV inspection; `make metadata` if version bump needed; bundle validation.

## 6. Verification matrix (maps to spec acceptance)

| Category | Coverage | Files / Suites |
|----------|----------|----------------|
| Unit | Rotation config extraction (6 cases), DaemonSet hook (4 cases + missing container + coexistence + baseline regression), setArg (4 cases), getRequiresRepublish (3 cases), getTokenRequests (8 cases), dynamic AssetFunc (4 cases) | `pkg/operator/rotation_test.go`, `pkg/operator/csidriver_asset_test.go`, `pkg/operator/starter_test.go` |
| Integration | N/A — no integration test tier in this repo | — |
| E2E | Rotation defaults, Custom interval, None (disabled), toggle, tokenRequests Unmanaged/Managed/empty/multi-audience, upgrade scenarios (no driverConfig, pre-existing tokenRequests preservation) | `hack/e2e.sh` + CI Prow jobs (UNVERIFIED — E2E implementation status not confirmed) |
| Manual / Cluster | Verify CSIDriver spec: `oc get csidriver secrets-store.csi.k8s.io -o yaml`; Verify DaemonSet args: `oc get ds -n openshift-cluster-csi-drivers secrets-store-csi-driver-node -o jsonpath='{.spec.template.spec.containers[?(@.name=="csi-driver")].args}'` | — |

### Spec FR → Phase → Verification mapping

| FR | Phase | Verification |
|----|-------|-------------|
| FR-001 (disable rotation) | Phase 2 | Unit: `TestGetSecretRotationConfig` "type None"; E2E: rotation None scenario |
| FR-002 (custom interval) | Phase 2 | Unit: `TestGetSecretRotationConfig` "type Custom"; E2E: Custom interval scenario |
| FR-003 (default behavior) | Phase 2 | Unit: `TestDefaultPathMatchesPreFeatureBaseline`; E2E: no driverConfig scenario |
| FR-006 (token audiences) | Phase 3 | Unit: `TestGetTokenRequests` "Managed with audiences"; E2E: Managed scenario |
| FR-010 (Managed/Unmanaged) | Phase 3 | Unit: `TestGetTokenRequests` preservation cases; E2E: Unmanaged → Managed transition |
| FR-011 (preserve existing) | Phase 3 | Unit: `TestGetTokenRequests` all nil-path cases; E2E: upgrade with pre-existing tokenRequests |
| FR-014 (dynamic DaemonSet) | Phase 4 | Unit: `TestWithSecretRotationDaemonSetHook`; E2E: config change → rolling update |
| FR-015 (dynamic CSIDriver) | Phase 4 | Unit: `TestNewDynamicCSIDriverAssetFunc`; E2E: config change → CSIDriver recreate |
| FR-016 (no change on upgrade) | Phase 2+3 | Unit: `TestDefaultPathMatchesPreFeatureBaseline`, `TestNewDynamicCSIDriverAssetFunc` "no driverConfig" |
| FR-017 (requiresRepublish lifecycle) | Phase 3 | Unit: `TestGetRequiresRepublish`; E2E: rotation None → requiresRepublish false |
| FR-018 (discriminated unions) | Phase 1 | CEL rules in openshift/api CRD schema (not operator code) |

## 7. Risks, migrations, and operational follow-ups

### Upgrade/migration

- **No behavior change on upgrade:** Clusters upgrading with no `driverConfig` set see identical behavior. `TestDefaultPathMatchesPreFeatureBaseline` is a dedicated regression test ensuring the hook produces byte-for-byte identical DaemonSet args.
- **requiresRepublish nil→true:** On upgrade, the operator will set `requiresRepublish: true` on the CSIDriver (previously nil). This introduces net-new kubelet `NodePublishVolume` calls but is functionally safe because the driver already handled rotation via DaemonSet args. The spec-hash change triggers a one-time CSIDriver recreate.
- **tokenRequests preservation:** Multi-level nil-check in `getTokenRequests` ensures existing manually-patched tokenRequests are preserved at every fallback path. 8-case unit test matrix validates this.

### Compatibility

- **Hypershift / Hosted Control Planes:** N/A per spec (A-005).
- **MicroShift:** Not applicable — operator is not part of MicroShift.
- **OpenShift Kubernetes Engine:** N/A per spec.

### Upstream API drift risks

- **openshift/api types:** If `SecretsStoreDriverType` or related types are renamed/removed in a future openshift/api release, the vendor update will fail to compile — caught by CI.
- **library-go ApplyCSIDriver:** If the hash-based recreate behavior changes, the dynamic CSIDriver controller may not properly detect spec changes. Low risk — this is a stable library-go pattern used by many operators.

### Operational

- **CSIDriver absence window:** During spec-hash-triggered delete+recreate, the CSIDriver object briefly does not exist. Running pods are unaffected (already have volumes mounted). New pod mounts during this window will fail and retry.
- **Rate-limit risk:** Setting `minimumRefreshAge` to 1 second is allowed but the effective floor is kubelet's syncFrequency (~1 minute). Documentation should advise sensible values.

## 8. Open questions / SME decisions

None — all decisions resolved in this plan. The feature is already implemented on the pinned branch, and the architectural approach follows Constitution principles I–X without conflicts. The remaining work is verification (E2E tests, OLM CSV alignment) which does not require SME decisions.

Validation non-blockers (from validation.json) have been addressed:
- Repository inventory: documented in repo-assessment §0 and A-009.
- requiresRepublish upgrade behavior: documented in §7 Upgrade/migration.
- Rate-limit mitigation: documented in §7 Operational.
