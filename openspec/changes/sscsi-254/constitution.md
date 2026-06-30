<!-- Companion artifact: repo-assessment.md (target files, reusable assets, change cascade) -->
# secrets-store-csi-driver-operator Constitution

**AgentRoutingMode:** PROVIDED

**Version**: 1.0 | **Ratified**: 2026-06-30 | **Last Amended**: 2026-06-30

---

## Core Principles

### I. Library-go CSIControllerSet Is the Sole Reconciliation Framework

All operator reconciliation is implemented using library-go's `CSIControllerSet` and its pre-built controller types (`WithLogLevelController`, `WithManagementStateController`, `WithConditionalStaticResourcesController`, `WithCSIConfigObserverController`, `WithCSIDriverNodeService`). New behavior is added as hooks (`AssetFunc`, `DaemonSetHookFunc`) to the existing chain — never as new controllers, new managers, or new reconcile loops. controller-runtime is not used and must not be introduced.

**Evidence:** `pkg/operator/starter.go:73–116` — the entire `CSIControllerSet` chain is built in a single fluent expression inside `RunOperator()`. No controller-runtime imports exist in the module.

---

### II. All Operator Logic Lives in pkg/operator/starter.go

This is a single-file operator. There are no per-resource reconciler files (`serviceaccounts.go`, `deployments.go`, etc.), no separate manager setup file, and no addon controller packages. All wiring — client creation, informer startup, controller chain construction — lives in `starter.go`. New operator-level logic (asset functions, DaemonSet hooks, config-reading helpers) must be added to this file, not extracted into new packages or files without explicit justification.

**Evidence:** `pkg/operator/` contains exactly two files: `starter.go` (all logic) and `starter_test.go` (unit tests). All 202 lines of `starter.go` are substantive operator code.

---

### III. Upstream API Ownership — No CRD Types in This Repo

The `ClusterCSIDriver` API is defined in `github.com/openshift/api/operator/v1` and is not implemented here. This repo contains no `*_types.go` files, no kubebuilder markers, no `make generate`, no `make manifests`, and no `zz_generated.deepcopy.go`. API field additions for SSCSI-254 (`SecretsStore`, `secretRotation`, `tokenRequests`) are implemented in `openshift/api` and consumed here as an import. Downstream agents must not generate CRD types or run `make generate` in this repo.

**Evidence:** `go.mod` imports `github.com/openshift/api v0.0.0-*` as an external dependency. No `api/` directory exists in this repo.

---

### IV. Static Operand Manifests via Go Embed — No Code Generation Pipeline

Operand YAML manifests are embedded at compile time using Go's native `//go:embed` directive in `assets/assets.go`. There is no `go-bindata`, no `make update-bindata`, and no `hack/update-*-manifests.sh` pipeline. Manifest changes take effect on next `go build`. New manifests in the covered glob paths (`*.yaml`, `rbac/*.yaml`, `network-policy/*.yaml`) are automatically embedded. Dynamic enrichment of manifests (e.g., setting fields from `ClusterCSIDriver` config at reconcile time) is performed in-memory in `starter.go`, not by modifying the embedded YAML on disk.

**Evidence:** `assets/assets.go` — `//go:embed *.yaml rbac/*.yaml network-policy/*.yaml` with `var f embed.FS`. No `Makefile` target for bindata regeneration exists.

---

### V. Unit Tests Use Library-go Fakes — No External Mock Frameworks

Unit tests in `pkg/operator/starter_test.go` use `v1helpers.NewFakeOperatorClientWithObjectMeta` from library-go for operator client mocking. Tests are table-driven with `t.Run(tc.name, ...)` and standard `t.Fatalf` for assertions. counterfeiter, gomock, and testify are not used. New test functions for SSCSI-254 must follow this exact pattern: define a `FakeOperator` struct embedding `metav1.ObjectMeta`, `opv1.OperatorSpec`, and `opv1.OperatorStatus`; create a fake client via `NewFakeOperatorClientWithObjectMeta`; call the function under test; assert with `t.Fatalf`.

**Evidence:** `pkg/operator/starter_test.go:17–72` — `TestGetOperatorSyncState` defines this pattern completely. No third-party assertion libraries are imported.

---

### VI. FIPS-Compliant Build Is Required for CI and Production

All CI and production builds must use `CGO_ENABLED=1 GOEXPERIMENT=strictfipsruntime go build -trimpath -tags strictfipsruntime,openssl`. Non-FIPS builds (`CGO_ENABLED=1 go build`) are permitted for local development only and generate a Makefile warning. No PR may introduce build tags or `CGO_ENABLED=0` assumptions. The build system uses `build-machinery-go` included via `vendor/`.

**Evidence:** `Makefile:22–34` — the Makefile auto-detects FIPS support, sets `GO` and `GO_BUILD_FLAGS` accordingly, and prints an explicit warning when FIPS is unavailable.

---

### VII. Three-State Management with Removable Operator Semantics

The operator participates in the OpenShift Managed/Unmanaged/Removed lifecycle. `WithManagementStateController(operandName, true)` marks this operator as removable: when `ClusterCSIDriver.spec.managementState` is `Removed` or the CR's `DeletionTimestamp` is set, `getOperatorSyncState` returns `Removed` and `ConditionalStaticResourcesController` deletes all 8 managed assets. New features that add managed resources must register them with `ConditionalStaticResourcesController` so they participate in the Removed cleanup path. Resources that must survive Removed state must not be added to the static resources list.

**Evidence:** `pkg/operator/starter.go:78,95–99,141–171` — `WithManagementStateController(operandName, true)` and `getOperatorSyncState` implement this state machine. `func() bool { return getOperatorSyncState(operatorClient) == opv1.Removed }` is the deletion predicate.

---

## Additional Constraints

- **No feature gates:** This operator has no OpenShift TechPreview/GA feature gate mechanism. Do not add `DefaultFeatureGate.Enabled(...)` calls or `featuregates.config.openshift.io` discovery logic. — **Evidence:** `pkg/operator/starter.go` — no feature gate imports or checks.

- **No admission webhooks:** API validation is enforced via CEL rules in `openshift/api` at the CRD level. Do not add webhook servers, `cert-manager` Certificate CRs for TLS, or any webhook registration code to this operator. — **Evidence:** No webhook files exist in `pkg/` or `config/`.

- **Namespace:** The operator and operand both run in `openshift-cluster-csi-drivers`. All namespace-scoped assets use `${NAMESPACE}` substitution via `replaceNamespaceFunc`. New namespace-scoped assets must use this substitution — do not hardcode the namespace string. — **Evidence:** `pkg/operator/starter.go:36,131–138` — `namespaceKey = "${NAMESPACE}"` constant.

- **DaemonSet constraints:** The node plugin DaemonSet must maintain `priorityClassName: system-node-critical`, `tolerations: [{operator: Exists}]`, and the `cluster-autoscaler.kubernetes.io/enable-ds-eviction: "false"` annotation. These are required for correct node-level CSI operation. Do not remove or weaken them. — **Evidence:** `assets/node.yaml` — these fields are set in the static manifest.

- **OLM version management:** OCP version in the CSV must be bumped only via `make metadata VERSION=x.y` or `./hack/update-metadata.sh x.y`. Hand-editing version fields in `config/manifests/stable/*.clusterserviceversion.yaml` is forbidden. — **Evidence:** `hack/update-metadata.sh` — script updates `package.yaml`, CSV, `README.md`, and `Makefile` atomically.

- **Single operator namespace + single manager:** There is exactly one `CSIControllerSet` instance running. Do not create additional manager instances or parallel controller sets. — **Evidence:** `pkg/operator/starter.go:73–116` — single `csiControllerSet` variable.

---

## Development Workflow

| Activity | Requirement | Evidence |
|----------|-------------|----------|
| Local unit tests | `go test ./pkg/... ./cmd/... -v -count=1` or `make test-unit` | `Makefile:GO_TEST_PACKAGES` |
| Build verification | `go build ./...` (with CGO_ENABLED=1) | `Makefile:GO_BUILD_FLAGS` |
| Full pre-PR check | `make check` (= `make verify` + `make test-unit`) | `Makefile:check` target |
| Verify only | `make verify` (build-machinery-go format/vet/imports checks) | `Makefile:include golang.mk` |
| FIPS local build | `CGO_ENABLED=1 GOEXPERIMENT=strictfipsruntime go build -trimpath -tags strictfipsruntime,openssl ./...` | `Makefile:22–34` |
| OCP version bump | `make metadata VERSION=x.y` | `hack/update-metadata.sh` |
| OLM bundle build | `cd hack && ./create-bundle <images>` | `hack/create-bundle` |
| E2E tests | `hack/e2e.sh` (live cluster required; not runnable locally) | `Makefile:test-e2e` |
| Code generation | Not applicable — no custom CRDs in this repo | — |
| API changes | Must land in `openshift/api` first; bump `go.mod` dependency afterward | `go.mod` |

---

## Agent Routing

**AgentRoutingMode: PROVIDED** — `openspec/schemas/openspec-agile-workflow/agents.md` is installed.

| Agent ID | Scope | When to route |
|----------|-------|---------------|
| **OperatorController_Agent** | `pkg/operator/starter.go` — CSIControllerSet chain wiring, new `AssetFunc`, new `DaemonSetHookFunc`, config-reading helpers, informer wiring changes | Any task modifying operator reconciliation logic |
| **ManifestsAssets_Agent** | `assets/*.yaml`, `assets/rbac/*.yaml`, `assets/network-policy/*.yaml` — static manifest content changes | Asset YAML content changes only (no Go code) |
| **OLMRelease_Agent** | `config/manifests/`, `config/metadata/` — CSV, package manifest, `make metadata` | OLM bundle updates, CSV field changes, OCP version bumps |
| **Testing_Agent** | `pkg/operator/starter_test.go` — unit tests; `hack/e2e.sh` — E2E test authoring | Unit test additions; E2E test tasks |
| **Docs_Agent** | `README.md`, `must-gather/` | Documentation-only changes |

**OAPE command mapping:**

| Agent ID | OAPE command |
|----------|-------------|
| OperatorController_Agent | `api-implement` |
| Testing_Agent (unit) | `api-implement` |
| Testing_Agent (E2E) | `e2e-generate` |
| ManifestsAssets_Agent | Manual (edit YAML + `go build ./...`) |
| OLMRelease_Agent | Manual (`make metadata`) |
| Docs_Agent | Manual |

**Routing rules for SSCSI-254:**
- Tasks touching `starter.go` → `OperatorController_Agent` via `api-implement`
- Asset YAML content changes → `ManifestsAssets_Agent` (manual); split from starter.go tasks
- OLM/CSV update → `OLMRelease_Agent` (separate task, never combined with code changes)
- No `api-generate` tasks — there are no custom CRD types in this repo
- No `api-generate-tests` tasks — no kubebuilder API validation testsuites in this repo

---

## Governance

- This constitution supersedes ad-hoc conventions for all downstream Planning, Task Creation, and Code Generation agents working on this change.
- **Amendments:** require observable repo evidence (new file, Makefile target, or CI step); bump Version and Last Amended date.
- **Conflicts:** if `specs.md` requirements contradict a constitution principle, surface the conflict in `plan.md §8` (Risks) — do not silently override either document.
- **Companion docs:** `openspec/schemas/openspec-agile-workflow/agents.md` provides operator-specific execution routing and stage hints. When agents.md and this constitution conflict, agents.md governs for routing decisions; this constitution governs for coding conventions.
- **Precedence for SSCSI-254:** openshift/api PR #2846 must be available before operator tasks compile; this is a structural dependency, not optional.
- **Complexity:** any deviation from the CSIControllerSet-only pattern (e.g., adding a standalone goroutine, a new package, or a second manager) requires explicit written rationale in plan.md before implementation.
