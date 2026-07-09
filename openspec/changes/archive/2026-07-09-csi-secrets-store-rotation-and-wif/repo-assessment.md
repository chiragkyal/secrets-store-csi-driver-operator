# Repository Assessment Report

## 0. Assessment Header

| Field | Value |
|---|---|
| Repository | `secrets-store-csi-driver-operator` (working-folder mode — analyzed in place, no clone) |
| Root | `/Users/ckyal/go/src/github.com/chiragkyal/secrets-store-csi-driver-operator` |
| Remotes | `origin` → `git@github.com:chiragkyal/secrets-store-csi-driver-operator.git` (fork); `openshift` → `https://github.com/openshift/secrets-store-csi-driver-operator.git` (upstream) |
| Branch | `openspec-cursor-agent-sonnet5` |
| Commit | `573b5a09` |
| Go version (module / local) | `go 1.25.0` (go.mod) / local toolchain `go1.25.4 darwin/arm64` |
| Operator/OLM version | `5.0.0` (CSV `secrets-store-csi-driver-operator.v5.0.0`, `olm.skipRange: ">=4.13.0-0 <5.0.0"`) |
| tooling_status | OK — full read/shell/grep access to source, vendor, and git history; no BLOCKED sections |
| Spec status | `specs.md` — **Approved** (User Story 1–4, FR-001…FR-012, 1 open `[NEEDS CLARIFICATION]` on downgrade behavior) |
| Validation status (Stage 0) | `validation.json` — **PASS** (87%), approved |

**Correction to a Stage 0/1 assumption**: `validation.json` flagged `"The feature is be supported since OpenShift 5.0"` in the source enhancement proposal as a likely version typo, and `specs.md` (A-006) treated the exact version as a documentation detail out of scope. Repo evidence contradicts that suspicion: this repo's CSV (`v5.0.0`), package manifest (`currentCSV: ...v5.0.0`), `Makefile` (`ocp/5.0` image registry path), `.ci-operator.yaml` (`...openshift-5.0` build root), and `Dockerfile.openshift` (`ocp/5.0:base-rhel9`) all consistently target **OpenShift 5.0**. The enhancement proposal's version reference is correct, not a typo. This does not require reopening the locked `validation`/`specs` artifacts (no functional-requirement impact), but the Planning Agent should treat "OpenShift 5.0" as the accurate target release.

## 1. Architecture Overview

### 1.1 Controller Framework

This operator is a single-binary Kubernetes operator built on `library-go`'s `csicontrollerset` framework (not `controller-runtime`/`kubebuilder`). All controllers are composed via method chaining in one function:

```23:129:pkg/operator/starter.go
func RunOperator(ctx context.Context, controllerConfig *controllercmd.ControllerContext) error {
	...
	csiControllerSet := csicontrollerset.NewCSIControllerSet(
		operatorClient,
		controllerConfig.EventRecorder,
	).WithLogLevelController().WithManagementStateController(
		operandName,
		true, // Set this operator as removable
	).WithConditionalStaticResourcesController(
		"SecretsStoreConditionalStaticResourcesController",
		...
	).WithCSIConfigObserverController(
		"SecretsStoreDriverCSIConfigObserverController",
		configInformers,
	).WithCSIDriverNodeService(
		"SecretsStoreDriverNodeServiceController",
		replaceNamespaceFunc(operatorNamespace),
		"node.yaml",
		kubeClient,
		kubeInformersForNamespaces.InformersFor(operatorNamespace),
		nil,
		csidrivernodeservicecontroller.WithCABundleDaemonSetHook(
			operatorNamespace,
			trustedCAConfigMap,
			configMapInformer,
		),
	)
	...
}
```

There are exactly **five** controllers, all registered in `RunOperator` (`pkg/operator/starter.go`):

1. `LogLevelController` — syncs `spec.logLevel` from `ClusterCSIDriver` to the operator (no changes needed for this feature).
2. `ManagementStateController` — handles Managed/Unmanaged/Removed. This operator is registered **removable** (`true` at L78).
3. `SecretsStoreConditionalStaticResourcesController` — a `WithConditionalStaticResourcesController` instance reconciling 8 static files (RBAC, ServiceAccount, `csidriver.yaml`, ConfigMap, NetworkPolicy) using **one shared** `AssetFunc` (`replaceNamespaceFunc`).
4. `SecretsStoreDriverCSIConfigObserverController` — observes cluster `Infrastructure`/`Proxy`/`APIServer` config objects, **not** `ClusterCSIDriver.spec.driverConfig`. Not relevant to this feature's data path.
5. `SecretsStoreDriverNodeServiceController` — a `WithCSIDriverNodeService` instance managing the `secrets-store-csi-driver-node` DaemonSet from `node.yaml`, currently with exactly one `DaemonSetHookFunc` (`WithCABundleDaemonSetHook`) and `nil` for `optionalInformers`.

### 1.2 Operator Client and CR Watching

The operator uses a **generic** operator client, not a typed `ClusterCSIDriverLister`:

```52:66:pkg/operator/starter.go
	gvr := opv1.SchemeGroupVersion.WithResource("clustercsidrivers")
	gvk := opv1.SchemeGroupVersion.WithKind("ClusterCSIDriver")
	operatorClient, dynamicInformers, err := goc.NewClusterScopedOperatorClientWithConfigName(
		clock.RealClock{},
		controllerConfig.KubeConfig,
		gvr,
		gvk,
		providerName,
		extractOperatorSpec,
		extractOperatorStatus,
	)
```

`operatorClient.GetOperatorState()` returns `(*opv1.OperatorSpec, *opv1.OperatorStatus, string, error)` — the **generic** `OperatorSpec`/`OperatorStatus` types, which do **not** include `ClusterCSIDriverSpec.DriverConfig`. There is currently no code path anywhere in this repo that reads `ClusterCSIDriver.Spec.DriverConfig`. This is the single most important architectural gap this feature must close (see §1.3 and §11).

### 1.3 Dead Code / Traps for the Planner

- **`optionalInformers` is `nil` today** (`WithCSIDriverNodeService(... , nil, csidrivernodeservicecontroller.WithCABundleDaemonSetHook(...))`, L110 of `starter.go`). A `DaemonSetHookFunc` closure that needs to react to `ClusterCSIDriver` changes (not just resync every 1 minute) MUST add the operator client's informer (or a dedicated `ClusterCSIDriver` informer) to this slice — copying the `WithCABundleDaemonSetHook` pattern of taking an informer via closure is necessary but not sufficient for *event-driven* resync; the informer must also be passed into `optionalInformers` for the controller's `factory.New().WithInformers(...)` wiring to actually trigger a sync on CR change.
- **`DaemonSetHookFunc` signature is `func(*opv1.OperatorSpec, *appsv1.DaemonSet) error`** (`vendor/.../csidrivernodeservicecontroller/csi_driver_node_service_controller.go:41`). The `*opv1.OperatorSpec` parameter passed to every hook is the **generic** spec — it does **not** carry `DriverConfig`. A hook implementing `--enable-secret-rotation`/`--rotation-poll-interval` **cannot** get the rotation config from its function parameters; it must close over its own lister/getter for the live `ClusterCSIDriver` object (exactly as `WithCABundleDaemonSetHook` closes over a `configMapInformer` rather than reading from `opSpec`). This is not obvious from the hook's type signature alone and is a common integration mistake.
- **`WithConditionalStaticResourcesController` applies ONE `AssetFunc` uniformly across ALL files in its list** (`vendor/.../csicontrollerset/csi_controller_set.go:90-113`; confirmed by `staticresourcecontroller.WithConditionalResources`, which stores a single `manifests resourceapply.AssetFunc` per call, `vendor/.../staticresourcecontroller/static_resource_controller.go:159-185`). Today `csidriver.yaml` shares the exact same `replaceNamespaceFunc` as RBAC/SA/ConfigMap/NetworkPolicy files. There is **no per-file hook mechanism** in this controller (unlike `WithCSIDriverNodeService`'s `DaemonSetHookFunc`). Making `csidriver.yaml` dynamic (per ep.md's "Dynamic AssetFunc" proposal) requires either (a) branching inside a custom `AssetFunc` on the filename `== "csidriver.yaml"` to inject dynamic `requiresRepublish`/`tokenRequests` bytes before/after `replaceNamespaceFunc`, or (b) calling `WithConditionalStaticResourcesController` a **second time** (the method explicitly supports multiple calls, "The func can be called multiple times to setup multiple controllers") with `csidriver.yaml` split into its own controller instance with a dedicated `AssetFunc`. Option (b) is architecturally cleaner and matches the existing one-controller-per-concern pattern already used for RBAC/SA/NetworkPolicy.
- **`resourceapply.ApplyDirectly` (used internally by `StaticResourceController.Sync`) already dispatches `storagev1.CSIDriver` objects to `resourceapply.ApplyCSIDriver`**, which already implements the exact hash-based recreate semantics ep.md describes ("Assume whole re-create is needed on any spec change... Delete()+Create()", `vendor/.../resourceapply/storage.go:138-189`, `SetSpecHashAnnotation`/`needsRecreate`). **No new recreate/hash logic needs to be written** — this is existing, reusable library-go behavior; only the desired `CSIDriver.spec` bytes need to be computed correctly by whatever `AssetFunc` produces `csidriver.yaml`'s content.
- **`opv1.CSIDriverConfigSpec` (vendored `openshift/api`) does NOT yet contain a `SecretsStore` field, and `CSIDriverType` does NOT yet include `"SecretsStore"` as an enum value** (`vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go:116-159`, pinned at `github.com/openshift/api v0.0.0-20260302174620-dcac36b908db`). This is the **critical blocking dependency** — see §11.

## 2. Directory Structure

```
cmd/secrets-store-csi-driver-operator/main.go   — cobra CLI wiring only (no business logic)
pkg/operator/starter.go                          — RunOperator: all controller composition (single file)
pkg/operator/starter_test.go                     — unit tests for getOperatorSyncState only
pkg/version/version.go                           — build version + Prometheus gauge
pkg/dependencymagnet/dependencymagnet.go         — build-tag-guarded import, DO NOT REMOVE, DO NOT put business logic here
assets/assets.go                                 — embed.FS + ReadFile(); //go:embed *.yaml rbac/*.yaml network-policy/*.yaml
assets/node.yaml                                 — DaemonSet (csi-driver, csi-node-driver-registrar, csi-liveness-probe containers)
assets/csidriver.yaml                            — static storage.k8s.io/v1 CSIDriver manifest — TARGET for dynamic AssetFunc
assets/node_sa.yaml, cabundle_cm.yaml            — ServiceAccount, trusted-CA ConfigMap
assets/rbac/*.yaml                               — 2 ClusterRoles + 2 ClusterRoleBindings (privileged SCC, secretproviderclasses)
assets/network-policy/*.yaml                     — metrics ingress NetworkPolicy
config/manifests/                                — OLM bundle: package.yaml, art.yaml, stable/ (CSV v5.0.0, image-references, CRDs, sample YAMLs)
docs/*.md                                        — security/performance/error-handling/testing guidelines (already read in full; summarized inline below)
hack/e2e.sh, hack/update-metadata.sh, hack/create-bundle — e2e test driver, OCP-version bump script, bundle builder
must-gather/gather                               — must-gather collection script (out of scope for this feature)
vendor/                                          — committed dependencies incl. openshift/api, library-go, client-go
```

No test directory beyond `pkg/operator/starter_test.go` exists in this repo — it is the **only** unit test file. There is no `pkg/operator/rotation_test.go` or similar yet (greenfield for this feature's unit tests).

## 3. Feature-Specific Analysis

### 3.1 Feature Presence on This Branch

**GREENFIELD.** Verified by direct inspection:
- `pkg/operator/starter.go` contains **no** reference to `SecretsStore`, `secretRotation`, `tokenRequests`, `requiresRepublish`, or `DriverConfig` anywhere.
- `assets/node.yaml` hardcodes `--enable-secret-rotation=true` and `--rotation-poll-interval=2m` as static string args (L45-46) — not templated, not read from any variable.
- `assets/csidriver.yaml` has no `requiresRepublish` or `tokenRequests` field.
- `vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go` has no `SecretsStore` type at all.

This confirms the User Story 1–4 acceptance scenarios in `specs.md` describe entirely new capability, not hardening of existing (partially working) code.

### 3.2 Current Hardcoded Defaults (baseline the feature must preserve for FR-003/FR-012)

| Setting | Current hardcoded value | Location |
|---|---|---|
| Rotation enabled | `true` | `assets/node.yaml:45` (`--enable-secret-rotation=true`) |
| Rotation poll interval | `2m` | `assets/node.yaml:46` (`--rotation-poll-interval=2m`) |
| `CSIDriver.spec.requiresRepublish` | unset (nil/absent) | `assets/csidriver.yaml` (no such field present) |
| `CSIDriver.spec.tokenRequests` | unset (nil/absent) | `assets/csidriver.yaml` (no such field present) |

FR-003 requires these exact values (`true`, `2m`) to remain the default when no `driverConfig.secretsStore` is set — this table is the ground truth to preserve.

### 3.3 Related Existing RBAC (verify before assuming new RBAC is needed)

`assets/rbac/secretproviderclasses_role.yaml` already grants the node-plugin ServiceAccount (`secrets-store-csi-driver-node-sa`) `create` on `serviceaccounts/token` (comment: `# for CSI driver token requests`). **This pre-dates this feature** and its exact purpose (bound-token minting by the driver itself, vs. kubelet-supplied CSI `tokenRequests`) is not fully clear from RBAC alone — the kubelet-driven `CSIDriverSpec.tokenRequests` mechanism ep.md describes does **not** require the driver to call the TokenRequest API itself (kubelet mints the token and passes it via `NodePublishVolume`'s `volume_context`). Flag as **UNVERIFIED** (§11.1): confirm during implementation whether this existing permission is already sufficient, unrelated, or needs a scope change — do not assume new RBAC is required without checking first, and do not assume this permission was granted for the new WIF feature either.

The CSV's `clusterPermissions` (`config/manifests/stable/secrets-store-csi-driver-operator.clusterserviceversion.yaml:140-160`) already grants the **operator's own** ServiceAccount `get/list/watch/update/patch` on `clustercsidrivers` and `clustercsidrivers/status` — sufficient for the operator to read the new `driverConfig.secretsStore` fields once they exist upstream; **no CSV RBAC change needed for the operator itself.**

## 4. Configuration & Extension Points

### 4.1 Configurable Fields Relevant to This Feature (pre- and post-feature)

| Field | Current | After this feature |
|---|---|---|
| `ClusterCSIDriver.spec.managementState` | Managed/Unmanaged/Removed | unchanged |
| `ClusterCSIDriver.spec.logLevel` | consumed by `LogLevelController` | unchanged |
| `ClusterCSIDriver.spec.driverConfig.driverType` | Enum: `""`, `AWS`, `Azure`, `GCP`, `IBMCloud`, `vSphere` | **must gain `SecretsStore`** (upstream `openshift/api` change, not in this repo) |
| `ClusterCSIDriver.spec.driverConfig.secretsStore.secretRotation` | does not exist | new: `type: None\|Custom`, `custom.minimumRefreshAge` (1–31560000s) |
| `ClusterCSIDriver.spec.driverConfig.secretsStore.tokenRequests` | does not exist | new: `type: Managed\|Unmanaged`, `managed.audiences[]` (audience + optional expirationSeconds 600–315360000) |

### 4.2 Hook / Pipeline Points

| # | Hook point | Type | Current registrations | Error behavior |
|---|---|---|---|---|
| 1 | `WithCSIDriverNodeService(..., optionalDaemonSetHooks...)` | `csidrivernodeservicecontroller.DaemonSetHookFunc` | 1: `WithCABundleDaemonSetHook` | Any hook error → `getDaemonSet` returns `fmt.Errorf("error running hook function (index=%d): %w", i, err)`; propagates to `sync()` → `WithSyncDegradedOnError(operatorClient)` sets the controller's `Degraded=true` condition. |
| 2 | `WithConditionalStaticResourcesController(..., manifests AssetFunc, files []string, shouldCreateFn, shouldDeleteFn)` | `resourceapply.AssetFunc` (`func(name string) ([]byte, error)`) | 1 shared func (`replaceNamespaceFunc`) across 8 files | `AssetFunc` errors surface via `resourceapply.ApplyDirectly`'s per-file `ApplyResult.Error`, aggregated into the `<name>Degraded` condition by `StaticResourceController.Sync`. |
| 3 | `WithCSIConfigObserverController` | n/a (fixed set: Infrastructure/Proxy/APIServer) | 1 | Not extensible without vendoring a new library-go observer — **not the right hook for driverConfig**, do not attempt to route rotation/WIF config through this controller. |

## 5. Reusable Assets (Anti-Duplication)

- `resourceapply.ApplyCSIDriver` (`vendor/github.com/openshift/library-go/pkg/operator/resource/resourceapply/storage.go:141`): already implements spec-hash-based delete+recreate for `storagev1.CSIDriver`. **Use this via the existing `StaticResourceController`/`ApplyDirectly` path — do not write custom CSIDriver create/update/delete logic.**
- `csidrivernodeservicecontroller.WithCABundleDaemonSetHook` (`vendor/.../csidrivernodeservicecontroller/helpers.go:32-75`): demonstrates the **exact pattern** to copy for a new rotation-args hook — a `DaemonSetHookFunc` factory function that closes over an informer/lister (here a `ConfigMapInformer`) rather than relying on the `opSpec` argument. **Model the new hook after this function's shape**, substituting a `ClusterCSIDriver` lister/getter for the `ConfigMapInformer`.
- `v1helpers.NewKubeInformersForNamespaces` / `configInformers.NewSharedInformerFactory` (already constructed in `starter.go` L45, L50): use the **existing** `operatorClient`/`dynamicInformers` (already returned by `goc.NewClusterScopedOperatorClientWithConfigName` at L55) as the source for any new `ClusterCSIDriver`-aware lister — do not create a second, redundant informer factory for `ClusterCSIDriver`.
- `getOperatorSyncState(operatorClient)` (`pkg/operator/starter.go:150-171`): existing helper to determine Managed/Unmanaged/Removed for conditional resource create/delete decisions — reuse this for any new conditional logic rather than re-deriving management state.
- `replaceNamespaceFunc(namespace)` (`pkg/operator/starter.go:131-139`): existing `${NAMESPACE}` substitution `AssetFunc` — if `csidriver.yaml` is split into its own controller call, its new dynamic `AssetFunc` should still call through to (or wrap) this function so namespace substitution is not duplicated or dropped.
- `docs/*.md` (`security-guidelines.md`, `performance-guidelines.md`, `error-handling-guidelines.md`, `testing-guidelines.md`): already comprehensive and specific to this repo's conventions (error wrapping with `%w`, `klog` usage, table-driven tests with `v1helpers.NewFakeOperatorClientWithObjectMeta`, no assertion libraries). **Follow these verbatim** — do not introduce new logging, testing, or error-handling conventions.

## 6. Architectural Guardrails

- **Structural**: Controllers are composed via `csicontrollerset` method chaining in `RunOperator` only — do not register controllers ad hoc elsewhere. `cmd/` contains zero business logic; keep it that way.
- **API / Schema**: This feature's core data types (`SecretsStoreCSIDriverConfigSpec`, `SecretsStoreSecretRotation`, `SecretsStoreTokenRequests`, discriminated unions, CEL immutability rule) live in `openshift/api`, an **external, separately-versioned repository this operator only vendors** — they cannot be added directly to this repo's `vendor/` tree by hand (vendor changes must track a real upstream `openshift/api` commit; hand-editing vendored generated code will be silently reverted by the next `go mod vendor`). See §11 for the resulting cross-repo sequencing constraint.
- **Build / Tooling**: Go 1.25 (`go.mod`), FIPS builds via `GOEXPERIMENT=strictfipsruntime` with `-tags strictfipsruntime,openssl` (Makefile L34-43) — any new code must build cleanly under both the FIPS and non-FIPS paths (avoid non-FIPS-approved crypto primitives; this feature does not need any crypto, so this is a low-risk guardrail here).
- **Deployment / Packaging**: New `${...}` template variables in `assets/node.yaml` or new fields in `assets/csidriver.yaml` do **not** need new environment-variable plumbing for rotation/tokenRequests — those are driven by `ClusterCSIDriver` reads inside hooks/AssetFuncs, not by operator-pod environment variables (unlike `${DRIVER_IMAGE}` etc., which come from `os.Getenv` in `csidrivernodeservicecontroller.replacePlaceholders`).
- **Code Generation**: There is **no** local codegen step in this repo for `ClusterCSIDriver` types (no `zz_generated.deepcopy.go` for operator-owned types here — those live in vendored `openshift/api`). The only local generated-adjacent artifact is the embedded assets (`//go:embed`), which is a build-time compile step, not a `make generate` step.
- **Security**: Node DaemonSet containers keep `privileged: true` for `csi-driver` and `csi-node-driver-registrar` only; `csi-liveness-probe` stays non-privileged. Do not add privileged escalation to any new component this feature might introduce (it should not need any — rotation/WIF config only affects existing container args and the CSIDriver object's fields, not pod security context).

## 7. Change Cascade Checklist

| When you change... | You must also... | Verify with... |
|---|---|---|
| `assets/node.yaml` (e.g., placeholder for rotation args) | Ensure `replacePlaceholders`/hook logic in the new `DaemonSetHookFunc` correctly finds and replaces the `--enable-secret-rotation=` / `--rotation-poll-interval=` args by prefix (there is no existing helper for "find arg by flag prefix and replace" in this repo — must be written new, following the string-arg convention already used in `node.yaml`) | `make test-unit` (add a new `_test.go` for the hook); manually inspect rendered DaemonSet YAML in a test |
| `assets/csidriver.yaml` (making it dynamic) | If splitting into a second `WithConditionalStaticResourcesController` call, update `starter.go`'s file list (remove `"csidriver.yaml"` from the shared list, add a new call with the dynamic `AssetFunc` and `["csidriver.yaml"]`) | `make test-unit`; confirm `assets/assets.go`'s `//go:embed` still covers `csidriver.yaml` (it does, top-level `*.yaml` glob — no embed change needed) |
| `vendor/github.com/openshift/api` (bump for `SecretsStore` types) | Run `go mod tidy && go mod vendor`; regenerate/verify `client-go` apply-configurations (`applyoperatorv1.ExtractClusterCSIDriver` etc. used in `extractOperatorSpec`/`extractOperatorStatus`, `pkg/operator/starter.go:173-201`) still compile against the new types | `make verify` (runs `verify-deps` from `build-machinery-go`), `go build ./...` |
| Any new operator Go logic in `pkg/operator/` | Add a matching `_test.go` in the same package using `v1helpers.NewFakeOperatorClientWithObjectMeta`, table-driven, following `starter_test.go`'s exact shape | `make test-unit` |
| Any new/changed static asset under `assets/` (new file/subdirectory) | Update the `//go:embed` directive in `assets/assets.go` if a new subdirectory is introduced (not needed for edits to existing `assets/*.yaml` or `assets/rbac|network-policy/*.yaml`) | `make build` (embed errors are compile-time panics if the glob misses a path) |
| Any Go source formatting | — | `make update-gofmt` then `make verify` |
| OCP version / CSV bump (not expected for this feature, but do not confuse with it) | `./hack/update-metadata.sh X.Y` | `make verify` |

**No `make generate`, `make manifests`, or `make bundle` targets exist in this repo's own `Makefile`** (those exist in `openshift/api` for CRD generation, and in `hack/create-bundle` only for building the OLM bundle image — not for regenerating types). Do not assume kubebuilder-style codegen targets exist here.

## 8. Test & CI Reference

### 8.1 Test Structure

- Only tier present: Go unit tests co-located with source (`pkg/operator/starter_test.go` next to `pkg/operator/starter.go`), using stdlib `testing` (table-driven, `t.Run`), plus `library-go`'s `v1helpers.NewFakeOperatorClientWithObjectMeta` fake.
- No `testify`/`gomega`/`ginkgo` anywhere in `go.mod` — do not introduce them.
- E2E tests are **not** Go tests — they are a single bash script, `hack/e2e.sh`, run against a live cluster with `oc`.

### 8.2 How to Run Tests Locally

```bash
make test-unit          # go test ./pkg/... ./cmd/...  (GO_TEST_PACKAGES in Makefile)
make verify             # go vet, gofmt check, go-version consistency
make check              # verify then test-unit (run this before every PR)
make update-gofmt       # auto-fix formatting violations found by verify
```

`make test-e2e` (`hack/e2e.sh`) requires a live OpenShift cluster with the operator, driver, and `csi-secrets-store-e2e-provider` pods already deployed, plus `oc` in `$PATH` and a valid `$KUBECONFIG` — **not runnable in this sandboxed session** and not expected to run locally per this repo's own conventions.

### 8.3 CI Pipeline

- CI config lives externally in `openshift/release` (Prow), not in this repo.
- Every PR runs `make verify` and `make test-unit` (per `AGENTS.md`/`docs/testing-guidelines.md`).
- CI builds are FIPS-compliant (`CGO_ENABLED=1 GOEXPERIMENT=strictfipsruntime -tags strictfipsruntime,openssl`).
- `.ci-operator.yaml` build root: `openshift/release:rhel-9-release-golang-1.26-openshift-5.0`.

### 8.4 Test Coverage Gaps

- **Zero unit test coverage today for anything except `getOperatorSyncState`.** There are no existing tests for `replaceNamespaceFunc`, `extractOperatorSpec`/`extractOperatorStatus`, or any DaemonSet/CSIDriver rendering logic — this feature's new hook/AssetFunc code will be the **first** tests of this kind in the repo; there is no existing pattern file to copy beyond the generic table-driven shape in `starter_test.go`.
- E2E is the **only** coverage for the actual DaemonSet/CSIDriver reconciliation behavior end-to-end; ep.md's proposed E2E scenarios (rotation toggle, WIF audiences, upgrade preservation) have zero existing scaffolding in `hack/e2e.sh` today — `hack/e2e.sh` only tests basic secret-mount functionality via a `SecretProviderClass` and a test pod, with no rotation- or token-audience-specific assertions.

## 9. Developer Workflow

### 9.1 Key Commands Reference

| Command | Purpose |
|---|---|
| `make build` (alias `make`/`all`) | Build the operator binary |
| `make test-unit` | Run unit tests |
| `make verify` | `go vet`, `gofmt`, Go version consistency |
| `make check` | `verify` then `test-unit` — **run before every PR** |
| `make test-e2e` | Run `hack/e2e.sh` (requires live cluster) |
| `make update-gofmt` | Auto-fix formatting |
| `make metadata VERSION=X.Y.Z` | Bump OCP version across CSV/package/Makefile/README (not needed for this feature) |
| `make clean` | Remove binary and yq tool |

### 9.2 Version Variables

- `go.mod`: `go 1.25.0`.
- `openshift/api`, `client-go`, `library-go` are pinned to specific pseudo-versions (`go.mod` L6-9) — bumping to pick up new `SecretsStore` API types means bumping `github.com/openshift/api` (and possibly `client-go` for new apply-configuration types) to a newer commit, then `go mod tidy && go mod vendor`.
- CSV/package version `5.0.0` — not something this feature should change.

### 9.3 Local Development Setup

- Requires Go 1.25+ matching `go.mod`; `make` (GNU Make); `yq` (auto-downloaded to `bin/yq` by the vendored `build-machinery-go` `yq.mk` target, `YQ_VERSION = v4.47.1`).
- No `.env` file or local secrets needed to build/unit-test.
- FIPS builds require `GOEXPERIMENT=strictfipsruntime`-capable Go; falls back to a warning + non-FIPS build otherwise (fine for local dev, not for CI/production).

### 9.4 How to Add a New DaemonSet Hook (walkthrough, following `WithCABundleDaemonSetHook`)

1. Write a new hook factory function, e.g. in a new file `pkg/operator/rotation_hook.go`, with signature `func(...) csidrivernodeservicecontroller.DaemonSetHookFunc`, following the exact shape of `csidrivernodeservicecontroller.WithCABundleDaemonSetHook` (`vendor/.../csidrivernodeservicecontroller/helpers.go:32`): close over whatever lister/getter is needed (here, a way to read the live `ClusterCSIDriver`'s `driverConfig.secretsStore.secretRotation`), and return a closure of type `func(*opv1.OperatorSpec, *appsv1.DaemonSet) error` that mutates `daemonSet.Spec.Template.Spec.Containers[...].Args` in place.
2. Find the `csi-driver` container by name (`node.yaml`'s container is literally named `csi-driver`) and replace the `--enable-secret-rotation=` / `--rotation-poll-interval=` argument strings by prefix match (no existing helper for this — write a small local `setArg(args []string, prefix, value string) []string` utility and unit-test it directly).
3. Register the new hook in `starter.go`'s `WithCSIDriverNodeService(...)` call as an additional variadic `optionalDaemonSetHooks` argument, alongside the existing `WithCABundleDaemonSetHook`.
4. If the hook needs to react to `ClusterCSIDriver` changes promptly (not just the controller's 1-minute resync), add the appropriate informer to the `optionalInformers` slice (currently `nil` at `starter.go:110`) — see §1.3 trap above.
5. Add a table-driven `_test.go` following `starter_test.go`'s exact shape (`v1helpers.NewFakeOperatorClientWithObjectMeta`, `t.Run`, `t.Fatalf`).

*(No equivalent "how to add a dynamic AssetFunc" example exists yet in this repo — the closest analog is copying the `replaceNamespaceFunc` shape and layering CSIDriver-specific field injection on top, as described in §1.3.)*

## 10. Platform & Environment Integration

### 10.1 Security Context & Permissions

- Node DaemonSet runs under the `privileged` SCC (bound via `assets/rbac/node_privileged_binding.yaml` + `privileged_role.yaml`) — required for CSI host-mount operations, unrelated to this feature and must not be touched.
- No PodSecurityStandards conflicts expected — this feature changes container **args** and a CRD **spec** field, not pod security context.

### 10.2 Proxy & Network Configuration

- CA bundle injection via `assets/cabundle_cm.yaml` (`config.openshift.io/inject-trusted-cabundle: "true"`) + `WithCABundleDaemonSetHook` — unrelated to this feature; do not disturb this existing hook when adding the new rotation hook to the same `optionalDaemonSetHooks` variadic list.
- `NetworkPolicy` (`assets/network-policy/allow-ingress-to-metrics-operand.yaml`) only opens metrics port 8095 — unaffected by this feature.

### 10.3 Cloud Provider Integration

- **This is the core of the WIF half of the feature.** Today, this operator has **zero** cloud-provider credential integration code (no CCO `CredentialsRequest`, no cloud SDK imports in `go.mod`). The entire WIF mechanism this feature implements is: the operator sets `CSIDriver.spec.tokenRequests` (audience + expirationSeconds); **kubelet** — not this operator, not the driver — mints the bound service-account token and passes it to the driver via `NodePublishVolume`'s `volume_context`; the **driver** (upstream `secrets-store-csi-driver`, a separate repository/binary, only *deployed* by this operator) uses that token to authenticate to the cloud provider. This operator's WIF responsibility is narrowly: **surface `tokenRequests` configuration onto the `CSIDriver` object** — it does not itself talk to AWS STS/Azure AD/GCP IAM.
- Explicitly **not supported** (per ep.md Non-Goals and `specs.md` A-002/A-003, confirmed by absence of any provider SDK in `go.mod`): automatic cloud-provider detection, and any provider-specific plugin logic (Vault/AWS Secrets Manager/Azure Key Vault are separate provider plugins entirely outside this repo).

### 10.4 Build & Compliance Constraints

- FIPS: `GOEXPERIMENT=strictfipsruntime`, tags `strictfipsruntime,openssl` — this feature introduces no new crypto, so no FIPS-specific risk.
- Multi-arch: not specially configured in this repo's own `Makefile` beyond the standard OCP build pipeline; out of scope for this feature.
- Disconnected/air-gapped: images use `${...}` variable substitution (`assets/node.yaml`) resolved from the release payload — unaffected by this feature.

### 10.5 Console / UI Integration

- No console plugin, quickstart, or CLI tooling in this repo beyond the sample `SecretProviderClass` YAMLs in `config/manifests/stable/sscsi-sample-*.yaml`. Not applicable to this feature unless a new sample YAML demonstrating rotation/WIF config is desired (optional, not required by any FR in `specs.md`).

### 10.6 Packaging & Lifecycle

- OLM: single `stable` channel, CSV `v5.0.0`, `olm.skipRange: ">=4.13.0-0 <5.0.0"`. This feature does **not** require a CSV version bump by itself (it's a feature within the 5.0 line, gated by the CRD schema from `openshift/api`, not by OLM channel/version metadata) — do not conflate this feature's delivery with `./hack/update-metadata.sh`.
- CRD ownership: `ClusterCSIDriver`'s CRD is **not** vendored/owned by this repo's `config/manifests/` (no `clustercsidrivers.crd.yaml` present here) — it is defined and shipped by `openshift/api`/the cluster-config-operator, confirming again that the schema change is an **external** dependency (see §11).

## 11. Risks & Downstream Impacts

* **CRITICAL — Upstream API dependency blocks all implementation.** The vendored `openshift/api` at `v0.0.0-20260302174620-dcac36b908db` has no `SecretsStore` driver type. Every FR in `specs.md` (FR-001 through FR-012) ultimately depends on `ClusterCSIDriver.Spec.DriverConfig.SecretsStore` existing as a typed field. **This repo alone cannot deliver this feature.** Impact: blocks the entire implementation until `openshift/api` merges the type extension (per ep.md §"API Extensions") and this repo bumps `go.mod`/`vendor/github.com/openshift/api` to a commit that includes it. Mitigation: sequence `tasks.md` so the very first task is "vendor the new `openshift/api` types" (assume it will be available as a dependency — do not attempt to hand-write the union/CEL rules into `vendor/`, which would be silently reverted on the next `go mod vendor`), and flag this dependency explicitly to reviewers/PM before estimating timelines.
* **DaemonSet hook informer wiring risk.** If the new rotation hook is added without also adding a `ClusterCSIDriver`-derived informer to `optionalInformers` (currently `nil`), the DaemonSet will still eventually pick up config changes via the controller's 1-minute `ResyncEvery`, but changes will not be event-driven/immediate — this could look like a bug ("I changed the CR and nothing happened for up to a minute") during manual testing. Impact: minor UX/testing confusion, not a correctness bug. Mitigation: wire the informer; document the resync cadence in the PR description if not wired.
* **Single shared `AssetFunc` for 8 static files.** If `csidriver.yaml`'s dynamic behavior is implemented by branching inside `replaceNamespaceFunc` (Option (a) in §1.3) rather than splitting into a second controller call (Option (b)), every other static file's rendering path now runs through a function with an added conditional — a subtle bug in that branch could silently corrupt RBAC/SA/ConfigMap/NetworkPolicy rendering for unrelated resources. Impact: broad blast radius for a narrow change. Mitigation: strongly prefer Option (b) (separate `WithConditionalStaticResourcesController` call scoped to `["csidriver.yaml"]` only).
* **`tokenRequests.type: Managed` immutability (per ep.md/specs.md) has no enforcement code in this repo.** The CEL rule enforcing "cannot revert from Managed" lives in the **CRD schema** (`openshift/api`), not in this operator. This operator must not attempt to re-implement that validation in Go — it should only read the (already-validated) `ClusterCSIDriver` object. Impact: if a future contributor mistakenly adds redundant validation here, it will drift from the CRD's CEL rule. Mitigation: document in code comments that immutability is enforced upstream, not in this operator.
* **Downgrade behavior is genuinely undefined** (carried forward from `validation.json` and `specs.md`'s one `[NEEDS CLARIFICATION]` marker) — repo evidence does not resolve this either; there is no existing downgrade-handling code pattern anywhere in this operator to model from. Impact: unknown until product/API owners decide. Mitigation: do not invent behavior in `plan.md`/`tasks.md`; carry the open question forward or get an explicit decision before implementation tasks are finalized.

### 11.1 Assessment Limitations / UNVERIFIED Items

* The exact purpose of the pre-existing `serviceaccounts/token: create` RBAC grant (`assets/rbac/secretproviderclasses_role.yaml`, commented "for CSI driver token requests") relative to the new WIF `tokenRequests` feature is **UNVERIFIED** — it may be for an unrelated, pre-existing driver capability (e.g., the driver's own bound-token minting for a different purpose) rather than a precondition for this feature's kubelet-driven `tokenRequests` mechanism. Verify by reading the upstream `secrets-store-csi-driver` binary's use of this permission (out of this repo) before assuming it is or is not sufficient.
* `docs/*.md` guideline files describe conventions accurately as cross-checked against `pkg/operator/starter.go` and `assets/node.yaml`, but were not independently verified against **every** historical commit — treat them as authoritative for *current* conventions (they matched observed code in all spot checks performed).
* Whether `client-go`'s `applyoperatorv1.ExtractClusterCSIDriver`/`ExtractClusterCSIDriverStatus` (used in `extractOperatorSpec`/`extractOperatorStatus`, `pkg/operator/starter.go:173-201`) will need regeneration/changes when `openshift/api` gains the `SecretsStore` type is **UNVERIFIED** without seeing the actual upstream API PR — flag as a task-planning risk, not a certainty.
* No CRD YAML for `ClusterCSIDriver` exists in this repo to inspect the exact current OpenAPI schema/CEL rules as installed on a live 5.0 cluster — schema assertions in §1.3/§11 are based on the vendored Go types only, which is the correct source of truth for **this repo's compile-time contract**, but the CRD's cluster-installed schema is ultimately determined by `openshift/api`+CVO at a given cluster version.

## 12. Quick Reference Card

### Preflight Checklist (run before every PR)

```
1. make verify
2. make test-unit
   (equivalently: make check)
3. make update-gofmt   # only if verify reports formatting issues
```

### Key File Quick-Nav

| I want to... | Look at... |
|---|---|
| Add rotation/tokenRequests reading logic | New file(s) under `pkg/operator/` (e.g. `rotation.go`, `wif.go`) — no existing file to extend, this is new |
| Add a new DaemonSet arg hook | `pkg/operator/starter.go`'s `WithCSIDriverNodeService(...)` call; model on `vendor/.../csidrivernodeservicecontroller/helpers.go`'s `WithCABundleDaemonSetHook` |
| Make `CSIDriver` object dynamic | `pkg/operator/starter.go`'s `WithConditionalStaticResourcesController(...)` call + `assets/csidriver.yaml`; consider splitting into a second controller call |
| Change RBAC | `assets/rbac/*.yaml` (NOT the generated CSV `clusterPermissions` block, which should be kept in sync but is not the source of truth) |
| Add a unit test | `pkg/operator/starter_test.go`'s exact pattern (table-driven, `v1helpers.NewFakeOperatorClientWithObjectMeta`) |
| Bump vendored `openshift/api` for new types | `go.mod` L6, then `go mod tidy && go mod vendor`, then `make verify` |
| Understand upstream `requiresRepublish`/rotation semantics | `openspec/inputs/ep.md` §"Upstream `requiresRepublish` Mechanism" (not in Go source — this is upstream driver behavior, not this operator's code) |
