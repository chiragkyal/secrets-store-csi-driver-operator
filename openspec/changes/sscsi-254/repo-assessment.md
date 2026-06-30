# Repository Assessment: secrets-store-csi-driver-operator

**Feature**: SSCSI-254 — Configurable Secret Rotation and Workload Identity Federation  
**Repository**: github.com/openshift/secrets-store-csi-driver-operator  
**Assessment mode**: Working-folder (local checkout)  
**Branch**: main  
**Assessed**: 2026-06-30

---

## §0 Inputs & Tooling

### 0.1 Assessed Files
All assertions in this report are grounded in the following files read directly from the working checkout:

| File | Purpose |
|------|---------|
| `pkg/operator/starter.go` | Complete operator reconciliation logic |
| `pkg/operator/starter_test.go` | Unit test patterns and fake client usage |
| `assets/assets.go` | Go embed declaration |
| `assets/node.yaml` | DaemonSet manifest (3-container CSI node plugin) |
| `assets/csidriver.yaml` | CSIDriver manifest (currently static) |
| `assets/cabundle_cm.yaml` | Trusted CA ConfigMap |
| `assets/node_sa.yaml` | ServiceAccount |
| `assets/rbac/*.yaml` | 4 RBAC assets |
| `assets/network-policy/*.yaml` | NetworkPolicy for metrics port |
| `Makefile` | Build targets, FIPS flags, test commands |
| `go.mod` | Module path and dependency versions |
| `README.md` | Local run and OLM instructions |
| `hack/update-metadata.sh` | OCP version bump script |

### 0.2 Tooling Status: READY
- Go 1.25, `CGO_ENABLED=1` (FIPS build), `make check` available
- No code generation pipeline in this repo (no custom CRDs, no `make generate`)

---

## §1 Architecture

### 1.1 Operator Model

This is a **pure library-go CSI operator**. There is no controller-runtime, no `ctrl.Manager`, no kubebuilder scaffolding, and no custom CRD. All reconciliation is performed by library-go's `CSIControllerSet`, chained in a single file (`pkg/operator/starter.go`).

The operator manages the singleton `ClusterCSIDriver` named `secrets-store.csi.k8s.io` — a generic upstream API from `operator.openshift.io/v1` (defined in `openshift/api`, not this repo).

**For SSCSI-254**: Both new behaviors (dynamic CSIDriver manifest, DaemonSet args injection) are implemented as additional hooks wired into the existing `CSIControllerSet` chain in `starter.go`. No new files, no new controllers, no new manager.

### 1.2 CSIControllerSet Chain (from `starter.go`)

The chain is built in `RunOperator()` and cannot be modified at runtime. Each controller adds to the chain via a fluent builder pattern:

```
csicontrollerset.NewCSIControllerSet(operatorClient, eventRecorder)
  .WithLogLevelController()
  .WithManagementStateController("secrets-store-csi-driver", removable=true)
  .WithConditionalStaticResourcesController("SecretsStoreConditionalStaticResourcesController",
      kubeClient, dynamicClient, kubeInformersForNamespaces,
      replaceNamespaceFunc(operatorNamespace),           ← AssetFunc: ${NAMESPACE} substitution
      []string{"node_sa.yaml", "csidriver.yaml", ...},  ← 8 static assets
      managedFunc, removedFunc)
  .WithCSIConfigObserverController("SecretsStoreDriverCSIConfigObserverController", configInformers)
  .WithCSIDriverNodeService("SecretsStoreDriverNodeServiceController",
      replaceNamespaceFunc(operatorNamespace), "node.yaml",
      kubeClient, kubeInformersForNamespaces,
      nil,                                              ← no extra informers today
      csidrivernodeservicecontroller.WithCABundleDaemonSetHook(...))
```

**For SSCSI-254:**
- The `AssetFunc` passed to `WithConditionalStaticResourcesController` currently returns the raw `csidriver.yaml` bytes after `${NAMESPACE}` substitution. This must be replaced with a **dynamic AssetFunc** that also sets `spec.requiresRepublish` and `spec.tokenRequests` from the `ClusterCSIDriver` configuration.
- The `nil` extra informers argument to `WithCSIDriverNodeService` must be replaced with the `dynamicInformers` (which watches `ClusterCSIDriver`), so DaemonSet reconciliation triggers immediately on `ClusterCSIDriver` changes.
- A `DaemonSetHookFunc` must be added to `WithCSIDriverNodeService` to inject `--enable-secret-rotation` and `--rotation-poll-interval` args into the `csi-driver` container.

### 1.3 Dead Code / Do Not Edit

| What | Why |
|------|-----|
| `extractOperatorSpec` / `extractOperatorStatus` | Library-go adapter functions — do not add logic here; these are pure converters |
| `replaceNamespaceFunc` | Does one thing: `${NAMESPACE}` replacement — do not add CSIDriver enrichment here |
| `getOperatorSyncState` | Controls Managed/Unmanaged/Removed — do not add feature logic here |

There is **no** `certmanager_controller.go` RBAC placeholder or equivalent dead RBAC code in this repo.

---

## §2 Repository Structure

```
pkg/operator/
  starter.go          ← ALL reconciliation wiring — the only file to edit for SSCSI-254 operator logic
  starter_test.go     ← Unit tests — extend here for new functions
pkg/version/
  version.go          ← Version info only — do not edit
pkg/dependencymagnet/
  dependencymagnet.go ← Blank-import dep pin — do not edit
cmd/secrets-store-csi-driver-operator/
  main.go             ← Entrypoint only — do not edit
assets/
  assets.go           ← //go:embed declaration — do not edit (unless adding new embed glob)
  node.yaml           ← DaemonSet — edit to change container args baseline or add containers
  csidriver.yaml      ← CSIDriver manifest — currently static; SSCSI-254 makes this dynamic
  cabundle_cm.yaml    ← CNO-injected CA bundle ConfigMap — do not edit
  node_sa.yaml        ← ServiceAccount — do not edit
  rbac/               ← 4 RBAC assets — edit only for RBAC surface changes
  network-policy/     ← NetworkPolicy for metrics port 8095 — do not edit
config/
  manifests/          ← OLM CSV and package manifest — edit only for OLM/CSV changes
  metadata/           ← OLM bundle metadata — edit only for OLM changes
hack/
  update-metadata.sh  ← OCP version bump — run with make metadata VERSION=x.y
  create-bundle       ← OLM bundle image builder — do not edit without OLM context
  e2e.sh              ← E2E runner (requires live cluster)
```

**Important**: The `assets/` directory is embedded via `//go:embed *.yaml rbac/*.yaml network-policy/*.yaml` in `assets/assets.go`. New YAML files placed in `assets/` or its covered subdirs are automatically embedded on next build; no changes to `assets.go` are needed unless adding a new subdirectory pattern.

---

## §3 Entry Points & Bootstrap

`main.go` → `controllercmd.NewControllerCommandConfig("secrets-store-csi-driver-operator", version.Get(), operator.RunOperator, clock.RealClock{}).NewCommand()` → `pkg/operator/starter.go:RunOperator()`

`RunOperator()` performs in order:
1. Create `kubeClient` + `kubeInformersForNamespaces` (operator namespace + cluster-wide)
2. Create `configClient` + `configInformers` (cluster Infrastructure, Proxy)
3. Create `operatorClient` via `goc.NewClusterScopedOperatorClientWithConfigName` for `clustercsidrivers` GVR, singleton name `secrets-store.csi.k8s.io`
4. Create `dynamicClient`
5. Build and start `CSIControllerSet` (see §1.2)
6. Start informers and block on `ctx.Done()`

**For SSCSI-254**: Reading `ClusterCSIDriver` configuration from within the new `AssetFunc` and `DaemonSetHookFunc` requires access to the `operatorClient` lister. This is available via `operatorClient.GetOperatorState()` (already used in `getOperatorSyncState`) or by obtaining a typed lister from `dynamicInformers`. The dynamic AssetFunc must not block — it reads the current state on each reconcile call.

---

## §4 Core Reconciliation

### 4.1 ClusterCSIDriver Configuration Surface

The `ClusterCSIDriver` CR (`operator.openshift.io/v1`) is the sole configuration surface for this operator. The relevant existing fields are:

| Field path | Type | Default | Operator behavior |
|-----------|------|---------|------------------|
| `spec.managementState` | `Managed\|Unmanaged\|Removed` | `Managed` | Controls conditional resource apply/delete |
| `spec.logLevel` | enum | `Normal` | Propagated to operator pod log level |
| `spec.operatorLogLevel` | enum | `Normal` | Operator process log verbosity |

**SSCSI-254 will add** (via openshift/api PR #2846):

| Field path | Type | Default (operator built-in) | Behavior |
|-----------|------|---------------------------|---------|
| `spec.driverConfig.driverType` | `SecretsStore` | absent | Enables SecretsStore-specific config |
| `spec.driverConfig.secretsStore.secretRotation.type` | `None\|Custom` | absent (→ rotation enabled) | Controls requiresRepublish + DaemonSet args |
| `spec.driverConfig.secretsStore.secretRotation.custom.minimumRefreshAge` | `int32` (seconds) | absent (→ 120s) | Sets `--rotation-poll-interval` |
| `spec.driverConfig.secretsStore.tokenRequests.type` | `Managed\|Unmanaged` | absent (→ Unmanaged) | Controls CSIDriver.spec.tokenRequests |
| `spec.driverConfig.secretsStore.tokenRequests.managed.audiences` | `[]TokenRequest` | — | When Managed: source of truth for tokenRequests |

**Nil-handling chain** (all levels must be checked): `DriverType != SecretsStore || SecretsStore == nil || SecretRotation == nil` → use built-in defaults. `TokenRequests == nil || type == Unmanaged` → read live CSIDriver.spec.tokenRequests and preserve.

### 4.2 Controller Chain — Reconciliation Detail

| # | Controller | Trigger | What it does | On error |
|---|-----------|---------|-------------|---------|
| 1 | `LogLevelController` | `ClusterCSIDriver.spec.logLevel` change | Sets operator pod log verbosity | Requeue |
| 2 | `ManagementStateController` | `managementState` change | Routes to Managed/Unmanaged/Removed | Requeue |
| 3 | `ConditionalStaticResourcesController` | Any `ClusterCSIDriver` or managed resource change | Apply or delete 8 static assets via `resourceapply.*` | Degraded condition |
| 4 | `CSIConfigObserverController` | Infrastructure/Proxy config change | Propagates proxy env, cluster Infrastructure info | Requeue |
| 5 | `CSIDriverNodeServiceController` | `ClusterCSIDriver` change (after SSCSI-254: via dynamicInformers) | Manages `node.yaml` DaemonSet; applies CA bundle hook + (after SSCSI-254) rotation arg hook | Degraded condition |

**Reconciliation trigger for SSCSI-254**: Today `WithCSIDriverNodeService` receives `nil` for the optional informers argument. After SSCSI-254, `dynamicInformers` must be passed so that `ClusterCSIDriver` changes trigger immediate DaemonSet reconciliation.

### 4.3 Asset Application (ConditionalStaticResourcesController)

The `AssetFunc` is called per-asset on each reconcile. Current implementation:

```go
func replaceNamespaceFunc(namespace string) resourceapply.AssetFunc {
    return func(name string) ([]byte, error) {
        content, err := assets.ReadFile(name)   // Go embed read
        if err != nil { panic(err) }
        return bytes.ReplaceAll(content, []byte("${NAMESPACE}"), []byte(namespace)), nil
    }
}
```

**For SSCSI-254 — dynamic AssetFunc**: The new function must:
1. Call `assets.ReadFile(name)` as before
2. For `name == "csidriver.yaml"` only:
   a. Deserialize bytes into `storagev1.CSIDriver` using `resourceread.ReadCSIDriverV1OrDie`
   b. Read `ClusterCSIDriver` config via `operatorClient.GetOperatorState()`
   c. Set `spec.requiresRepublish` and `spec.tokenRequests` on the typed object
   d. Serialize back to JSON/bytes for `resourceapply.ApplyCSIDriver`
3. For all other assets: apply `${NAMESPACE}` substitution as before (behavior unchanged)

`resourceapply.ApplyCSIDriver` uses spec-hash annotation to detect changes and performs delete+recreate for `CSIDriver` (immutable spec). The hash change is expected and handled transparently.

### 4.4 DaemonSet Args Hook (SSCSI-254)

Library-go `WithCSIDriverNodeService` accepts variadic `DaemonSetHookFunc` arguments. The existing CA bundle hook (`csidrivernodeservicecontroller.WithCABundleDaemonSetHook`) is already present. SSCSI-254 adds a second hook that:

1. Reads `ClusterCSIDriver` config
2. Finds the `csi-driver` container in the DaemonSet pod spec by name
3. Replaces `--enable-secret-rotation=` and `--rotation-poll-interval=` args by prefix match (find-and-replace, not append — baseline args are present in `node.yaml`)
4. Returns error if the `csi-driver` container is not found

**Baseline args in `node.yaml`** (current, hardcoded):
```
--enable-secret-rotation=true
--rotation-poll-interval=2m
```

The hook replaces these with operator-configured values. When `secretRotation` is nil/absent, it replaces them with the same values (no-op update).

### 4.5 tokenRequests Preservation (Unmanaged default)

When `tokenRequests` is absent or type is `Unmanaged`:
1. Read live `CSIDriver.spec.tokenRequests` from the cluster via the dynamic client or lister
2. Include those values in the desired `CSIDriver` spec before computing the hash
3. This prevents spec-hash change and avoids delete+recreate on upgrade

When `tokenRequests.type == Managed`:
1. Use `ClusterCSIDriver.spec.driverConfig.secretsStore.tokenRequests.managed.audiences` as sole source of truth
2. Operator sets `CSIDriver.spec.tokenRequests` from this list (may trigger hash change and delete+recreate)

### 4.6 Image Resolution

Three image env vars are set at operator pod startup and referenced in `node.yaml` as `${DRIVER_IMAGE}`, `${NODE_DRIVER_REGISTRAR_IMAGE}`, `${LIVENESS_PROBE_IMAGE}`. There is no `RELATED_IMAGE_*` env var convention here (unlike cert-manager-operator). The `replaceNamespaceFunc` only replaces `${NAMESPACE}`; image substitution is performed by library-go's `CSIDriverNodeService` controller separately.

### 4.7 Status & Conditions

Status is written to `ClusterCSIDriver.status` by the library-go controllers via the `operatorClient`. The operator sets `Degraded` conditions when controllers fail. No custom condition types are used in this operator — all conditions follow the standard `operator.openshift.io` condition pattern (`Available`, `Progressing`, `Degraded`).

---

## §5 Asset Management

### 5.1 Embedding

```go
// assets/assets.go
//go:embed *.yaml rbac/*.yaml network-policy/*.yaml
var f embed.FS
func ReadFile(name string) ([]byte, error) { return f.ReadFile(name) }
```

**Covered globs**: `assets/*.yaml` (node.yaml, csidriver.yaml, cabundle_cm.yaml, node_sa.yaml), `assets/rbac/*.yaml` (4 files), `assets/network-policy/*.yaml` (1 file). Total: 9 embedded assets. A file placed in `assets/subdir/*.yaml` is NOT embedded unless its glob is added to the `//go:embed` directive.

### 5.2 Asset Inventory

| Asset file | Kind | Namespace-templated | Purpose |
|-----------|------|-------------------|---------|
| `node_sa.yaml` | ServiceAccount | Yes | Node DaemonSet identity |
| `csidriver.yaml` | CSIDriver | No | Driver registration with kubelet |
| `cabundle_cm.yaml` | ConfigMap | Yes | CNO-injected trusted CA bundle |
| `rbac/privileged_role.yaml` | ClusterRole | No | SCC `privileged` use |
| `rbac/node_privileged_binding.yaml` | ClusterRoleBinding | Yes (subject namespace) | Binds privileged_role to node SA |
| `rbac/secretproviderclasses_role.yaml` | ClusterRole | No | Secrets, pods, SA tokens, SecretProviderClass CRDs |
| `rbac/secretproviderclasses_binding.yaml` | ClusterRoleBinding | Yes (subject namespace) | Binds secretproviderclasses_role |
| `network-policy/allow-ingress-to-metrics-operand.yaml` | NetworkPolicy | Yes | Port 8095 ingress for metrics |

**For SSCSI-254**: `csidriver.yaml` remains in the asset embed but becomes the static *base* manifest. The dynamic `AssetFunc` in `starter.go` enriches it at reconcile time with `requiresRepublish` and `tokenRequests`.

### 5.3 When to Reuse

| Symbol | Use for |
|--------|---------|
| `assets.ReadFile(name)` | Reading any embedded asset in `starter.go` hooks |
| `replaceNamespaceFunc(namespace)` | Namespace substitution in the AssetFunc — pass to `WithConditionalStaticResourcesController` |
| `getOperatorSyncState(operatorClient)` | Checking Managed/Removed in conditional logic |
| `resourceapply.ApplyCSIDriver` | Applying typed CSIDriver objects (handles hash + delete+recreate) |

---

## §6 Guardrails

### Structural
- All reconciliation logic lives in `pkg/operator/starter.go`. Do not create new reconciler files.
- The `CSIControllerSet` chain is built once in `RunOperator`. Do not add runtime chain modification.
- Do not create a second `ctrl.Manager` or use controller-runtime. This is a pure library-go operator.

### API
- `ClusterCSIDriver` is defined in `openshift/api` — do not add Go types to this repo.
- Do not run `make generate` or `make manifests` — there are no generated CRD types here.
- New API fields for SSCSI-254 must land in `openshift/api` (PR #2846) before the operator code compiles against them.

### Build
- Always build with `CGO_ENABLED=1`. Non-CGO builds are not valid for CI or production.
- Run `make check` (`make verify && make test-unit`) before any PR.
- Do not add build tags without updating the Makefile `GO_BUILD_FLAGS`.

### Asset Embedding
- New subdirectory assets require a new `//go:embed` glob in `assets/assets.go`.
- `csidriver.yaml` will remain in the embed — the dynamic enrichment is in-memory, not on-disk.

### OLM
- `make metadata VERSION=x.y` is the only safe way to bump OCP version in the CSV. Do not hand-edit `config/manifests/stable/*.clusterserviceversion.yaml` version fields.
- CSV `alm-status-descriptors` should reflect new spec fields added in SSCSI-254 — this is a separate task from the operator logic.

---

## §7 Change Cascade for SSCSI-254

| Change | Files to edit | Verification |
|--------|--------------|-------------|
| Dynamic AssetFunc in starter.go (CSIDriver enrichment) | `pkg/operator/starter.go` | `go build ./... && go vet ./...` |
| DaemonSetHookFunc in starter.go (rotation args) | `pkg/operator/starter.go` | `go build ./... && go vet ./...` |
| Wire ClusterCSIDriver informer to NodeService controller | `pkg/operator/starter.go` | `go build ./...` |
| Unit tests for new functions | `pkg/operator/starter_test.go` | `go test ./pkg/... ./cmd/... -v -count=1` |
| Full check | — | `make check` |
| OLM CSV alm-status-descriptors | `config/manifests/stable/*.clusterserviceversion.yaml` | `make metadata` |
| E2E tests (rotation scenarios, WIF migration, upgrade) | `hack/e2e.sh` (test authored separately) | Requires live cluster |

**No cascade to:**
- `assets/*.yaml` — `csidriver.yaml` stays as-is (base manifest, enriched in-memory)
- `assets/assets.go` — embed globs unchanged
- `go.mod` — no new dependencies expected (library-go already has `resourceread`, `resourceapply`)

---

## §8 Testing

### 8.1 Unit Tests

Pattern from `pkg/operator/starter_test.go`:
- `v1helpers.NewFakeOperatorClientWithObjectMeta(&ObjectMeta, &Spec, &Status, nil)` — library-go fake
- Table-driven with `t.Run(tc.name, ...)` — no external mock framework
- `t.Fatalf("expected %v, got %v", expected, actual)` — standard library only

**Copy-paste-ready commands:**
```bash
# Run full unit suite
go test ./pkg/... ./cmd/... -v -count=1

# Run operator package only
go test ./pkg/operator/... -v -count=1 -run TestGet

# Verify + unit (full check)
make check
```

### 8.2 Verification

```bash
make verify    # build-machinery-go: format, vet, imports, generated-code checks
make check     # make verify && make test-unit
go vet ./...   # standalone vet pass
```

### 8.3 E2E Tests

```bash
hack/e2e.sh   # Requires: live OpenShift cluster, openshift-tests in $PATH
```

E2E tests are not runnable locally. CI runs them in OpenShift CI via openshift/release (not in-repo).

### 8.4 SSCSI-254 Unit Test Coverage Requirements (from EP)

| Test case | What to assert |
|-----------|---------------|
| nil `driverConfig` → rotation enabled defaults | `requiresRepublish=true`, `--enable-secret-rotation=true`, `--rotation-poll-interval=2m` |
| nil `secretsStore` → same defaults | Same as above |
| `secretRotation.type: None` | `requiresRepublish=false`, `--enable-secret-rotation=false` |
| `secretRotation.type: Custom`, `minimumRefreshAge: 300` | `requiresRepublish=true`, `--rotation-poll-interval=5m0s` |
| `tokenRequests: nil` + existing CSIDriver tokenRequests | Existing tokenRequests preserved in desired spec |
| `tokenRequests.type: Managed` + audiences list | CSIDriver.spec.tokenRequests matches audiences list |
| `tokenRequests.type: Managed` + empty audiences | CSIDriver.spec.tokenRequests cleared |
| Hook: csi-driver container not found | Hook returns error |
| Namespace substitution: non-CSIDriver assets unchanged | `${NAMESPACE}` replaced, no `requiresRepublish` field injected |

---

## §9 Build & Platform

### 9.1 FIPS Build

```bash
# FIPS-compliant (CI/production)
CGO_ENABLED=1 GOEXPERIMENT=strictfipsruntime go build -trimpath -tags strictfipsruntime,openssl ./...

# Local non-FIPS (development only — not for CI)
CGO_ENABLED=1 go build -trimpath ./...
```

The Makefile auto-detects FIPS support and sets `GO` and `GO_BUILD_FLAGS` accordingly.

### 9.2 OCP Version Bump

```bash
make metadata VERSION=4.22
# Updates: config/manifests/secrets-store-csi-driver-operator.package.yaml
#          config/manifests/stable/*.clusterserviceversion.yaml
#          README.md, Makefile image refs
```

### 9.3 How to Add a New Static Asset (walkthrough)

1. Create `assets/<name>.yaml` (or in a covered subdirectory)
2. If a new subdirectory: add `<newdir>/*.yaml` to `//go:embed` in `assets/assets.go`
3. Add the asset filename to the string slice in `WithConditionalStaticResourcesController` in `starter.go`
4. Build: `go build ./...` (embed is resolved at compile time)
5. Verify: `make check`

No `make update-bindata` equivalent exists — Go embed is self-contained.

### 9.4 Platform Considerations

| Concern | Detail |
|---------|--------|
| Namespace | `openshift-cluster-csi-drivers` — both operator and operand run here |
| SCC | `privileged` — required for the CSI node DaemonSet (bind mounts, hostPath) |
| DaemonSet priority | `system-node-critical` — cannot be removed; prevents autoscaler eviction |
| CA bundle | CNO injects into `cabundle_cm.yaml` ConfigMap; hook mounts it into DaemonSet via projected volume |
| Proxy | `CSIConfigObserverController` propagates `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` |
| Network policy | Port 8095 (metrics) allowed inbound; no other ingress |
| MicroShift | Not supported — operator is not available on MicroShift |

---

## §10 Dependencies

### 10.1 Cross-Repo Dependency for SSCSI-254

| Dependency | Repo | Status | Impact |
|-----------|------|--------|--------|
| `CSIDriverConfigSpec.SecretsStore` API types | openshift/api PR #2846 | Open (must land first or concurrently) | Operator `starter.go` cannot compile against new fields until this merges |

**Action**: The operator implementation task must gate on `openshift/api` being available (or use a local replace directive during development). This is the critical path dependency.

### 10.2 Key Library-go Packages Used

| Package | Used for |
|---------|---------|
| `library-go/pkg/operator/csi/csicontrollerset` | CSIControllerSet builder |
| `library-go/pkg/operator/csi/csidrivernodeservicecontroller` | DaemonSet management + hook registration |
| `library-go/pkg/operator/resource/resourceapply` | ApplyCSIDriver, AssetFunc type |
| `library-go/pkg/operator/v1helpers` | FakeOperatorClient (tests), InformersForNamespaces |
| `library-go/pkg/operator/genericoperatorclient` | NewClusterScopedOperatorClientWithConfigName |
| `library-go/pkg/operator/management` | IsOperatorRemovable() |

---

## §11 Known Gaps & Absences (UNVERIFIED list)

### 11.1 Branch/Feature Absences

| Item | Status |
|------|--------|
| `ClusterCSIDriver.spec.driverConfig.secretsStore` | **NOT present** — requires openshift/api PR #2846 to land |
| Dynamic AssetFunc for CSIDriver | **NOT present** — greenfield implementation in starter.go |
| DaemonSetHookFunc for rotation args | **NOT present** — greenfield implementation in starter.go |
| `ClusterCSIDriver` informer wired to NodeService controller | **NOT present** — currently passes `nil`; must be replaced with `dynamicInformers` |
| Feature gates | **Not applicable** — this operator has none |
| Admission webhooks | **Not applicable** — validation is in openshift/api via CEL rules |
| controller-runtime | **Not used** — pure library-go |
| Per-resource reconciler files | **Do not exist** — all logic in starter.go |
| `test/apis/` validation test suite | **Not present** — no custom CRDs, CEL tests live in openshift/api |

### 11.2 Coverage Gaps (Unit Tests)

- No unit tests for the DaemonSet hook (does not yet exist)
- No unit tests for the dynamic CSIDriver AssetFunc (does not yet exist)
- No unit tests for tokenRequests preservation logic (does not yet exist)
- Existing `TestGetOperatorSyncState` covers only state machine logic

---

## §12 Quick Reference

### 12.1 Preflight Checklist (before writing code for SSCSI-254)

- [ ] `openshift/api` PR #2846 is available (merged or via local `replace` in `go.mod`)
- [ ] `go build ./...` passes on the current branch
- [ ] `make check` passes (verify + unit suite green)
- [ ] `pkg/operator/starter.go` read and understood (see §1.2 and §4.2)
- [ ] `assets/csidriver.yaml` read — understand base manifest structure
- [ ] `assets/node.yaml` read — identify `csi-driver` container and its args section

### 12.2 Quick Navigation

| Need | Go to |
|------|-------|
| All reconciliation wiring | `pkg/operator/starter.go:RunOperator()` |
| CSIControllerSet chain | `starter.go:73–116` |
| Current AssetFunc (namespace substitution) | `starter.go:replaceNamespaceFunc()` |
| Management state logic | `starter.go:getOperatorSyncState()` |
| DaemonSet template | `assets/node.yaml` |
| CSIDriver template | `assets/csidriver.yaml` |
| Unit test pattern | `pkg/operator/starter_test.go` |
| Build commands | `Makefile` (`make check`, `make build`, `make metadata`) |
| OLM metadata | `config/manifests/stable/*.clusterserviceversion.yaml` |
| OCP version bump | `hack/update-metadata.sh` |
| E2E runner | `hack/e2e.sh` |

### 12.3 Key Constants

| Constant | Value | File |
|----------|-------|------|
| `operatorName` | `"secrets-store-csi-driver-operator"` | `starter.go` |
| `operandName` | `"secrets-store-csi-driver"` | `starter.go` |
| `providerName` | `"secrets-store.csi.k8s.io"` | `starter.go` |
| `trustedCAConfigMap` | `"secrets-store-csi-driver-trusted-ca-bundle"` | `starter.go` |
| `namespaceKey` | `"${NAMESPACE}"` | `starter.go` |
| `resync` | `20 * time.Minute` | `starter.go` |
| Metrics port | `8095` | `network-policy/allow-ingress-to-metrics-operand.yaml` |
