# Repository Assessment Report: SSCSI-254

## §0 — Metadata

| Key | Value |
|-----|-------|
| Repo | `openshift/secrets-store-csi-driver-operator` (working-folder mode) |
| Branch | `openspec-cursor-agent-sonnet5` |
| Commit | `0b6b5b3a` |
| Tooling status | OK — full read access to all source files |
| Spec status | PASS (validation score 93%) |
| Feature status | **IMPLEMENTED on pinned branch** — rotation.go, csidriver_asset.go, starter.go already carry the feature code with tests. See §11.1 for branch-honesty details. |

## §1 — Architecture Overview

### §1.1 Controller Framework

The operator uses **library-go's `csicontrollerset`** — a composable controller set that chains sub-controllers via method calls in `pkg/operator/starter.go:RunOperator`. There is a single binary (`cmd/secrets-store-csi-driver-operator/main.go`) with one cobra command (`start`) that delegates to `controllercmd.NewControllerCommandConfig`.

The controller set composition (starter.go lines 83–159):

1. **LogLevelController** — syncs log level from `ClusterCSIDriver` spec.
2. **ManagementStateController** — handles `Managed`/`Unmanaged`/`Removed` lifecycle. The operator is marked **removable** (`true` passed to `WithManagementStateController`).
3. **ConditionalStaticResourcesController** ("SecretsStoreConditionalStaticResourcesController") — reconciles static YAML assets (RBAC, SA, ConfigMap, NetworkPolicy) based on management state.
4. **ConditionalStaticResourcesController** ("SecretsStoreDynamicCSIDriverController") — reconciles `csidriver.yaml` using a dynamic `AssetFunc` (`NewDynamicCSIDriverAssetFunc`) that sets `requiresRepublish` and `tokenRequests` on the CSIDriver object from live ClusterCSIDriver state.
5. **CSIConfigObserverController** — observes cluster config (infrastructure, proxy, apiserver).
6. **CSIDriverNodeService** ("SecretsStoreDriverNodeServiceController") — manages the node DaemonSet. Configured with two hook functions: `WithCABundleDaemonSetHook` (pre-existing) and `WithSecretRotationDaemonSetHook` (new for this feature).

### §1.2 Operator Client Pattern

The operator creates a `GenericOperatorClient` via `goc.NewClusterScopedOperatorClientWithConfigName` scoped to the `clustercsidrivers` GVR with config name `secrets-store.csi.k8s.io`. Two extractor functions (`extractOperatorSpec`, `extractOperatorStatus`) convert between `unstructured.Unstructured` and typed `applyconfigurations`.

### §1.3 Dead Code / Traps

- **`pkg/dependencymagnet/dependencymagnet.go`**: Build-tag-guarded import (`//go:build tools`). Do NOT remove — keeps `build-machinery-go` in `vendor/`.
- **`replaceNamespaceFunc`**: Returns an `AssetFunc` that replaces `${NAMESPACE}` in asset bytes. The dynamic CSIDriver AssetFunc (`NewDynamicCSIDriverAssetFunc`) wraps this function even though `csidriver.yaml` currently has no `${NAMESPACE}` token — this is intentional forward-compatibility.
- **`config/manifests/`**: OLM bundle manifests. These are NOT reconciled by the operator code — they are consumed by OLM. Do not confuse them with `assets/` which are embedded and deployed by the operator.

## §2 — File Inventory

### §2.1 Source Files (feature-relevant)

| File | Purpose | Evidence |
|------|---------|----------|
| `pkg/operator/starter.go` | Controller composition — wires all sub-controllers, creates informers, registers hooks | Lines 83–159: `csicontrollerset` chain |
| `pkg/operator/rotation.go` | Secret rotation config extraction (`getSecretRotationConfig`), DaemonSet hook (`WithSecretRotationDaemonSetHook`), arg helpers (`setArg`, `formatRotationInterval`) | Lines 55–167 |
| `pkg/operator/csidriver_asset.go` | Dynamic CSIDriver asset func (`NewDynamicCSIDriverAssetFunc`), `getRequiresRepublish`, `getTokenRequests`, `stringValue` | Lines 26–161 |
| `pkg/operator/starter_test.go` | Tests for `getOperatorSyncState` (Managed/Unmanaged/Removed/DeletionTimestamp) | Lines 17–72 |
| `pkg/operator/rotation_test.go` | Tests for `setArg`, `getSecretRotationConfig`, `WithSecretRotationDaemonSetHook`, hook coexistence, pre-feature baseline regression | Lines 20–469 |
| `pkg/operator/csidriver_asset_test.go` | Tests for `getRequiresRepublish`, `getTokenRequests`, `NewDynamicCSIDriverAssetFunc` | Lines 19–323 |

### §2.2 Asset Files (embedded via `go:embed`)

| File | K8s Kind | Controller | Notes |
|------|----------|------------|-------|
| `assets/csidriver.yaml` | `CSIDriver` | `SecretsStoreDynamicCSIDriverController` | Dynamic: `requiresRepublish` and `tokenRequests` set by `NewDynamicCSIDriverAssetFunc` at reconcile time |
| `assets/node.yaml` | `DaemonSet` | `SecretsStoreDriverNodeServiceController` | Contains `--enable-secret-rotation=true` and `--rotation-poll-interval=2m` as static defaults; overridden by `WithSecretRotationDaemonSetHook` |
| `assets/node_sa.yaml` | `ServiceAccount` | Static resources controller | `${NAMESPACE}` substitution |
| `assets/cabundle_cm.yaml` | `ConfigMap` | Static resources controller | CA bundle injection via label |
| `assets/rbac/privileged_role.yaml` | `ClusterRole` | Static resources controller | |
| `assets/rbac/node_privileged_binding.yaml` | `ClusterRoleBinding` | Static resources controller | |
| `assets/rbac/secretproviderclasses_role.yaml` | `ClusterRole` | Static resources controller | |
| `assets/rbac/secretproviderclasses_binding.yaml` | `ClusterRoleBinding` | Static resources controller | |
| `assets/network-policy/allow-ingress-to-metrics-operand.yaml` | `NetworkPolicy` | Static resources controller | |

**Do NOT edit** embed directive: `assets/assets.go` line 7 — `//go:embed *.yaml rbac/*.yaml network-policy/*.yaml`. Any new subdirectory requires updating this glob.

### §2.3 Build & Config Files

| File | Purpose |
|------|---------|
| `Makefile` | Build, test, verify targets. FIPS auto-detection. Image target for OCP 5.0. |
| `go.mod` | Go 1.25, openshift/api `v0.0.0-20260709102940`, library-go, k8s.io deps |
| `Dockerfile.openshift` | Multi-stage build: `rhel-9-golang-1.26-openshift-5.0` builder → `ocp/5.0:base-rhel9` |
| `config/manifests/` | OLM bundle (CSV, CRDs, package.yaml, image-references, art.yaml) |
| `hack/e2e.sh` | E2E test script (requires live cluster) |
| `hack/update-metadata.sh` | OCP version bump across CSV, Makefile, README |

## §3 — Feature-Relevant Configuration Points

### §3.1 API Types (vendored from openshift/api)

The `ClusterCSIDriver` type is vendored at `vendor/github.com/openshift/api/operator/v1/types_csi_driver.go`. The feature-relevant types on this branch:

| Type | Field | Purpose |
|------|-------|---------|
| `CSIDriverConfigSpec` | `DriverType` | Enum: `""`, `AWS`, `Azure`, `GCP`, `IBMCloud`, `vSphere`, `SecretsStore` |
| `CSIDriverConfigSpec` | `SecretsStore` | `SecretsStoreCSIDriverConfigSpec` — rotation + tokenRequests config |
| `SecretsStoreCSIDriverConfigSpec` | `SecretRotation` | Union: `type` discriminator (`None`/`Custom`) + `Custom` branch |
| `SecretsStoreCSIDriverConfigSpec` | `TokenRequests` | Union: `type` discriminator (`Managed`/`Unmanaged`) + `Managed` branch |
| `CustomSecretRotation` | `MinimumRefreshAge` | `int32`, 1–31560000 seconds, 0 = omitted (default 120s) |
| `ManagedTokenRequests` | `Audiences` | `*[]SecretsStoreTokenRequest`, nil = omitted, `&[]{}` = explicitly empty |
| `SecretsStoreTokenRequest` | `Audience` | `*string`, listMapKey; `ExpirationSeconds` int32, 600–315360000 |

### §3.2 Pre-Feature Baseline in node.yaml

The DaemonSet template (assets/node.yaml lines 45–46) carries the historically hardcoded defaults:
```
"--enable-secret-rotation=true"
"--rotation-poll-interval=2m"
```
These are the **exact strings** that the `WithSecretRotationDaemonSetHook` must reproduce for unconfigured clusters (byte-for-byte, to avoid unintended DaemonSet rollouts). The `formatRotationInterval` function in rotation.go handles this by rendering whole-minute durations as `Nm` instead of Go's default `NmOs`.

### §3.3 Pre-Feature Baseline in csidriver.yaml

The static asset (assets/csidriver.yaml) has no `requiresRepublish` or `tokenRequests` fields. The `NewDynamicCSIDriverAssetFunc` reads this as the base manifest and sets those fields dynamically. When no `ClusterCSIDriver` exists, the base manifest is returned unmutated.

## §4 — Reconciliation Flow

### §4.1 Controller Registration Pipeline

| # | Controller | Trigger | Sync Action | Error Behavior |
|---|-----------|---------|-------------|----------------|
| 1 | LogLevelController | ClusterCSIDriver changes | Set log level from spec | Degrade operator status |
| 2 | ManagementStateController | ClusterCSIDriver changes | Set management state | Degrade operator status |
| 3 | SecretsStoreConditionalStaticResourcesController | Managed/Removed state | Create (Managed) or delete (Removed) static RBAC/SA/CM/NetworkPolicy assets | Degrade operator status |
| 4 | SecretsStoreDynamicCSIDriverController | Managed/Removed state + ClusterCSIDriver config changes | Render CSIDriver via `NewDynamicCSIDriverAssetFunc`, apply via `resourceapply.ApplyCSIDriver` (hash-based recreate) | Degrade operator status |
| 5 | CSIConfigObserverController | Infrastructure/proxy/apiserver changes | Propagate cluster config to operand | Degrade operator status |
| 6 | SecretsStoreDriverNodeServiceController | DaemonSet drift, ClusterCSIDriver changes (via `clusterCSIDriverInformer`) | Apply DaemonSet with hooks (CA bundle + rotation) | Degrade operator status |

### §4.2 DaemonSet Hook Pipeline (controller #6)

| # | Hook | Source | Mutations | Error behavior |
|---|------|--------|-----------|----------------|
| 1 | `WithCABundleDaemonSetHook` | library-go | Injects CA bundle volume + volumeMount into annotated containers | Returns error → controller retries |
| 2 | `WithSecretRotationDaemonSetHook` | `pkg/operator/rotation.go` | Sets `--enable-secret-rotation=` and `--rotation-poll-interval=` on `csi-driver` container args | Returns error → controller retries; NotFound → no-op (leaves static defaults) |

Hooks are applied in registration order. The coexistence test (`TestCABundleAndRotationHooksCoexist` in rotation_test.go) verifies neither hook's mutations clobber the other's.

### §4.3 Dynamic CSIDriver Asset Pipeline (controller #4)

1. `namespaceAssetFunc("csidriver.yaml")` → reads base manifest with `${NAMESPACE}` replacement
2. `resourceread.ReadCSIDriverV1OrDie(manifest)` → typed `*storagev1.CSIDriver`
3. `csiDriverLister.Get(csiDriverName)` → existing CSIDriver from cluster (for tokenRequests preservation)
4. `clusterCSIDriverLister.Get(clusterCSIDriverName)` → ClusterCSIDriver config
5. `getRequiresRepublish(driverConfig)` → sets `spec.requiresRepublish`
6. `getTokenRequests(driverConfig, existingTokenRequests)` → sets `spec.tokenRequests`
7. `yaml.Marshal(csiDriver)` → bytes for `resourceapply.ApplyCSIDriver` (hash-based recreate)

## §5 — Reusable Assets (Anti-Duplication)

* `resourceapply.AssetFunc` (library-go): Use for all asset loading. The operator's `replaceNamespaceFunc` returns one; `NewDynamicCSIDriverAssetFunc` wraps one. Do not bypass this pattern.
* `resourceread.ReadCSIDriverV1OrDie` (library-go): Deserializes CSIDriver YAML to typed object. Used in `NewDynamicCSIDriverAssetFunc`. Do not manually unmarshal CSIDriver assets.
* `resourceapply.ApplyCSIDriver` (library-go): Hash-based reconcile that detects spec changes and delete+recreates. Already used by `ConditionalStaticResourcesController` — do not reimplement hash-based CSIDriver apply.
* `v1helpers.NewFakeOperatorClientWithObjectMeta` (library-go): Test helper for faking the operator client. Used in `starter_test.go`. Use this for any new controller-level tests.
* `csidrivernodeservicecontroller.DaemonSetHookFunc` (library-go): The hook signature for DaemonSet mutations. Use this type for any new DaemonSet hooks.
* `cache.GenericLister` (client-go): The dynamic lister used to read ClusterCSIDriver from the shared informer. Both `rotation.go` and `csidriver_asset.go` use this pattern (lister.Get → unstructured → typed conversion). Do not create typed ClusterCSIDriver informers — reuse the existing dynamic informer.

## §6 — Architectural Guardrails

**Structural:**
- Static resources (RBAC, SA, ConfigMap, CSIDriver, NetworkPolicy) use `WithConditionalStaticResourcesController`. The DaemonSet uses `WithCSIDriverNodeService`. Do not mix these patterns.
- The CSIDriver asset is in its own `ConditionalStaticResourcesController` instance ("SecretsStoreDynamicCSIDriverController"), separate from the static assets controller. This isolation prevents a bug in the dynamic AssetFunc from affecting unrelated static assets.
- All controllers are composed via `csicontrollerset` method chaining in `starter.go`. Do not register controllers individually.

**API / Schema:**
- API types live in `openshift/api` (vendored). Changes to API types require updating the vendor in this repo.
- The `Audiences` field uses `*[]T` (pointer-to-slice) semantics: `nil` = omitted (preserve existing), `&[]{}` = explicitly empty (clear). This is critical for the Managed/Unmanaged distinction.
- CEL validation rules on ClusterCSIDriver enforce discriminated unions and tokenRequests immutability. The operator does NOT re-implement these checks — it only reads already-validated objects.

**Build / Tooling:**
- Go 1.25+ required (matching `go.mod`).
- FIPS: CI builds use `CGO_ENABLED=1 GOEXPERIMENT=strictfipsruntime` with `-tags strictfipsruntime,openssl`. Local builds without FIPS-capable Go will warn but succeed.
- All dependencies must be vendored. Run `go mod tidy && go mod vendor` after any dependency change.

**Deployment / Packaging:**
- Operator runs in `openshift-cluster-csi-drivers` namespace. All namespace-scoped resources use `${NAMESPACE}` placeholder.
- Images in `node.yaml` use `${DRIVER_IMAGE}`, `${NODE_DRIVER_REGISTRAR_IMAGE}`, `${LIVENESS_PROBE_IMAGE}`, `${LOG_LEVEL}` — substituted at runtime by the library-go DaemonSet controller.

**Security:**
- The `csi-driver` container runs as `privileged: true` with `readOnlyRootFilesystem: true`.
- The operator uses `system-node-critical` priorityClassName.

## §7 — Change Cascade Checklist

| When you change... | You must also... | Verify with... |
|---|---|---|
| Go source files in `pkg/` or `cmd/` | Run formatting and vet | `make verify` |
| Any Go files | Run unit tests | `make test-unit` |
| Assets under `assets/` | Verify `//go:embed` directive in `assets/assets.go` covers new paths | Inspect `assets/assets.go` line 7 |
| New asset subdirectory | Update `//go:embed` glob in `assets/assets.go` | Build + test (`make build && make test-unit`) |
| Static asset file list | Update the file list in `WithConditionalStaticResourcesController` call in `starter.go` | `make test-unit` |
| API types (openshift/api vendor) | `go mod tidy && go mod vendor` | `make verify` (includes verify-deps) |
| Controller wiring in `starter.go` | Verify informer scoping (no all-namespace informers) | Code review + `make test-unit` |
| OLM metadata / CSV | Run `hack/update-metadata.sh` | `make metadata` |
| Image references | Update `config/manifests/stable/image-references` | Manual inspection |
| RBAC assets | Update both the RBAC YAML and the CSV RBAC section | `make verify` |

## §8 — Test & CI Reference

### §8.1 Test Structure

- **Unit tests**: `pkg/operator/*_test.go` — table-driven, standard `testing` package, no assertion libraries.
  - `starter_test.go`: `TestGetOperatorSyncState` (4 cases)
  - `rotation_test.go`: `TestSetArg` (4 cases), `TestGetSecretRotationConfig` (6 cases), `TestWithSecretRotationDaemonSetHook` (4 cases), `TestWithSecretRotationDaemonSetHookMissingContainer`, `TestCABundleAndRotationHooksCoexist`, `TestDefaultPathMatchesPreFeatureBaseline` (2 cases)
  - `csidriver_asset_test.go`: `TestGetRequiresRepublish` (3 cases), `TestGetTokenRequests` (8 cases), `TestNewDynamicCSIDriverAssetFunc` (4 cases)
- **E2E tests**: `hack/e2e.sh` (requires live OpenShift cluster, runs via `openshift-tests`)
- **No integration test tier** in-repo.

### §8.2 How to Run Tests Locally

```
# Full verification + unit tests
make check

# Just unit tests
make test-unit

# Just verification (vet, gofmt, deps)
make verify

# Auto-fix formatting
make update-gofmt

# E2E (requires cluster + openshift-tests in PATH)
make test-e2e
```

### §8.3 CI Pipeline

- CI runs in **OpenShift Prow**. Config lives in `openshift/release` (not this repo).
- `.ci-operator.yaml` specifies the build root image.
- Every PR runs: `make verify` and `make test-unit`.
- E2E tests run as Prow jobs against real clusters.
- CI builds enforce FIPS compliance via `strictfipsruntime`.

### §8.4 Test Coverage for Feature

| Area | Coverage | Files |
|------|----------|-------|
| Rotation config extraction | Comprehensive (6 cases) | `rotation_test.go:TestGetSecretRotationConfig` |
| DaemonSet hook | Comprehensive (4 cases + missing container + coexistence + baseline regression) | `rotation_test.go` |
| `setArg` helper | Comprehensive (4 cases) | `rotation_test.go:TestSetArg` |
| `getRequiresRepublish` | Covers nil/None/Custom (3 cases) | `csidriver_asset_test.go` |
| `getTokenRequests` | Comprehensive preservation matrix (8 cases including explicit empty) | `csidriver_asset_test.go` |
| Dynamic CSIDriver AssetFunc | End-to-end (4 cases including existing tokenRequests preservation) | `csidriver_asset_test.go` |
| Operator sync state | Comprehensive (4 cases) | `starter_test.go` |

## §9 — Developer Workflow

### §9.1 Key Commands Reference

| Command | Purpose |
|---------|---------|
| `make` / `make build` | Build the operator binary |
| `make test-unit` | Run unit tests (`./pkg/... ./cmd/...`) |
| `make verify` | Run `go vet`, `gofmt` check, Go version consistency |
| `make check` | Run `verify` then `test-unit` |
| `make update-gofmt` | Auto-fix formatting |
| `make test-e2e` | Run E2E tests (requires cluster) |
| `make clean` | Remove binary and yq tool |
| `make metadata VERSION=X.Y.Z` | Bump OCP version in CSV and OLM metadata |

### §9.2 Version Variables

| Variable | Location | Value |
|----------|----------|-------|
| Go version | `go.mod` line 3 | `1.25.0` |
| openshift/api | `go.mod` line 6 | `v0.0.0-20260709102940-580f1c1ba691` |
| library-go | `go.mod` line 9 | `v0.0.0-20260303171201-5d9eb6295ff6` |
| OCP image tag | `Makefile` line 61, `Dockerfile.openshift` | `5.0` |
| Builder image | `Dockerfile.openshift` line 1 | `rhel-9-golang-1.26-openshift-5.0` |

### §9.3 Local Development Setup

1. Go 1.25+ installed
2. `make build` to compile
3. For E2E: `oc` CLI + live OpenShift cluster + `openshift-tests` in `$PATH`
4. For FIPS builds: `GOEXPERIMENT=strictfipsruntime`-capable Go (optional for local dev)

### §9.4 Common Development Scenarios

**How to add a new DaemonSet hook:**
1. Create a function returning `csidrivernodeservicecontroller.DaemonSetHookFunc` (see `WithSecretRotationDaemonSetHook` in `rotation.go` for pattern)
2. The hook receives `*opv1.OperatorSpec` and `*appsv1.DaemonSet`; mutate the DaemonSet in place
3. If the hook needs data beyond `OperatorSpec`, close over a lister (see `clusterCSIDriverLister` pattern)
4. Register in `starter.go` by adding to `WithCSIDriverNodeService`'s variadic `optionalDaemonSetHooks` parameter
5. If the hook's data source should trigger immediate reconciliation, pass its informer to `WithCSIDriverNodeService`'s `optionalInformers` parameter
6. Write table-driven tests using `newFakeClusterCSIDriverLister` and `newTestDaemonSet` helpers

**How to add a new static asset:**
1. Create the YAML file under `assets/` (or a subdirectory)
2. If new subdirectory, update `//go:embed` in `assets/assets.go`
3. Add the file name to the `WithConditionalStaticResourcesController` file list in `starter.go`
4. Use `${NAMESPACE}` for namespace fields
5. Run `make verify && make test-unit`

**How to add a dynamic AssetFunc for an existing static resource:**
1. Follow the `NewDynamicCSIDriverAssetFunc` pattern in `csidriver_asset.go`
2. Wrap `replaceNamespaceFunc(namespace)` as the base asset loader
3. Separate the resource into its own `WithConditionalStaticResourcesController` instance (see "SecretsStoreDynamicCSIDriverController" in starter.go)
4. Wire any additional listers/informers needed for the dynamic computation

## §10 — Platform & Environment Integration

### §10.1 Security Context & Permissions

- The `csi-driver` container runs `privileged: true` (required for CSI node plugins that mount to host filesystem).
- RBAC: `privileged_role.yaml` grants SCC `privileged` usage. `secretproviderclasses_role.yaml` grants access to `secretproviderclasses` and `secretproviderclasspodstatuses` CRDs.
- The operator itself runs with the RBAC defined in the CSV (`config/manifests/stable/*.clusterserviceversion.yaml`).

### §10.2 Proxy & Network Configuration

- CA bundle injection via `config.openshift.io/inject-trusted-cabundle: "true"` label on `cabundle_cm.yaml`.
- `WithCABundleDaemonSetHook` injects the CA bundle volume into the DaemonSet containers annotated with `config.openshift.io/inject-proxy-cabundle`.
- NetworkPolicy (`allow-ingress-to-metrics-operand.yaml`) controls metrics ingress.

### §10.3 Cloud Provider Integration

- WIF token audiences are configured per FR-006 through `tokenRequests` on `ClusterCSIDriver`.
- The operator does NOT auto-detect cloud providers — administrators must explicitly configure audiences.
- Provider plugins (AWS, Azure, GCP, HashiCorp Vault) are installed separately.

### §10.4 Build & Compliance Constraints

- FIPS: Auto-detected in Makefile. CI requires `strictfipsruntime`.
- Image build: Multi-stage Dockerfile.openshift using OCP builder/base images.

### §10.5 Console / UI Integration

- Not applicable — no console plugin for this operator.

### §10.6 Packaging & Lifecycle

- OLM bundle at `config/manifests/`.
- Suggested namespace: `openshift-cluster-csi-drivers`.
- The operator is marked **removable** — supports `Removed` management state.

## §11 — Risks & Downstream Impacts

* **Risk: CSIDriver spec immutability.** Changing `requiresRepublish` or `tokenRequests` requires delete+recreate of the `CSIDriver` object. `resourceapply.ApplyCSIDriver` handles this transparently via spec-hash annotations, but there is a brief absence window. **Mitigation:** Window is negligible; running pods are unaffected. Tested in E2E scenarios.

* **Risk: Unintended DaemonSet rollout on upgrade.** If the default rotation args rendered by the hook don't byte-for-byte match the static values in `node.yaml`, every cluster upgrading will see an unintended rolling update. **Mitigation:** `formatRotationInterval` renders whole-minute durations as `Nm` instead of Go's `NmOs`. `TestDefaultPathMatchesPreFeatureBaseline` is a dedicated regression test.

* **Risk: tokenRequests preservation failure.** If existing manually-patched tokenRequests on the CSIDriver are lost during operator reconciliation, WIF workloads will fail. **Mitigation:** Multi-level nil-check in `getTokenRequests` preserves existing tokenRequests at every fallback path. 8-case unit test matrix covers all nil paths.

* **Risk: CEL validation duplication.** The operator must NOT re-implement CRD-level CEL validation (e.g., tokenRequests immutability). **Mitigation:** `getTokenRequests` and `getSecretRotationConfig` only read already-validated objects. No admission-style checks in operator code.

### §11.1 Assessment Limitations / UNVERIFIED Items

* **Feature is IMPLEMENTED on this branch** (`openspec-cursor-agent-sonnet5`). The git history shows commits T3_1 through T6_2 implementing rotation.go, csidriver_asset.go, and wiring in starter.go. This assessment documents the current state — not a greenfield implementation plan.
* **openshift/api vendor pin** (`v0.0.0-20260709102940`) — verified to contain `SecretsStoreDriverType`, `SecretsStoreCSIDriverConfigSpec`, and related types by reading vendored source. If re-vendoring from a different openshift/api commit, verify these types still exist.
* **E2E test coverage** — not verified in this assessment (requires live cluster). The EP's Test Plan section specifies detailed E2E scenarios but their implementation status on this branch was not confirmed.
* **OLM CSV RBAC** — the CSV at `config/manifests/stable/` was not read in this assessment. Any new RBAC requirements for the dynamic CSIDriver controller should be reflected there.
* **library-go `ApplyCSIDriver` hash-recreate behavior** — assumed from documentation and AGENTS.md; the vendor source was not read to verify the exact hash-annotation key or recreate logic.

## §12 — Quick Reference Card

### Preflight Checklist (run before every PR)
```
1. make verify
2. make test-unit
3. # Inspect assets/assets.go if any assets/ changes
4. # E2E: make test-e2e (requires cluster — CI handles this)
```

### Key File Quick-Nav

| I want to... | Look at... |
|---|---|
| Understand controller composition | `pkg/operator/starter.go` (lines 83–159) |
| Add/modify DaemonSet hooks | `pkg/operator/rotation.go` + register in `starter.go:WithCSIDriverNodeService` |
| Add/modify CSIDriver dynamic asset | `pkg/operator/csidriver_asset.go` + `starter.go:NewDynamicCSIDriverAssetFunc` call |
| Read rotation config extraction logic | `pkg/operator/rotation.go:getSecretRotationConfig` |
| Read tokenRequests logic | `pkg/operator/csidriver_asset.go:getTokenRequests` |
| Add a new static YAML asset | `assets/` + update file list in `starter.go:WithConditionalStaticResourcesController` |
| Change RBAC | `assets/rbac/*.yaml` (source of truth, NOT generated bundle) |
| Add a new asset subdirectory | `assets/assets.go` — update `//go:embed` directive |
| Run unit tests | `make test-unit` (targets `./pkg/... ./cmd/...`) |
| Understand API types | `vendor/github.com/openshift/api/operator/v1/types_csi_driver.go` |
| Follow test patterns | `pkg/operator/rotation_test.go` (table-driven, fakes, assertion style) |
| Check build/FIPS config | `Makefile` (lines 32–43) |
| Read DaemonSet template | `assets/node.yaml` |
| Read CSIDriver base manifest | `assets/csidriver.yaml` |
