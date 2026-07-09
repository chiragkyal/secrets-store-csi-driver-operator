# Repository Assessment Report
**Feature:** Configurable Secret Rotation and Workload Identity Federation for the Secrets Store CSI Driver (SSCSI-254)

## 0. Inputs & Tooling

- `repo`: `https://github.com/openshift/secrets-store-csi-driver-operator.git` (working-folder mode — analyzed the current checkout directly, no clone)
- `branch`: `openspec-ai-helpers-composer`
- `commit`: `953f4aee6f71a886390db4fc1e7aa931f450bb93`
- `tooling_status`: OK — full local filesystem and `vendor/` access; no network-dependent tools were required.
- Spec status: `specs.md` — **APPROVED** (Stage 1, this change). Feature spans 3 user stories (P1: rotation control, P1: WIF token audiences, P2: upgrade-safety preservation). `validation.json` (Stage 0) scored 93% PASS on the source Enhancement Proposal (`openspec/inputs/ep.md`).
- **Branch-verified greenfield finding (critical — see §1.3, §7, §11)**: the `SecretsStore` driver-type API surface this feature requires does **not** exist on this branch's vendored `github.com/openshift/api`. This is genuinely greenfield API work with a cross-repository dependency, not a hardening/delta task.

## 1. Architecture Overview

### 1.1 Project Type & Tech Stack

- **Language**: Go `1.25.0` (`go.mod` line 3).
- **Framework**: `library-go`'s CSI controller-set framework (`github.com/openshift/library-go/pkg/operator/csi/csicontrollerset`) — this operator uses **only** the library-go factory-controller pattern. There is no controller-runtime/kubebuilder reconciler anywhere in this repo (confirmed: no `sigs.k8s.io/controller-runtime` import in `go.mod`).
- **Key pinned dependencies** (`go.mod`): `github.com/openshift/api v0.0.0-20260302174620-dcac36b908db`, `github.com/openshift/client-go v0.0.0-20260302182750-20813ce71ca6`, `github.com/openshift/library-go v0.0.0-20260303171201-5d9eb6295ff6`, `k8s.io/client-go v0.35.2`, `k8s.io/apimachinery v0.35.2`.
- **Build system**: `make` via vendored `build-machinery-go` (`Makefile` includes `golang.mk`, `targets/openshift/deps-gomod.mk`, `targets/openshift/images.mk`, `targets/openshift/yq.mk`).
- **CRD ownership**: This repo does **not** own or generate the `ClusterCSIDriver` CRD — it is defined and generated entirely in `github.com/openshift/api` and consumed here only as a vendored Go type + externally-installed CRD (OLM/CVO delivers the CRD from the release payload, not this operator's bundle). There is no `config/crd/` directory in this repo.

### 1.2 Component Map

| Package | Responsibility | Generated? |
|---|---|---|
| `cmd/secrets-store-csi-driver-operator/` | CLI entry point (cobra command wiring only, no business logic) | Hand-written |
| `pkg/operator/starter.go` | `RunOperator`: builds clients/informers, wires the `CSIControllerSet`, defines `extractOperatorSpec`/`extractOperatorStatus`/`getOperatorSyncState`/`replaceNamespaceFunc` | Hand-written |
| `pkg/operator/starter_test.go` | Unit test for `getOperatorSyncState` only | Hand-written |
| `pkg/version/version.go` | Build version info + Prometheus gauge | Hand-written |
| `pkg/dependencymagnet/dependencymagnet.go` | Build-tag-guarded import to keep `build-machinery-go` vendored | Hand-written (do not remove — see §1.3) |
| `assets/` | Embedded YAML manifests (`//go:embed`) + `ReadFile()` wrapper | Hand-written wrapper, generated-at-build-time embed |
| `vendor/` | All third-party deps including `openshift/api`, `library-go` | Vendored (do not hand-edit) |

Dependency flow: `cmd/` → `pkg/operator.RunOperator` → `csicontrollerset.CSIControllerSet` (library-go) → reads `assets/*.yaml` via `assets.ReadFile` → applies via `resourceapply`/`staticresourcecontroller`/`csidrivernodeservicecontroller` (all library-go, vendored, not owned by this repo).

### 1.3 Framework & Pattern Architecture

**Single framework, no dual-pattern risk** (unlike operators that mix library-go and controller-runtime): everything here is `library-go` factory controllers composed via `csicontrollerset.NewCSIControllerSet(...)` method chaining in `pkg/operator/starter.go:73-116`.

**Entry point / bootstrap sequence** (`pkg/operator/starter.go:RunOperator`):
1. Build `kubeClient`, `kubeInformersForNamespaces` (scoped to operator namespace + cluster-scope `""` — line 45), and a `configMapInformer` for the operator namespace.
2. Build `configClient`/`configInformers` (20-minute resync, line 37/50) — used only by `CSIConfigObserverController`.
3. Build the generic cluster-scoped operator client for `ClusterCSIDriver` via `goc.NewClusterScopedOperatorClientWithConfigName(...)` (lines 55-63) — this is the **single** typed access point this operator has to the `ClusterCSIDriver` CR, and it goes through **unstructured conversion**, not a typed lister (see §1.3 dead-code/gap note below).
4. Chain the `CSIControllerSet`: `WithLogLevelController()` → `WithManagementStateController(operandName, true)` (removable=true) → `WithConditionalStaticResourcesController(...)` (8 static assets, lines 79-100) → `WithCSIConfigObserverController(...)` → `WithCSIDriverNodeService(...)` (DaemonSet, 1 existing hook: `WithCABundleDaemonSetHook`, lines 104-116).
5. Start all informers, then `csiControllerSet.Run(ctx, 1)`.

**Dead-code / do-not-edit trap**: `pkg/dependencymagnet/dependencymagnet.go` exists solely to keep `build-machinery-go` in `go.mod`/`vendor/` under a `tools` build tag — it is never compiled into the binary. **Do not remove it** and do not add real logic there.

**Architectural gap directly relevant to this feature** (branch-verified): `extractOperatorSpec`/`extractOperatorStatus` (`starter.go:173-201`) convert the `ClusterCSIDriver` unstructured object to `opv1.ClusterCSIDriver` **only to extract the generic `OperatorSpecApplyConfiguration`/`OperatorStatusApplyConfiguration`** (management state, log level, observed config) — they explicitly discard everything under `.spec.driverConfig`. There is currently **no code path anywhere in this repo that reads `ClusterCSIDriver.Spec.DriverConfig`**. Today the operator does not need to — no per-driver-type config is used. This feature is the first to require reading `driverConfig.secretsStore.*`, so a new read path (typed client `Get`/lister, not the generic operator client) must be introduced — see §4.2 and §7.

### 1.4 Runtime Data/Control Flow

Today (no `driverConfig.secretsStore` support yet):
1. Administrator edits `ClusterCSIDriver` (`spec.managementState`, `spec.logLevel`, etc.) — the only fields any controller here actually consumes.
2. `goc` generic operator client's informer fires → each library-go controller's `factory.Controller` picks up the resync trigger.
3. `WithConditionalStaticResourcesController` re-applies `csidriver.yaml` + 7 other static assets via `replaceNamespaceFunc` (byte-level `${NAMESPACE}` substitution only — **no ClusterCSIDriver-derived content**) — confirmed no dynamic `AssetFunc` exists today (`pkg/operator/starter.go:131-139`; `assets/csidriver.yaml` is a fully static 15-line manifest with no templated fields beyond namespace).
4. `WithCSIDriverNodeService` reconciles `node.yaml` (DaemonSet) — args are 100% static from the embedded YAML (`--enable-secret-rotation=true`, `--rotation-poll-interval=2m` hardcoded in `assets/node.yaml:45-46`) plus whatever `optionalDaemonSetHooks` mutate at runtime (today: only the CA-bundle hook).
5. Status is reported back to `ClusterCSIDriver.status` via the generic operator client's `ApplyOperatorStatus`.

For this feature, the flow **must be extended** at steps 3 and 4: a new dynamic `AssetFunc` (or a post-processing hook on the existing static resource path) for `csidriver.yaml`, and a new `DaemonSetHookFunc` for `node.yaml`, each of which must independently read `ClusterCSIDriver.Spec.DriverConfig.SecretsStore` — which requires a **new read path**, since neither the `DaemonSetHookFunc` signature (`func(*opv1.OperatorSpec, *appsv1.DaemonSet) error` — only the generic embedded spec, not the full `ClusterCSIDriverSpec`) nor the existing `AssetFunc` signature (`func(name string) ([]byte, error)` — no CR access at all) currently has access to driver-type-specific config. See §4.2/§7 for the concrete mechanism.

## 2. Target Files (Modification & Creation)

* `pkg/operator/starter.go`: (Modify, confidence: high) — wire a new typed read path for `ClusterCSIDriver` (e.g. an `operatorv1informers`/typed-client lister, since the existing `goc` generic client discards `driverConfig`); construct and register a new `csidrivernodeservicecontroller.DaemonSetHookFunc` for rotation args in the `WithCSIDriverNodeService(...)` call (line 104-116); wire whatever mechanism is chosen for the dynamic `CSIDriver` object mutation (either a custom `resourceapply.AssetFunc` passed into `WithConditionalStaticResourcesController`, or a small dedicated controller — see §11 risk).
* `pkg/operator/starter_test.go`: (Modify, confidence: high) — extend with table-driven tests for the new rotation-config-extraction and CSIDriver-field-mapping helper functions, following the existing `TestGetOperatorSyncState` pattern (`v1helpers.NewFakeOperatorClientWithObjectMeta`).
* `assets/csidriver.yaml`: (Modify or superseded by dynamic generation, confidence: high) — currently fully static (`podInfoOnMount`, `attachRequired`, `fsGroupPolicy`, `volumeLifecycleModes` only); needs `spec.requiresRepublish` and `spec.tokenRequests` populated dynamically per `ClusterCSIDriver` config, so this file becomes a *base template* read by a new `AssetFunc` rather than applied byte-for-byte.
* `assets/node.yaml`: (Modify, confidence: high) — the hardcoded `--enable-secret-rotation=true` / `--rotation-poll-interval=2m` args (lines 45-46) become the **fallback defaults** a new `DaemonSetHookFunc` must preserve when `driverConfig.secretsStore.secretRotation` is unset (upgrade-safety requirement, User Story 3).
* **`vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go`**: (New content required upstream, confidence: high — branch-verified absent) — `CSIDriverType` enum (line 116-117 in vendored copy) is currently `"";AWS;Azure;GCP;IBMCloud;vSphere` with **no `SecretsStore` value**, and `CSIDriverConfigSpec` (line 131-159) has no `SecretsStore *SecretsStoreCSIDriverConfigSpec` field. **This is not a file this repo can change directly** — it must land in `github.com/openshift/api` first, then be picked up here via `go.mod`/`go.sum`/`vendor/` bump. Do not hand-edit `vendor/`.
* `go.mod` / `go.sum` / `vendor/modules.txt` + regenerated `vendor/github.com/openshift/api/...`: (Modify, confidence: high, blocked on upstream) — once the `openshift/api` PR adding the `SecretsStore` variant merges and is tagged, run `go get github.com/openshift/api@<new-sha>` then `go mod vendor`.
* `config/manifests/stable/secrets-store-csi-driver-operator.clusterserviceversion.yaml`: (Likely no change needed, confidence: medium) — RBAC already grants full CRUD on `csidrivers` (storage.k8s.io, lines 274-284: create/get/list/watch/update/delete) and get/list/watch/update/patch on `clustercsidrivers` + `clustercsidrivers/status` (lines 140-160). No new RBAC verbs appear necessary for this feature; verify once the exact read mechanism (typed client vs. lister) is chosen.
* **Do NOT modify** `assets/node_sa.yaml`, `assets/rbac/*.yaml`, `assets/network-policy/*.yaml`, `assets/cabundle_cm.yaml` — none are implicated by this feature; touching them risks unrelated RBAC/network-policy regressions.
* **Do NOT modify** `pkg/dependencymagnet/dependencymagnet.go` (dead-code trap, see §1.3).

## 3. Reference Context (Read-Only)

### 3.1 Entry Points & Wiring
* `cmd/secrets-store-csi-driver-operator/main.go` — cobra command wiring only; confirms no business logic lives here.
* `pkg/operator/starter.go:40-129` (`RunOperator`) — the only place controllers are composed; any new controller/hook must be registered here.

### 3.2 API / Interface Patterns
* `vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go` — `ClusterCSIDriver`, `ClusterCSIDriverSpec`, `CSIDriverConfigSpec`, and the existing per-cloud config pattern (`AWSCSIDriverConfigSpec`, `AzureCSIDriverConfigSpec`, etc., lines 161-382) is the **exact structural precedent** a `SecretsStoreCSIDriverConfigSpec` addition should follow (discriminated union via `DriverType` + `+union` marker on `CSIDriverConfigSpec`, line 130).
* `vendor/github.com/openshift/library-go/pkg/operator/csi/csidrivernodeservicecontroller/csi_driver_node_service_controller.go:40-41` — `DaemonSetHookFunc` type definition.
* `vendor/github.com/openshift/library-go/pkg/operator/resource/resourceapply/generic.go:31` — `AssetFunc` type definition.
* `vendor/github.com/openshift/library-go/pkg/operator/resource/resourceapply/storage.go:141` — `ApplyCSIDriver(...)` (delete+recreate-on-spec-hash-change semantics the EP describes; called internally by the static-resource apply path, not directly by this repo's code).

### 3.3 Build, CI & Tooling
* `Makefile` — FIPS auto-detection (`GOEXPERIMENT=strictfipsruntime`, lines 32-43), `check`/`test-unit`/`test-e2e` targets, `metadata` target for OCP version bumps.
* `Dockerfile.openshift` — multi-stage operator image build.
* `.ci-operator.yaml` — Prow build-root image config (CI itself lives in `openshift/release`, not here).

### 3.4 Manifest / Config Generation Pipelines
* `assets/assets.go` — `//go:embed *.yaml rbac/*.yaml network-policy/*.yaml`; any new YAML subdirectory requires updating this directive.
* `hack/update-metadata.sh` — bumps OCP version across CSV/Makefile/README (not relevant to this feature unless a version bump ships alongside it).
* `hack/create-bundle` — builds OLM bundle + index images.
* No kustomize/Helm/upstream-sync pipeline exists in this repo (unlike operators with `bindata/` — confirmed by directory listing: no `bindata/` directory here).

### 3.5 Test Patterns & Fixtures
* `pkg/operator/starter_test.go` — the **only** existing test file in the repo; establishes the table-driven + `FakeOperator` struct + `v1helpers.NewFakeOperatorClientWithObjectMeta` pattern that new tests must follow.
* No `test/e2e/` directory was found in this repo's top-level tree; e2e is invoked via `hack/e2e.sh` (script-driven, not a Go test package under version control here at the reviewed depth) — see §8.4 for the coverage-gap implication.

## 4. Configuration Surface & Runtime Behavior

### 4.1 Current Configuration Surface

`ClusterCSIDriver` (`secrets-store.csi.k8s.io`), fields actually consumed by this operator today:

| Field | Type | Consumed by | Notes |
|---|---|---|---|
| `spec.managementState` | `opv1.ManagementState` (embedded `OperatorSpec`) | `getOperatorSyncState` (`starter.go:150-171`) | Drives Managed/Unmanaged/Removed branching for conditional static resources. |
| `spec.logLevel` / `spec.operatorLogLevel` | embedded `OperatorSpec` | `LogLevelController` (library-go) | Standard log-level sync, no custom code. |
| `spec.driverConfig` | `CSIDriverConfigSpec` | **Not consumed anywhere today** | This is the field the feature must extend (add `SecretsStore` variant) — branch-verified absent as a consumed or even typed-supported field for this driver. |

No `overrideArgs`/`overrideEnv`/`overrideResources`/`overrideScheduling` pattern exists in this operator (unlike cert-manager-style operators) — `node.yaml` args are either fully static or mutated only via the library-go `DaemonSetHookFunc` mechanism. There is no validation-allowlist code in this repo for container args; validation for the new `secretRotation`/`tokenRequests` fields will live entirely in `openshift/api` CEL rules (per the EP's proposed `+kubebuilder:validation:XValidation` markers), not in this operator's Go code.

### 4.2 Reconciliation / Processing Flow (Detailed)

| # | Stage | Trigger | Error behavior |
|---|---|---|---|
| 1 | `LogLevelController` sync | `operatorClient.Informer()` + periodic resync | Standard library-go retry; no custom handling. |
| 2 | `ManagementStateController` sync | Same | Standard library-go retry. |
| 3 | `ConditionalStaticResourcesController` sync (8 files incl. `csidriver.yaml`) | `operatorClient.Informer()` (confirmed: `staticresourcecontroller.NewStaticResourceController` wires `factory.New().WithInformers(operatorClient.Informer()).ResyncEvery(1 * time.Minute)` — `vendor/.../static_resource_controller.go:119`) + Kube informers added via `AddKubeInformers` (adds a `storage.k8s.io/v1 CSIDrivers` informer automatically when `csidriver.yaml` is in the file list — `vendor/.../static_resource_controller.go:255`) | On error, static resource controller reports Degraded via `ApplyOperatorStatus`; retried on next resync/informer event. |
| 4 | `CSIConfigObserverController` sync | `configInformers` (Infrastructure/Proxy/APIServer) | Standard library-go retry. |
| 5 | `CSIDriverNodeService` (DaemonSet) sync | `namespacedInformerFactory` DaemonSet informer + `optionalInformers` (currently only the CA-bundle ConfigMap informer, via `WithCABundleDaemonSetHook`'s closure) | Hook errors (`DaemonSetHookFunc` returning non-nil) abort that sync pass; controller retries on next resync. |

**Gap for this feature**: step 3 (CSIDriver mutation) and step 5 (DaemonSet arg mutation) both need `ClusterCSIDriver.Spec.DriverConfig.SecretsStore` data, but **neither currently has a lister/typed-client reference to `ClusterCSIDriver` beyond the generic operator client**, which (per §1.3) discards `driverConfig` during extraction. Implementation must introduce a new typed read path (e.g. `github.com/openshift/client-go/operator/clientset/versioned` + informer, already transitively vendored since `operatorinformer` is imported by `csicontrollerset.go`, or a direct `Get` call against the typed clientset inside each hook/AssetFunc closure) — this is the single most consequential design decision for `plan.md`.

### 4.3 Image / Dependency Resolution

* Images are injected via env vars on the operator pod and substituted into `node.yaml` at apply time: `${DRIVER_IMAGE}`, `${NODE_DRIVER_REGISTRAR_IMAGE}`, `${LIVENESS_PROBE_IMAGE}`, `${LOG_LEVEL}` (`assets/node.yaml:35,90,126,40,93,132`). Substitution mechanism itself (library-go's DaemonSet controller variable expansion) is unaffected by this feature — no new images are introduced.
* `README.md:26-29` documents the exact env vars for local/CLI runs.

### 4.4 Status / Health Reporting

* Single status system: standard OpenShift `OperatorStatus` (`Available`/`Progressing`/`Degraded` conditions) applied via the generic operator client's `ApplyOperatorStatus` — confirmed in `docs/error-handling-guidelines.md:19-26` and `starter.go`'s use of `goc.NewClusterScopedOperatorClientWithConfigName`. There is no secondary/custom condition system (unlike operators with per-addon CRDs).
* Error classification: none of the library-go controllers used here expose a custom irrecoverable-vs-retryable distinction visible in this repo's code — `getOperatorSyncState` treats any error fetching operator state as `Unmanaged` (fail-safe, `starter.go:151-155`), which is the only custom error-handling logic in the package.

### 4.5 Feature Gate / Feature Flag Mechanism

* **No feature-gate mechanism exists in this repo.** No `features.go`, no `FeatureGate` usage, no cluster `FeatureSet` discovery code was found anywhere under `pkg/`. This operator ships features directly to GA/Managed state with no TechPreview gating layer of its own (any TechPreview gating for the new API field would live entirely in `openshift/api`'s own FeatureGate system, per its `AGENTS.md`, not in this repo). This matches the EP's Graduation Criteria ("target GA directly").

## 5. Reusable Assets (Anti-Duplication)

* `csidrivernodeservicecontroller.WithCABundleDaemonSetHook` (`vendor/.../csidrivernodeservicecontroller/helpers.go:32-75`): Use this as the **structural template** for a new rotation-args `DaemonSetHookFunc` — it demonstrates the exact pattern (closure-captured lister → read config → mutate `daemonSet.Spec.Template.Spec` → hash-annotate for rollout via `addObjectHash`). Do not reimplement hook wiring from scratch.
  Evidence: read in full; already registered in `starter.go:111-115`.
* `csidrivernodeservicecontroller.WithConfigMapHashAnnotationHook` / `WithSecretHashAnnotationHook` (same file, lines 78-115): reusable if the new config source needs a rollout-triggering hash annotation on the DaemonSet pod template — use `addObjectHash` (line 117) rather than writing custom annotation logic.
* `resourceapply.ApplyCSIDriver` (`vendor/.../resourceapply/storage.go:141`): already implements the delete+recreate-on-spec-hash-change semantics the EP requires for mutating `CSIDriver.spec.requiresRepublish`/`tokenRequests` — it is invoked internally by the static-resource-controller apply path already used for `csidriver.yaml`. Do not write a custom apply/diff routine for the `CSIDriver` object.
* `v1helpers.NewFakeOperatorClientWithObjectMeta` (`github.com/openshift/library-go/pkg/operator/v1helpers`, used in `starter_test.go:65`): use for any new unit test that needs a fake `ClusterCSIDriver`/operator client — do not introduce a mocking framework.
* `resourceapply.AssetFunc` + `replaceNamespaceFunc` pattern (`starter.go:131-139`): the existing namespace-substitution `AssetFunc` shows the exact closure-over-context style expected; a new dynamic `AssetFunc` for `csidriver.yaml` should follow the same shape (`func(name string) ([]byte, error)`) but additionally read `ClusterCSIDriver` state before returning bytes.
* `github.com/openshift/client-go/operator/informers/externalversions` (already an indirect/transitive dependency via `library-go`'s `csicontrollerset` package, which imports it as `operatorinformer` for other hook constructors like `WithStorageClassController`/`WithCredentialsRequestController`): use this for a typed `ClusterCSIDriver` informer/lister rather than hand-rolling `dynamicClient` unstructured polling.

## 6. Architectural Guardrails

- **Structural**: Controllers are composed via `csicontrollerset` method chaining in one function (`RunOperator`) — do not create a second composition root or a parallel `main`-adjacent wiring path. New hooks/controllers are added as arguments to existing `With*` calls or as new `With*` chain calls, not as free-standing goroutines.
- **Structural**: `cmd/` contains no business logic (verified: only cobra wiring) — keep all new logic in `pkg/operator/`.
- **API / Schema**: This repo does not own the `ClusterCSIDriver` CRD schema. Any new field must be added in `github.com/openshift/api` first (following the existing `CSIDriverConfigSpec` discriminated-union pattern, §3.2) and vendored in — this repo cannot unilaterally add API fields.
- **API / Schema**: `CSIDriverConfigSpec` is a `+union` type keyed by `DriverType` (evidence: `+union` marker + `+unionDiscriminator` on `DriverType`, `types_csi_cluster_driver.go:130,137`) — a `SecretsStore` variant must follow the same one-field-per-driver-type convention, not a generic/shared config blob.
- **Build / Tooling**: Go 1.25.0 pinned in `go.mod`; FIPS build via `GOEXPERIMENT=strictfipsruntime` auto-detected in `Makefile:32-43` — local builds without FIPS support still succeed but produce a non-production binary (warning only, not a failure).
- **Build / Tooling**: `make verify` checks `go vet`, `gofmt`, and Go-version consistency across `go.mod`/Dockerfile (`docs/testing-guidelines.md:49-56`) — run before every commit.
- **Deployment / Packaging**: All manifests are Go-embedded (`//go:embed`) — no runtime file I/O for assets. Adding a new asset subdirectory requires updating the embed glob in `assets/assets.go:7`.
- **Deployment / Packaging**: Static resources are split into unconditional (`WithStaticResourcesController` — unused in this repo, confirmed no call site) vs. **conditional** (`WithConditionalStaticResourcesController`, the only static-resource controller actually registered) keyed on Managed/Removed state via `shouldCreateFnArg`/`shouldDeleteFnArg` (`starter.go:95-100`).
- **Code Generation**: There is **no** code generation in this repo (no `zz_generated.*`, no `bindata.go`, no `hack/update-*-manifests.sh`) — everything under `pkg/`, `cmd/`, `assets/` is hand-written or hand-embedded. The only generated code this operator depends on lives in `vendor/` (from `openshift/api`/`client-go`), which is regenerated upstream, not here.
- **Security**: Least-privilege per-component RBAC — do not consolidate the `privileged_role.yaml` and `secretproviderclasses_role.yaml` ClusterRoles or grant new resources to the wrong one (`docs/security-guidelines.md:5-10`).
- **Security**: RBAC for `clustercsidrivers`, `clustercsidrivers/status`, and `csidrivers` (storage.k8s.io) already grants everything this feature is expected to need (get/list/watch/update/patch and full CRUD respectively, §2) — do not add duplicate or broader RBAC without first confirming a genuine gap.

## 7. Change Cascade Checklist

| When you change... | You must also... | Verify with... |
|---|---|---|
| `github.com/openshift/api` gains `SecretsStore` in `CSIDriverConfigSpec` (external repo) | Bump `go.mod`/`go.sum` to the new `openshift/api` commit, run `go mod vendor`, confirm `vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go` now has `SecretsStoreDriverType`/`SecretsStoreCSIDriverConfigSpec` | `go mod tidy && go mod vendor && make verify` |
| `pkg/operator/starter.go` — new typed read path for `ClusterCSIDriver.Spec.DriverConfig` | Add the new informer/lister to `RunOperator`'s informer-start block (`starter.go:118-121`) so it's actually started; add corresponding RBAC verbs only if a gap is found (§6) | `make test-unit`, manual RBAC diff review against `config/manifests/stable/*.clusterserviceversion.yaml` |
| `assets/csidriver.yaml` becomes dynamically generated | Replace the static `AssetFunc` passed for this file in `WithConditionalStaticResourcesController` (`starter.go:79-100`) with a wrapping `AssetFunc` that reads `ClusterCSIDriver` before returning bytes; add/extend unit tests in `starter_test.go` | `make test-unit` |
| `assets/node.yaml` args become conditional (`--enable-secret-rotation`, `--rotation-poll-interval`) | Add a new `DaemonSetHookFunc` and register it as an additional variadic arg to `WithCSIDriverNodeService(...)` (`starter.go:104-116`); preserve the existing hardcoded values as defaults for the upgrade-safety path (User Story 3) | `make test-unit` + manual `oc get ds ... -o jsonpath='{...args}'` per `docs/support-procedures` pattern in the EP |
| Any change under `pkg/` or `cmd/` | Run `gofmt`, `go vet`, confirm Go version consistency | `make verify` |
| Any change to `assets/*.yaml` | Confirm the `//go:embed` glob in `assets/assets.go:7` still covers the path (only relevant if a new subdirectory is added — not expected for this feature) | Build the binary (`make build`) — a missing glob panics at runtime, not at compile time |
| RBAC needs actually change (unexpected per §6, but verify) | Update `assets/rbac/*.yaml` **and** the corresponding CSV RBAC block in `config/manifests/stable/secrets-store-csi-driver-operator.clusterserviceversion.yaml` — these are two independent sources of truth in this repo (bundle CSV is not generated from `assets/rbac/`) | Manual diff review; no automated CSV/RBAC-sync tooling found in this repo |
| Before opening a PR | Full local verification | `make verify && make test-unit` |

## 8. Test & CI Reference

### 8.1 Test Structure
* Unit tests: `pkg/operator/starter_test.go` only (co-located with `starter.go`, same package `operator`). No other `_test.go` files exist under `pkg/` or `cmd/` at the reviewed depth.
* Framework: standard library `testing` only — no `testify`, no `ginkgo` for unit tests (confirmed: `docs/testing-guidelines.md:19` explicitly forbids third-party assertion/mocking libraries).
* E2E: driven by `hack/e2e.sh`, not a version-controlled Go test package visible in this repo's top-level tree at the depth reviewed.

### 8.2 How to Run Tests Locally
```
make test-unit          # go test ./pkg/... ./cmd/...
make verify             # go vet + gofmt + Go version consistency
make check              # runs verify then test-unit
make test-e2e           # hack/e2e.sh — REQUIRES a live OpenShift cluster + oc CLI; not runnable locally without one
```
Expected runtime: `make test-unit` is sub-second today (single small test file); `make test-e2e` requires cluster access and is not expected to complete locally.

### 8.3 CI Pipeline
* CI runs in OpenShift Prow; configuration lives in `openshift/release`, not this repo (confirmed absence of `.prow.yaml`/`openshift-ci` config here — only `.ci-operator.yaml` for the build-root image).
* Every PR runs `make verify` and `make test-unit` (per `AGENTS.md` "CI/CD" section and `docs/testing-guidelines.md:59-65`).
* CI builds enforce FIPS via `GOEXPERIMENT=strictfipsruntime` + `-tags strictfipsruntime,openssl`.
* E2E runs as separate Prow jobs against real clusters — not a required PR gate visible in this repo.

### 8.4 Test Coverage Gaps
* **No unit test coverage exists today for anything this feature touches**: `RunOperator` itself, the `WithConditionalStaticResourcesController`/`WithCSIDriverNodeService` wiring, and any DaemonSet-hook logic are entirely untested — the only existing unit test (`TestGetOperatorSyncState`) covers management-state branching, unrelated to `driverConfig`. This means the new rotation-config-extraction and CSIDriver-field-mapping logic will need **new** test files/functions from scratch, not extensions of existing coverage.
* E2E is the only place today's `csidriver.yaml`/`node.yaml` reconciliation is exercised end-to-end, and that requires a live cluster — plan for both new unit tests (fast feedback, following the `FakeOperatorClient` pattern) and new e2e scenarios (the EP's Test Plan already enumerates these in detail).

## 9. Developer Workflow

### 9.1 Key Commands Reference

| Command | Purpose |
|---|---|
| `make` / `make build` | Build the operator binary |
| `make test-unit` | `go test ./pkg/... ./cmd/...` |
| `make verify` | `go vet`, `gofmt` check, Go version consistency |
| `make check` | `verify` then `test-unit` |
| `make test-e2e` | `hack/e2e.sh` (requires live cluster + `oc`) |
| `make update-gofmt` | Auto-fix formatting |
| `make metadata VERSION=X.Y` | Bump OCP version across CSV/Makefile/README via `hack/update-metadata.sh` |
| `make clean` | Remove binary and `yq` tool |

**Preflight before every PR**: `make verify && make test-unit`.

### 9.2 Version Variables
* Go version: pinned in `go.mod` (`1.25.0`); must stay consistent with the Dockerfile (checked by `make verify`).
* Operand images (`DRIVER_IMAGE`, `NODE_DRIVER_REGISTRAR_IMAGE`, `LIVENESS_PROBE_IMAGE`) are **not** version variables in the Makefile — they're runtime env vars injected by OLM/CVO from the release payload (`README.md:26-29`); this feature does not touch them.
* OCP version strings live in `config/manifests/*.yaml` and `Makefile`, updated only via `hack/update-metadata.sh` — unrelated to this feature unless a version bump ships in the same PR.

### 9.3 Local Development Setup
* Apply a minimal `ClusterCSIDriver` CR (`README.md:9-20`), `make build`, set `OPERATOR_NAME`/`DRIVER_IMAGE`/`NODE_DRIVER_REGISTRAR_IMAGE`/`LIVENESS_PROBE_IMAGE` env vars, then run `./secrets-store-csi-driver-operator start --kubeconfig $KUBECONFIG --namespace openshift-cluster-csi-drivers` against a real or test cluster.
* Requires: Go 1.25+, `make`, access to an OpenShift cluster for meaningful local testing (unit tests run without a cluster).

### 9.4 Common Development Scenarios

**How to add a new `DaemonSetHookFunc` (the pattern this feature needs for rotation args)**:
1. Define the hook in a new or existing file under `pkg/operator/` (there is no dedicated `hooks.go` today — `starter.go` is the only file with controller-set logic, so either add a new file `pkg/operator/daemonset_hooks.go` or keep it in `starter.go` if small).
2. Follow the exact shape of `csidrivernodeservicecontroller.WithCABundleDaemonSetHook` (§5): accept whatever lister/client the hook needs as constructor params, return a `func(*opv1.OperatorSpec, *appsv1.DaemonSet) error` closure.
3. Since the `DaemonSetHookFunc` signature does not carry `ClusterCSIDriver.Spec.DriverConfig`, the closure must independently read the CR (typed client `Get` or a dedicated informer/lister — see §4.2 gap).
4. Register the hook as an additional variadic argument to `WithCSIDriverNodeService(...)` in `starter.go:104-116`.
5. Add a unit test in `starter_test.go` (or a new `_test.go` next to the hook file) using `v1helpers.NewFakeOperatorClientWithObjectMeta`.
6. Run `make verify && make test-unit`.

**How to add a new API field on `ClusterCSIDriver`** (not directly doable in this repo): file/land the change in `github.com/openshift/api` (following that repo's own `AGENTS.md` — new stable-API fields need `+optional`, documented defaults, and any cross-field constraints via `+kubebuilder:validation:XValidation`), then bump `go.mod`/vendor here once merged (§7).

## 10. Platform & Environment Integration

### 10.1 Security Context & Permissions
* Node DaemonSet runs under the `privileged` SCC (`assets/rbac/privileged_role.yaml` + `assets/rbac/node_privileged_binding.yaml`) — required for CSI mount operations; this feature does not change the SCC/security-context posture of any container.
* `docs/security-guidelines.md` documents the least-privilege RBAC split — this feature's RBAC needs are already covered (§6).

### 10.2 Proxy & Network Configuration
* Proxy propagation hook exists in library-go (`WithObservedProxyDaemonSetHook`, §5) but is **not currently registered** in `starter.go` — only the CA-bundle hook is active. Not relevant to this feature, noted for completeness.
* CA bundle injection: `assets/cabundle_cm.yaml` + `WithCABundleDaemonSetHook` — established pattern this feature's new hook should sit alongside (registered as an additional variadic arg, not a replacement).
* Network policy: `assets/network-policy/allow-ingress-to-metrics-operand.yaml` — unrelated to this feature.

### 10.3 Cloud Provider Integration
* This operator has **no CCO/CredentialsRequest integration today** (no `CredentialsRequest` asset or `WithCredentialsRequestController` call site found) — the operator itself does not need cloud credentials. Workload identity federation (WIF) in this feature is entirely about the **CSI driver operand** requesting service-account tokens for **workload pods** to present to cloud providers — it is not about the operator's own credentials. This is an important scope distinction for `plan.md`: no CCO/`CredentialsRequest` work is implied by this feature.

### 10.4 Build & Compliance Constraints
* FIPS: `GOEXPERIMENT=strictfipsruntime` + `-tags strictfipsruntime,openssl`, `CGO_ENABLED=1` in CI (`Makefile:32-43`). This feature adds no new cryptographic code, so no FIPS-specific concerns are expected — new Go code should simply compile under the existing flags.
* No multi-arch-specific Dockerfile branching was found beyond the standard `Dockerfile.openshift` multi-stage build.
* Disconnected/air-gapped: images are already fully substitution-based (§4.3); no new image references are introduced by this feature.

### 10.5 Console / UI Integration
* `config/manifests/stable/sscsi-example-quickstart.yaml` and `sscsi-sample-secretproviderclass-*.yaml` exist as console quickstart/sample content for the `SecretProviderClass` CR (a **different** CRD, owned by the upstream driver, not `ClusterCSIDriver`). Not applicable to this feature — no console changes are implied by rotation/WIF configuration on `ClusterCSIDriver`.

### 10.6 Packaging & Lifecycle
* OLM bundle: `config/manifests/secrets-store-csi-driver-operator.package.yaml` + `config/manifests/stable/secrets-store-csi-driver-operator.clusterserviceversion.yaml` (single `stable` channel visible in this checkout).
* This feature requires **no CSV/bundle changes** beyond the RBAC verification already covered in §6/§7 — no new owned CRDs, no new `relatedImages`, no new install modes.
* `hack/create-bundle` builds bundle + index images; `hack/update-metadata.sh` bumps versions — neither is directly implicated unless this feature ships alongside a version bump.

## 11. Risks & Downstream Impacts

* **Cross-repository API dependency (highest risk)**: The entire feature is blocked on a `github.com/openshift/api` change landing and being vendored first (§0, §2, §7). Impact: this repo's implementation work cannot proceed past API-consuming code until that dependency is satisfied; `plan.md`/`tasks.md` must explicitly sequence "wait for/land upstream API change" as a blocking phase, not assume it's already available. Mitigation: confirm with the API approvers (`api-approvers: ["@JoelSpeed"]` per the EP frontmatter) on timeline before committing to a delivery date; consider whether a local `replace` directive in `go.mod` against a fork/branch of `openshift/api` is acceptable for development-time iteration (not for merge).
* **No existing typed read path for `ClusterCSIDriver.Spec.DriverConfig`**: Every controller here today only reads the generic `OperatorSpec` embedded fields. Impact: implementing the two new read points (CSIDriver mutation, DaemonSet hook) requires introducing genuinely new plumbing (informer/lister or typed `Get` calls), which is more invasive than "add a field and read it" — there is no existing per-driver-type config consumer in this operator to copy from (this operator has never used `driverConfig` for anything). Mitigation: design this as a single shared helper (e.g. a small `secretsstoreconfig` helper package or file) used by both the AssetFunc and the DaemonSetHookFunc, rather than duplicating CR-fetch logic in two places.
* **`CSIDriver.spec` immutability / delete-recreate window**: `resourceapply.ApplyCSIDriver` (§5) already handles delete+recreate on spec-hash change, but this means every time an administrator changes rotation/WIF config, there is a brief window where the `CSIDriver` object doesn't exist. Impact: low per the EP's own analysis (kubelet caches CSIDriver info; no running pod mount is expected to be affected), but this repo has **no existing test that exercises this delete-recreate path** — coverage gap (§8.4) that should be closed with a new unit or e2e test specifically for the mutation-triggered recreate.
* **RBAC assumption not independently verified for a chosen implementation**: §6/§7 conclude no new RBAC is needed based on the currently-known access patterns (typed client `Get`/informer against `clustercsidrivers`, already fully permitted). If `plan.md` chooses a different mechanism (e.g. a `dynamicClient`-based watch), re-verify RBAC verbs against that specific access pattern before assuming no change is needed.
* **Upgrade-safety default preservation (User Story 3) has no existing test scaffold**: today's hardcoded `--enable-secret-rotation=true --rotation-poll-interval=2m` in `assets/node.yaml` is the *only* current behavior — there is no existing "preserve prior tokenRequests on the live CSIDriver object" logic anywhere in this repo to build on (this operator has never read the live `CSIDriver` object before writing to it). Impact: the nil-safety/preservation logic the EP describes in detail (5-level nil-check cascade) is entirely new code with no precedent in this codebase — treat it as a first-class, well-tested unit alone, not a small addition to existing preservation logic.

### 11.1 Assessment Limitations / UNVERIFIED Items

* `test/e2e/` directory contents were not opened/enumerated at file level (only referenced via `hack/e2e.sh` and `docs/testing-guidelines.md`) — verify actual e2e test file organization by reading `hack/e2e.sh` in full and any Go test files it invokes, before finalizing e2e task scoping in `plan.md`.
* The exact mechanism by which `github.com/openshift/client-go/operator/informers/externalversions` would be wired into `RunOperator` was reasoned from its use in `csicontrollerset.go` (as `operatorinformer`, passed into other `With*` methods like `WithStorageClassController`) but **no call site instantiating it exists in this repo's `starter.go` today** — this is a design recommendation based on library-go's exposed API surface, not a pattern already proven working in this specific operator. Verify by prototyping the informer construction against the real `controllerConfig.KubeConfig` before committing to this approach in `plan.md`.
* Whether `openshift/api`'s `SecretsStore` addition is already in-flight upstream (a PR open, a target OCP release) was **not verified** — this assessment only confirms its **absence** on the vendored commit pinned in this repo's `go.mod`. The Planning stage should ask the user/EP authors for the actual upstream PR status before sequencing work.
* CSV RBAC verbs were read for `clustercsidrivers`, `clustercsidrivers/status`, and `csidrivers` only (the resources named in the EP) — the full RBAC block (`config/manifests/stable/secrets-store-csi-driver-operator.clusterserviceversion.yaml`) was not read end-to-end; if a different resource turns out to be needed (unlikely per current design), re-check.
* `config/manifests/art.yaml` and ART version-substitution rules were not opened — not expected to be relevant to this feature, but flagged since it wasn't verified.

## 12. Quick Reference Card

### Preflight Checklist (run before every PR)
```
1. make verify
2. make test-unit
3. (optional, requires cluster) make test-e2e
```

### Key File Quick-Nav
| I want to... | Look at... |
|---|---|
| Wire a new controller/hook | `pkg/operator/starter.go` (`RunOperator`, lines 40-129) |
| Add a `DaemonSetHookFunc` | New file under `pkg/operator/` following `vendor/.../csidrivernodeservicecontroller/helpers.go` pattern; register in `starter.go:104-116` |
| Understand today's static `CSIDriver` manifest | `assets/csidriver.yaml` (15 lines, fully static today) |
| Understand today's DaemonSet args | `assets/node.yaml:37-48` (hardcoded rotation flags) |
| Check/extend RBAC | `assets/rbac/*.yaml` (asset-level) **and** `config/manifests/stable/secrets-store-csi-driver-operator.clusterserviceversion.yaml` (bundle-level — two independent sources) |
| Add a unit test | `pkg/operator/starter_test.go` pattern (`FakeOperator` + `v1helpers.NewFakeOperatorClientWithObjectMeta`) |
| Check the upstream API type this feature depends on | `vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go` (currently missing `SecretsStore` — branch-verified) |
| Bump a vendored dependency | `go.mod`/`go.sum` + `go mod vendor`, then `make verify` |
