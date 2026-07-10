# Repository Assessment Report
**Feature:** Configurable Secret Rotation and Workload Identity Federation

## 0. Inputs & Tooling
- `repo`: `/Users/ckyal/go/src/github.com/chiragkyal/secrets-store-csi-driver-operator`
- `branch`: `openspec-cursor-agent-gpt5-4`
- `commit`: `60ee14a2c706e7d09cdf4bee480bff73ab619719`
- `tooling_status`: OK
- Spec status: `PASS`. Approved upstream planning artifacts available at `openspec/changes/csi-secrets-store-rotation-and-wif/specs.md` and `validation.json`.
- Working mode: working-folder mode is active in `openspec/changes/csi-secrets-store-rotation-and-wif/inputs/jira.yaml`, pinned to the current checkout via `working_folder_path`.
- Feature status on this pinned branch: **NOT implemented in repo-local Go code yet; greenfield implementation is required in this branch.** Evidence: `pkg/operator/starter.go` only wires static `csidriver.yaml` and hardcoded `node.yaml` rotation flags, and `pkg/operator/*.go` contains no `driverConfig`/`DriverConfig` consumers.

## 1. Architecture Overview
*High-level architectural map so the Planning Agent understands the system before proposing any changes.*

### 1.1 Project Type & Tech Stack
- Project type: OpenShift/Kubernetes operator built on `library-go` CSI controller composition, not `controller-runtime`. Evidence: `pkg/operator/starter.go` uses `csicontrollerset.NewCSIControllerSet(...)`.
- Language/runtime: Go `1.25.0`. Evidence: `go.mod`.
- Core dependencies:
  - `github.com/openshift/library-go v0.0.0-20260303171201-5d9eb6295ff6` for controller composition, static resource application, and node service orchestration. Evidence: `go.mod`.
  - `github.com/openshift/api v0.0.0-20260302174620-dcac36b908db` for `ClusterCSIDriver` types. Evidence: `go.mod`.
  - `github.com/openshift/client-go v0.0.0-20260302182750-20813ce71ca6` for operator/config clients and applyconfigurations. Evidence: `go.mod`.
  - `k8s.io/client-go v0.35.2` and `k8s.io/apimachinery v0.35.2` for Kubernetes client plumbing. Evidence: `go.mod`.
- Build system: GNU Make layered on `build-machinery-go`. Evidence: `Makefile` includes vendored `golang.mk`, `deps-gomod.mk`, `images.mk`, and `yq.mk`.
- Packaging: OLM bundle + OpenShift image stream + multi-stage operator image build. Evidence: `config/manifests/stable/`, `hack/create-bundle`, `Dockerfile.openshift`, and `config/manifests/stable/image-references`.

### 1.2 Component Map
- `cmd/secrets-store-csi-driver-operator/`: CLI/bootstrap entrypoint only; dispatches to `operator.RunOperator`. Evidence: `main.go`.
- `pkg/operator/`: hand-written controller wiring and sync-state helpers; this is the primary implementation surface for the feature. Evidence: `starter.go`, `starter_test.go`.
- `assets/`: embedded, hand-authored operand manifests for the DaemonSet, ServiceAccount, CSIDriver, CA bundle, RBAC, and NetworkPolicy. Evidence: `assets/assets.go`, `assets/node.yaml`, `assets/csidriver.yaml`.
- `hack/`: hand-written developer/release scripts including `e2e.sh`, `update-metadata.sh`, and `create-bundle`. Evidence: `hack/e2e.sh`, `hack/create-bundle`, `README.md`.
- `config/manifests/stable/`: generated/packaging-oriented OLM and sample manifests. Treat as packaging outputs first, not primary implementation entrypoints. Evidence: stable CSV and sample SecretProviderClass YAMLs.
- `vendor/`: generated vendored dependencies; never edit manually. Evidence: repo layout plus `Makefile`/`go.mod` vendoring conventions.
- Dependency flow: CLI bootstrap -> `RunOperator()` -> controller set wiring -> embedded assets/hooks -> library-go reconciliation. Feature work must flow through `pkg/operator/` first, not directly through CSV or sample manifests.

### 1.3 Framework & Pattern Architecture
- Primary framework: single `library-go` `CSIControllerSet` chain. Evidence: `pkg/operator/starter.go` wires `WithLogLevelController()`, `WithManagementStateController(...)`, `WithConditionalStaticResourcesController(...)`, `WithCSIConfigObserverController(...)`, and `WithCSIDriverNodeService(...)`.
- No mixed framework architecture was found in repo-local code. There is no `controller-runtime` manager or reconciler registration in `cmd/` or `pkg/`.
- Entry/bootstrap sequence:
  - `cmd/.../main.go` builds the Cobra root command.
  - `controllercmd.NewControllerCommandConfig(..., operator.RunOperator, ...)` registers the `start` subcommand.
  - `RunOperator()` constructs clients/informers, then assembles and runs the `CSIControllerSet`. Evidence: `cmd/.../main.go`, `pkg/operator/starter.go`.
- Dead-code / do-not-edit traps:
  - `config/manifests/stable/*.yaml` are packaging outputs and examples, not the first place to implement runtime behavior. Runtime reconciliation comes from `assets/` + `pkg/operator/starter.go`.
  - `vendor/**` is generated; dependency changes must happen via `go.mod` and `go mod vendor`.
  - The feature is absent on this branch; do not assume `driverConfig.secretsStore` support exists locally just because the change proposal describes it.

### 1.4 Runtime Data/Control Flow
- Operator startup:
  1. `RunOperator()` builds Kubernetes and config clients and namespace-scoped informers. Errors here return immediately and abort startup. Evidence: `pkg/operator/starter.go`.
  2. It creates a cluster-scoped generic operator client for `ClusterCSIDriver`. Errors here also abort startup. Evidence: `goc.NewClusterScopedOperatorClientWithConfigName(...)` in `starter.go`.
  3. It composes the controller set and starts informers, then runs the set. Evidence: `starter.go`.
- Current reconcile path for feature-relevant resources:
  - `ClusterCSIDriver` changes feed the generic operator client.
  - `WithManagementStateController` and `getOperatorSyncState()` determine whether resources are created, skipped, or deleted.
  - `WithConditionalStaticResourcesController` applies the embedded YAML list using `replaceNamespaceFunc(...)`; the current `CSIDriver` object is still static.
  - `WithCSIDriverNodeService` reads `assets/node.yaml`, applies namespace substitution, and applies the CA-bundle DaemonSet hook.
- User action to runtime effect today:
  - Management-state changes affect whether assets are synced or removed.
  - Log-level changes are handled by `WithLogLevelController()`.
  - Secret rotation and workload identity settings do **not** flow through the runtime today because no repo-local code consumes `DriverConfig`.

## 2. Target Files (Modification & Creation)
*Files the Planner will actively need to change or create for the specified feature.*

### Operator Wiring
- `pkg/operator/starter.go`: Primary implementation file for this feature. It currently hardcodes `csidriver.yaml` in the static-resource list and wires `WithCSIDriverNodeService(..., nil, WithCABundleDaemonSetHook(...))`; this is where a dynamic asset function, optional informers, and a new DaemonSet hook must be introduced. Evidence: `starter.go`. (confidence: high)
- `pkg/operator/starter_test.go`: Existing test pattern for table-driven operator behavior; likely needs companion tests or targeted expansion for sync-state-adjacent behavior. Evidence: `starter_test.go`. (confidence: high)
- `pkg/operator/secrets_store_config.go`: (New) Best place for pure helper logic to derive rotation behavior, map managed/unmanaged token request behavior, and mutate DaemonSet args without bloating `starter.go`. Evidence: current repo has only `starter.go`; the feature needs new helper logic not present today. (confidence: high)
- `pkg/operator/secrets_store_config_test.go`: (New) Table-driven tests for config extraction, token-request preservation/ownership, and arg replacement behavior. Evidence: testing guidance favors `_test.go` beside source with table-driven subtests and `library-go` fakes. (confidence: high)

### Embedded Assets
- `assets/csidriver.yaml`: Keep as the base manifest for the managed `CSIDriver`, but treat it as the static seed for a dynamic asset function rather than the final source of truth for `requiresRepublish` and `tokenRequests`. Evidence: current file has no `requiresRepublish` or `tokenRequests`. (confidence: high)
- `assets/node.yaml`: Candidate for adjusting or documenting ownership of `--enable-secret-rotation` and `--rotation-poll-interval`; currently both are hardcoded. Evidence: `node.yaml` lines with those args. (confidence: high)
- `assets/assets.go`: Must change only if new asset subdirectories are added. Evidence: `//go:embed *.yaml rbac/*.yaml network-policy/*.yaml`. (confidence: medium)
- `assets/rbac/secretproviderclasses_role.yaml`: Read-only baseline during initial implementation; the existing role already grants `serviceaccounts/token` create and `csidrivers` read access relevant to rotation/WIF. Only edit if final implementation reveals a permission gap. Evidence: role YAML. (confidence: medium)

### Tests / Verification
- `hack/e2e.sh`: Primary place to extend e2e coverage for rotation defaults/customization and token-request behavior in this repo’s current testing style. Evidence: only repo-local e2e harness is here. (confidence: high)

### Dependency / Packaging
- `go.mod`: Likely needs a dependency bump once the upstream `openshift/api` change introducing the new Secrets Store config surface lands. Evidence: current vendored API supports `ClusterCSIDriver` but repo-local code shows no `DriverConfig` usage. (confidence: high)
- `vendor/`: Generated follow-on update after `go.mod` changes via `go mod vendor`; do not hand-edit. Evidence: vendoring guidance in `AGENTS.md` and `Makefile`. (confidence: high)
- `config/manifests/stable/secrets-store-csi-driver-operator.clusterserviceversion.yaml`: Packaging follow-up only if new runtime permissions or related images are required. Evidence: CSV already grants `csidrivers` and `serviceaccounts/token`. (confidence: medium)

## 3. Reference Context (Read-Only)
*Files the Planner must read to understand existing interfaces, patterns, and constraints.*

### 3.1 Entry Points & Wiring
- `cmd/secrets-store-csi-driver-operator/main.go`: CLI entry and `start` command wiring.
- `pkg/operator/starter.go`: Full controller composition, informer startup, static-resource list, and node-service hook wiring.
- `README.md`: Local dev entry command, required env vars, and quick-start `ClusterCSIDriver` sample.

### 3.2 API / Interface Patterns
- `pkg/operator/starter.go`: `extractOperatorSpec()` and `extractOperatorStatus()` show how this operator currently narrows `ClusterCSIDriver` down to generic `OperatorSpec` / `OperatorStatus`.
- `assets/csidriver.yaml`: Current static operand shape for the `CSIDriver`.
- `assets/node.yaml`: Current driver and sidecar flags, mounts, metrics, and health behavior.
- `assets/rbac/secretproviderclasses_role.yaml`: Existing rotation/token-minting permissions model.

### 3.3 Build, CI & Tooling
- `Makefile`: canonical targets, FIPS build behavior, and test package scopes.
- `go.mod`: exact dependency versions and Go toolchain target.
- `Dockerfile.openshift`: operator image build path (`RUN make`) and runtime image layout.

### 3.4 Manifest / Config Generation Pipelines
- `assets/assets.go`: embed boundary for runtime-managed YAML.
- `hack/create-bundle`: packaging flow from `config/` into bundle/index images.
- `config/manifests/stable/image-references`: release payload image mapping.
- `config/manifests/stable/secrets-store-csi-driver-operator.clusterserviceversion.yaml`: OLM packaging/RBAC layer, not first-edit runtime source.

### 3.5 Test Patterns & Fixtures
- `pkg/operator/starter_test.go`: existing table-driven unit-test style.
- `docs/testing-guidelines.md`: approved testing conventions for this repo.
- `hack/e2e.sh`: e2e flow and cluster-side setup/teardown pattern.

## 4. Configuration Surface & Runtime Behavior
*What is configurable today, and how the runtime processes configuration.*

### 4.1 Current Configuration Surface

#### Observed runtime/operator inputs in this repo

| Surface | Source | Current behavior in this branch | Constraints / evidence |
|---|---|---|---|
| `spec.managementState` | `ClusterCSIDriver` via generic operator client | Controls whether static resources are synced, skipped, or removed via `getOperatorSyncState()` | Evidence: `pkg/operator/starter.go`, `pkg/operator/starter_test.go` |
| `spec.logLevel` / `spec.operatorLogLevel` | `ClusterCSIDriver` + library-go log-level controller | Log level is handled by `WithLogLevelController()` and used for `${LOG_LEVEL}` substitution in the operand | Evidence: `starter.go`, `README.md`, `assets/node.yaml` |
| `spec.driverConfig` | `ClusterCSIDriver` upstream type surface | **Not consumed in repo-local code today** | Evidence: no `DriverConfig`/`driverConfig` matches in `pkg/operator/*.go` |
| `DRIVER_IMAGE`, `NODE_DRIVER_REGISTRAR_IMAGE`, `LIVENESS_PROBE_IMAGE` | Environment variables in local/dev or OLM injection path | Populate placeholders in `assets/node.yaml` | Evidence: `README.md`, `assets/node.yaml`, `config/manifests/stable/image-references` |
| `${NAMESPACE}` placeholders | asset substitution | Replaced by `replaceNamespaceFunc()` before static resource / DaemonSet application | Evidence: `starter.go`, `assets/node.yaml` |

#### Observed hardcoded feature-relevant defaults

| Behavior | Current value | Where set | Implication for this feature |
|---|---|---|---|
| Secret rotation enabled | `true` | `assets/node.yaml` | Must move under hook-driven config ownership |
| Rotation poll interval | `2m` | `assets/node.yaml` | Must become configurable without breaking upgrade defaults |
| `CSIDriver.requiresRepublish` | absent | `assets/csidriver.yaml` | Must become dynamically derived |
| `CSIDriver.tokenRequests` | absent | `assets/csidriver.yaml` | Must become dynamically derived/preserved |

### 4.2 Reconciliation / Processing Flow (Detailed)

| Step | Flow / hook | Evidence | Error behavior |
|---|---|---|---|
| 1 | Build kube/config clients and namespace-scoped informers | `pkg/operator/starter.go` | Client-construction failures return from `RunOperator()` and stop startup |
| 2 | Build cluster-scoped generic operator client for `ClusterCSIDriver` | `starter.go` | Failure returns from `RunOperator()` and stops startup |
| 3 | Build `CSIControllerSet` with log-level + management-state controllers | `starter.go` | No custom repo-local error recovery; runtime delegated to library-go controllers |
| 4 | `getOperatorSyncState()` reads `OperatorSpec.ManagementState` and object metadata | `starter.go`, `starter_test.go` | Read errors log with `klog.Errorf(...)` and degrade to `Unmanaged` to avoid unsafe sync |
| 5 | Conditional static resources apply `node_sa.yaml`, `csidriver.yaml`, CA bundle, RBAC, and NetworkPolicy only when sync state is `Managed` | `starter.go` | Unknown state prevents sync; `replaceNamespaceFunc()` panics if an embedded asset is missing |
| 6 | Config observer controller watches cluster config | `starter.go` | Repo-local code does not customize its error behavior |
| 7 | Node-service controller reads `assets/node.yaml`, substitutes namespace, and applies the CA bundle DaemonSet hook | `starter.go`, `assets/node.yaml` | Repo-local code currently passes no extra optional informers and no extra custom hooks beyond CA bundle |
| 8 | Informers start, then `csiControllerSet.Run(ctx, 1)` starts controllers | `starter.go` | Runtime reconciliation errors are handled inside vendored library-go controllers; repo-local code does not override retry policy |

Numbered hook/pipeline view for feature planning:
1. Bootstrap CLI in `cmd/.../main.go`.
2. Enter `RunOperator()`.
3. Create clients/informers.
4. Build generic operator client.
5. Compose controller chain in this exact order: log level -> management state -> conditional static resources -> config observer -> node service.
6. Start informers.
7. Run controller set.
8. During sync, management state gates resource creation/deletion; node service receives the CA bundle hook before DaemonSet apply.

### 4.3 Image / Dependency Resolution
- Operand images are placeholder-driven, not hardcoded in Go. Evidence: `${DRIVER_IMAGE}`, `${NODE_DRIVER_REGISTRAR_IMAGE}`, `${LIVENESS_PROBE_IMAGE}` in `assets/node.yaml`.
- Local dev path expects those env vars to be exported before `./secrets-store-csi-driver-operator start ...`. Evidence: `README.md`.
- Release payload mapping comes from `config/manifests/stable/image-references`, which maps the operator, driver, registrar, and liveness-probe images.
- Bundle/index packaging still does image replacement via `hack/create-bundle`, which rewrites the stable CSV before bundle build/push. Treat that as packaging flow, not runtime resolution.
- External dependency prerequisite for this feature: an upstream `openshift/api` change introducing the new Secrets Store config fields must land before repo-local code can compile against them cleanly.

### 4.4 Status / Health Reporting
- Repo-local status integration uses `extractOperatorStatus()` to expose generic `OperatorStatus` to library-go. Evidence: `pkg/operator/starter.go`.
- Repo-local custom condition-setting logic was **not** found; this operator relies heavily on library-go controllers for status handling.
- Repo-local health surfaces on the operand:
  - Metrics at `:8095`
  - Driver health via liveness endpoint on port `9808`
  - Node-service CA bundle injection via the DaemonSet hook
  Evidence: `assets/node.yaml`, `docs/performance-guidelines.md`, `docs/security-guidelines.md`.
- Planning implication: if the feature needs new degraded/reporting behavior, first verify whether library-go’s existing status pathways are sufficient before adding new repo-local status code.

### 4.5 Feature Gate / Feature Flag Mechanism
- No repo-local feature-gate mechanism was found in the source files read for this assessment.
- The feature therefore appears to be an ordinary operator/API rollout, not a runtime gate checked in repo-local code.
- If release gating is needed, it will likely happen through API availability, OLM/release packaging, or external product process rather than a repo-local feature-set switch. This remains partially unverified because no additional feature-gate files were opened.

## 5. Reusable Assets (Anti-Duplication)
*Existing functions, components, or utilities the Planner MUST use instead of reimplementing.*

- `pkg/operator/starter.go`: Use `replaceNamespaceFunc()` for asset namespace substitution instead of introducing parallel templating. Evidence: it already wraps `assets.ReadFile()` and performs `${NAMESPACE}` replacement.
- `pkg/operator/starter.go`: Use `getOperatorSyncState()` as the gate for all conditional resource behavior instead of duplicating management-state logic elsewhere. Evidence: both create/delete predicates in `WithConditionalStaticResourcesController(...)` delegate to it.
- `assets/assets.go`: Use `assets.ReadFile()` as the single embedded-asset loader for any dynamic `CSIDriver` asset wrapper. Evidence: current static-resource and namespace-substitution flow already depends on it.
- `github.com/openshift/library-go@v0.0.0-20260303171201-5d9eb6295ff6`: Use `CSIControllerSet`, `WithConditionalStaticResourcesController`, and `WithCSIDriverNodeService` extension points instead of inventing a second controller framework. Evidence: `pkg/operator/starter.go`.
- `github.com/openshift/library-go@v0.0.0-20260303171201-5d9eb6295ff6`: Reuse `WithCABundleDaemonSetHook(...)` when adding any new DaemonSet hook chain. Evidence: `starter.go` already wires it; removing it would regress trusted CA behavior.
- `github.com/openshift/library-go/pkg/operator/v1helpers`: Use `NewFakeOperatorClientWithObjectMeta(...)` for unit tests instead of introducing third-party mocking. Evidence: `pkg/operator/starter_test.go`, `docs/testing-guidelines.md`.
- `hack/e2e.sh`: Extend the existing bash e2e harness for cluster verification instead of inventing a second e2e framework inside this repo. Evidence: `Makefile` exposes `make test-e2e` only through `hack/e2e.sh`.
- `config/manifests/stable/image-references`: Reuse the established image-reference map when reasoning about operand image names instead of creating new naming schemes. Evidence: image stream tags in `image-references`.

## 6. Architectural Guardrails
*Rules the Planner MUST follow based on current repository patterns.*

- **Structural**
  - Keep feature logic in `pkg/operator/` and preserve the single-controller-set architecture. Evidence: `cmd/.../main.go` is thin CLI wiring; `pkg/operator/starter.go` is the only repo-local runtime composition point.
  - Static resources stay under `assets/`; the DaemonSet remains managed by `WithCSIDriverNodeService`, not the static-resource controller. Evidence: `starter.go`, `AGENTS.md`.
  - Avoid cluster-wide informer sprawl; informer scope is currently explicit (`operatorNamespace`, `""`). Evidence: `v1helpers.NewKubeInformersForNamespaces(kubeClient, operatorNamespace, "")` in `starter.go`.

- **API / Schema**
  - Do not plan repo-local feature work as if the Secrets Store driver config fields already exist in this branch. The feature is greenfield here and depends on upstream API availability.
  - Preserve management-state semantics: unknown operator state must not trigger sync. Evidence: `getOperatorSyncState()` returns `Unmanaged` on read errors.
  - Treat one-way managed ownership carefully; the proposal requires immutability semantics, but admission enforcement likely lives upstream in the API/CRD layer, not this repo.

- **Build / Tooling**
  - Honor Go `1.25.0` and the repo’s FIPS build behavior. Evidence: `go.mod`, `Makefile`, `Dockerfile.openshift`.
  - Pre-PR verification remains `make verify && make test-unit`. Evidence: `AGENTS.md`, `docs/testing-guidelines.md`.

- **Deployment / Packaging**
  - Keep image placeholder substitution patterns; do not hardcode release images in runtime assets. Evidence: `assets/node.yaml`, `image-references`, `README.md`.
  - Treat the stable CSV as packaging output and follow-up verification, not the primary source of runtime controller behavior. Evidence: runtime code reads `assets/`, not `config/manifests/stable/`.

- **Code Generation**
  - Dependency/API updates require `go mod tidy && go mod vendor`; do not edit `vendor/` manually. Evidence: `AGENTS.md`.
  - Adding asset subdirectories requires updating the `//go:embed` directive in `assets/assets.go`. Evidence: `assets/assets.go`, `AGENTS.md`.

- **Security**
  - Preserve `WithCABundleDaemonSetHook(...)` whenever DaemonSet hooks change. Evidence: `starter.go`; security docs call out CA bundle injection as standard.
  - Respect least-privilege RBAC separation. The node plugin role already includes secret rotation and token-minting access; new permissions should be justified before expanding scope. Evidence: `assets/rbac/secretproviderclasses_role.yaml`, `docs/security-guidelines.md`.
  - Maintain existing privileged container and hostPath assumptions for the node plugin unless there is a strong reason to change them. Evidence: `assets/node.yaml`, `docs/security-guidelines.md`.

## 7. Change Cascade Checklist
*When you change X, you MUST also change Y.*

| When you change... | You must also... | Verify with... |
|---|---|---|
| `ClusterCSIDriver` feature fields or any upstream API dependency | Update `go.mod`, run `go mod tidy && go mod vendor`, and verify repo-local code compiles against the new API surface | `make verify && make test-unit` |
| `pkg/operator/starter.go` controller wiring | Recheck management-state gating, CA-bundle hook preservation, and informer scope | `make verify && make test-unit` |
| `assets/csidriver.yaml` or dynamic CSIDriver asset rendering | Validate that the effective `CSIDriver` still reconciles and that packaging RBAC still covers it | `make verify && make test-unit && make test-e2e` |
| DaemonSet args or hook behavior in `assets/node.yaml` / new hook helpers | Verify upgrade-safe defaults, CA bundle behavior, and e2e pod-mount flow | `make verify && make test-unit && make test-e2e` |
| RBAC under `assets/rbac/` | Recheck runtime permissions and CSV packaging RBAC alignment | `make verify && make test-unit` |
| OLM metadata or image references | Rebuild/update packaging metadata and validate bundle expectations | `make metadata VERSION=<target> && make verify` or `./hack/create-bundle ...` when testing bundles |
| New asset subdirectories | Update `assets/assets.go` embed globs and register the asset in `starter.go` | `make verify && make test-unit` |
| `hack/e2e.sh` | Re-run cluster-backed e2e with required cluster prerequisites | `make test-e2e` |

## 8. Test & CI Reference
*How to test changes and what CI will enforce.*

### 8.1 Test Structure
- Unit tests live next to source in `pkg/operator/*_test.go`. Current pattern: `pkg/operator/starter_test.go`.
- Unit-test framework is standard `testing` with table-driven subtests and no assertion library. Evidence: `starter_test.go`, `docs/testing-guidelines.md`.
- Repo-local e2e coverage is shell-based in `hack/e2e.sh`, invoked by `make test-e2e`.
- No repo-local integration-test package distinct from unit/e2e was found in the files read.

### 8.2 How to Run Tests Locally
- Unit + verify preflight:
  ```bash
  make verify
  make test-unit
  ```
- Combined local gate:
  ```bash
  make check
  ```
- E2E (requires an OpenShift cluster, deployed operator/driver/provider, `oc`, and `openshift-tests` in `PATH`):
  ```bash
  make test-e2e
  ```
- Local operator run loop:
  ```bash
  make
  export OPERATOR_NAME=secrets-store-csi-driver-operator
  export DRIVER_IMAGE=registry.k8s.io/csi-secrets-store/driver:v1.3.3
  export NODE_DRIVER_REGISTRAR_IMAGE=quay.io/openshift/origin-csi-node-driver-registrar:latest
  export LIVENESS_PROBE_IMAGE=quay.io/openshift/origin-csi-livenessprobe:latest
  ./secrets-store-csi-driver-operator start --kubeconfig $KUBECONFIG --namespace openshift-cluster-csi-drivers
  ```

### 8.3 CI Pipeline
- Required PR checks: `make verify` and `make test-unit`. Evidence: `AGENTS.md`, `docs/testing-guidelines.md`.
- E2E runs against real clusters in OpenShift CI/Prow; local success is not assumed. Evidence: `AGENTS.md`, `docs/testing-guidelines.md`.
- CI configuration lives externally in `openshift/release`, not in this repo.
- CI/builds enforce FIPS-capable builds when available via `CGO_ENABLED=1 GOEXPERIMENT=strictfipsruntime` and `-tags strictfipsruntime,openssl`. Evidence: `Makefile`.

### 8.4 Test Coverage Gaps
- Feature-targeted unit coverage is currently thin: only `getOperatorSyncState()` has repo-local unit tests. Evidence: `pkg/operator/starter_test.go`.
- There is no repo-local unit coverage today for dynamic CSIDriver rendering, DaemonSet arg mutation, or token-request preservation logic because those features are not implemented yet.
- E2E today validates secret mount basics, not rotation cadence, WIF audiences, or upgrade-preservation paths. Evidence: `hack/e2e.sh`.

## 9. Developer Workflow
*Practical workflow reference so the Planning Agent can include correct build/verify/generate steps.*

### 9.1 Key Commands Reference

| Command | Purpose |
|---|---|
| `make` / `make build` | Build the operator binary |
| `make verify` | Run formatting/vet/version/dependency verification |
| `make test-unit` | Run unit tests for `./pkg/... ./cmd/...` |
| `make check` | Combined verify + unit test gate |
| `make test-e2e` | Run `hack/e2e.sh` against a live cluster |
| `make update-gofmt` | Auto-fix formatting |
| `make metadata VERSION=<x.y>` | Update OCP/OLM metadata with `hack/update-metadata.sh` |
| `./hack/create-bundle <driver> <operator> <bundle> <index>` | Build/push OLM bundle and index images |

### 9.2 Version Variables
- Go version: `go 1.25.0` in `go.mod`.
- Builder image/runtime image track OpenShift `5.0` in `Dockerfile.openshift`.
- `YQ_VERSION = v4.47.1` in `Makefile`.
- Image registry default: `IMAGE_REGISTRY ?= registry.svc.ci.openshift.org` in `Makefile`.
- Release payload image tags are mapped in `config/manifests/stable/image-references`.

### 9.3 Local Development Setup
- Required tools: Go `1.25+`, GNU `make`, `oc`, and cluster access for meaningful e2e.
- For local CLI runs, create a minimal `ClusterCSIDriver` first using the README sample, then export the three image env vars and run the operator `start` command.
- For bundle work, `podman` or `docker` plus `opm` are required by `hack/create-bundle`.

### 9.4 Common Development Scenarios
- **How to add new operator-consumed config from `ClusterCSIDriver`:**
  1. Confirm the upstream API field exists in the dependency version used by `go.mod`.
  2. Update `go.mod` and vendor if the API field is not yet available locally.
  3. Add repo-local helpers in `pkg/operator/` to extract/derive behavior from the new config.
  4. Wire those helpers into `starter.go`, preserving controller ordering and CA-bundle hook behavior.
  5. Add table-driven tests beside the helpers using `v1helpers.NewFakeOperatorClientWithObjectMeta(...)` where applicable.
  6. Run `make verify && make test-unit`.
- **How to add a new runtime-managed operand manifest:**
  1. Add the YAML under `assets/`.
  2. Update `assets/assets.go` if a new subdirectory is introduced.
  3. Register the asset in the correct controller in `pkg/operator/starter.go` (`WithConditionalStaticResourcesController` for static resources, not the node service).
  4. Re-run `make verify && make test-unit`.
- **How to extend e2e coverage for a new operator behavior:**
  1. Add setup/assertion steps to `hack/e2e.sh`.
  2. Preserve namespace creation/teardown and pod dump behavior already used for failure diagnostics.
  3. Run `make test-e2e` against a prepared cluster.

## 10. Platform & Environment Integration
*Platform-specific concerns that constrain or enable features.*

### 10.1 Security Context & Permissions
- The node plugin runs privileged and mounts host paths under `/var/lib/kubelet` and provider directories. Evidence: `assets/node.yaml`, `docs/security-guidelines.md`.
- SCC use is explicit: `privileged_role.yaml` + `node_privileged_binding.yaml` grant privileged SCC only to the node ServiceAccount. Evidence: security guidelines.
- The `secretproviderclasses-role` already includes:
  - secret CRUD for rotation/sync
  - `serviceaccounts/token` create
  - `csidrivers` get/list/watch
  Evidence: `assets/rbac/secretproviderclasses_role.yaml`.

### 10.2 Proxy & Network Configuration
- Cluster config observation is already wired through `WithCSIConfigObserverController(...)`. Evidence: `starter.go`.
- Trusted CA propagation is handled through `cabundle_cm.yaml` plus `WithCABundleDaemonSetHook(...)`. Evidence: `starter.go`, `docs/security-guidelines.md`.
- Metrics ingress is constrained via `assets/network-policy/allow-ingress-to-metrics-operand.yaml` in the static-resource list. Evidence: `starter.go`.

### 10.3 Cloud Provider Integration
- Current operator scope is provider-agnostic lifecycle management for the Secrets Store CSI driver, not provider-specific configuration. Evidence: approved spec and README/operator overview.
- The repo already anticipates bound token minting via RBAC, but repo-local code does not yet manage WIF audience configuration.
- Explicit non-goal for the feature: no provider auto-detection or provider-specific secret configuration should be planned in this repo-assessment stage.

### 10.4 Build & Compliance Constraints
- FIPS-capable builds are preferred/required in CI and production. Evidence: `Makefile`.
- Operator image build is a multi-stage build that runs `make` in the builder stage. Evidence: `Dockerfile.openshift`.
- Disconnected/release payload support depends on placeholder-driven images and `image-references`; do not regress this by hardcoding images.

### 10.5 Console / UI Integration
- * Not applicable — no console plugin or UI implementation surfaces were found in the repo-local files read.

### 10.6 Packaging & Lifecycle
- This repo ships through OLM. Stable CSV, package metadata, and image references live under `config/manifests/stable/`.
- `hack/create-bundle` copies `config/` into `opm-bundle/`, rewrites the stable CSV image references, then builds/pushes bundle and index images.
- Version/metadata bumps are done through `hack/update-metadata.sh`, exposed as `make metadata`.

## 11. Risks & Downstream Impacts
*Warnings for the Planner regarding potential breakages and high-risk areas.*

- **Upstream API dependency risk:** The feature spec depends on new Secrets Store config fields that are not consumed in this branch today. Impact: local compilation and planning can stall if the API dependency is not available first. Mitigation: plan the upstream API/vendor step explicitly before repo-local runtime wiring.
- **CSIDriver replacement risk:** `CSIDriver` behavior is currently static, and changing its spec will move the repo toward delete/recreate behavior managed by library-go. Impact: brief runtime transition windows and upgrade sensitivity. Mitigation: make preservation/default logic explicit and add focused unit + e2e coverage.
- **Hook ordering risk:** DaemonSet changes that forget the existing CA-bundle hook or misuse `WithCSIDriverNodeService` can silently regress trust-bundle behavior. Impact: provider connectivity failures. Mitigation: preserve hook chaining and treat CA bundle integration as mandatory.
- **Packaging drift risk:** Implementing behavior only in packaging files or sample manifests will not change runtime reconciliation. Impact: false sense of completion and CI/runtime mismatch. Mitigation: make `pkg/operator/` and `assets/` the primary implementation surfaces and treat CSV/sample updates as follow-on tasks.
- **RBAC overreach risk:** The repo already grants sensitive permissions for rotation and token minting. Impact: unnecessary privilege growth if implementation broadens RBAC casually. Mitigation: reuse existing privileges where possible and justify any additions narrowly.

### 11.1 Assessment Limitations / UNVERIFIED Items
- Repo-local feature absence is verified, but the exact vendored `openshift/api` type surface for future `SecretsStore` config was not opened directly in `vendor/`; verify the concrete type definitions there before planning the code shape.
- Detailed library-go retry/status semantics for the static-resource and node-service controllers were not read in vendored source during this assessment; verify vendored controller internals if the plan depends on precise degraded/retry behavior.
- No repo-local feature-gate files were found, but external product/release gating was not assessed; verify release-process expectations outside the repo if gating is required.
- Stable CSV install-mode/channel/update semantics were not read end-to-end; verify them directly in `config/manifests/stable/secrets-store-csi-driver-operator.clusterserviceversion.yaml` if the plan needs packaging changes beyond RBAC/image references.

## 12. Quick Reference Card
*A condensed cheat sheet the Planning Agent references when constructing implementation task sequences.*

### Preflight Checklist (run before every PR)
```bash
1. make verify
2. make test-unit
3. make check
4. make test-e2e   # only when cluster-backed coverage is required and environment is available
```

### Key File Quick-Nav
| I want to... | Look at... |
|---|---|
| Add new operator-consumed config behavior | `pkg/operator/starter.go` + new helper files in `pkg/operator/` |
| Add a new static runtime resource | `assets/` + `assets/assets.go` + `pkg/operator/starter.go` |
| Change the node DaemonSet runtime behavior | `assets/node.yaml` + `pkg/operator/starter.go` |
| Change the managed CSIDriver behavior | `assets/csidriver.yaml` + `pkg/operator/starter.go` |
| Change RBAC | `assets/rbac/*.yaml` first, then verify `config/manifests/stable/*.yaml` packaging alignment |
| Add/extend unit tests | `pkg/operator/starter_test.go` pattern or new `pkg/operator/*_test.go` files |
| Add/extend e2e coverage | `hack/e2e.sh` |
| Update release/payload image mappings | `config/manifests/stable/image-references` + `hack/create-bundle` |
