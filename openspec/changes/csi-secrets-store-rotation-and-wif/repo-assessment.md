# Repository Assessment Report

## 0. Assessment Header

| Field | Value |
|---|---|
| Repository | `secrets-store-csi-driver-operator` (working-folder mode — analyzed in place, no clone) |
| Root | `/home/spatidar/Downloads/secrets-store-csi-driver-operator` |
| Remotes | `origin` → `https://github.com/chiragkyal/secrets-store-csi-driver-operator.git` (fork) |
| Branch | `openspec-cursor-agent-sonnet5` |
| Commit | `0b6b5b3a` (`archive: csi-secrets-store-rotation-and-wif`) |
| Go version (module / local) | `go 1.25.0` (go.mod) |
| Operator/OLM version | `5.0.0` (CSV `secrets-store-csi-driver-operator.v5.0.0`, `olm.skipRange: ">=4.13.0-0 <5.0.0"`) |
| Vendored `openshift/api` | `v0.0.0-20260709102940-580f1c1ba691` — **includes** `SecretsStore` driver types |
| tooling_status | OK — full read/shell/grep access; `make test-unit` passed locally |
| Spec status | `specs.md` — **Approved** (User Story 1–4, FR-001…FR-012, 1 open `[NEEDS CLARIFICATION]` on downgrade behavior) |
| Validation status (Stage 0) | `validation.json` — **PASS** (87%), approved |

**Feature presence conclusion**: **DELTA / POST-IMPLEMENTATION** — the rotation + WIF feature described in `specs.md` is **already implemented** on this branch in `pkg/operator/rotation.go`, `pkg/operator/csidriver_asset.go`, and wired in `pkg/operator/starter.go`, with unit tests (`rotation_test.go`, `csidriver_asset_test.go`) and extended E2E scaffolding in `hack/e2e.sh`. Downstream `plan.md`/`tasks.md` should treat remaining work as verification, gap closure, and PR readiness — not greenfield controller authoring — unless a diff against upstream `openshift/secrets-store-csi-driver-operator` reveals missing pieces.

**Stage 0/1 note on OpenShift 5.0**: Repo evidence (CSV `v5.0.0`, `.ci-operator.yaml` `openshift-5.0`, Makefile image paths) confirms **OpenShift 5.0** is the correct target release; this is not a typo in the enhancement proposal.

## 1. Architecture Overview

### 1.1 Controller Framework

Single-binary operator on `library-go`'s `csicontrollerset` (not controller-runtime). All controllers composed in `RunOperator` (`pkg/operator/starter.go`):

1. **LogLevelController** — syncs `spec.logLevel`; unchanged by this feature.
2. **ManagementStateController** — Managed/Unmanaged/Removed; operator is **removable** (`true` at L88).
3. **SecretsStoreConditionalStaticResourcesController** — reconciles 7 static files (RBAC, SA, ConfigMap, NetworkPolicy). **`csidriver.yaml` was removed from this list** and split out (L95–103).
4. **SecretsStoreDynamicCSIDriverController** — **new second** `WithConditionalStaticResourcesController` call (L110–139) with `NewDynamicCSIDriverAssetFunc(...)` for `["csidriver.yaml"]` only.
5. **SecretsStoreDriverCSIConfigObserverController** — observes Infrastructure/Proxy/APIServer; **not** used for `driverConfig.secretsStore`.
6. **SecretsStoreDriverNodeServiceController** — DaemonSet from `node.yaml` with two hooks: `WithCABundleDaemonSetHook` + **`WithSecretRotationDaemonSetHook`** (L155–158).

### 1.2 Operator Client and CR Watching

Generic operator client via `goc.NewClusterScopedOperatorClientWithConfigName` (L56–64). Feature code reads the **typed** `ClusterCSIDriver` via a **dynamic informer lister**:

```81:81:pkg/operator/starter.go
	clusterCSIDriverInformer := dynamicInformers.ForResource(gvr)
```

Passed to:
- `NewDynamicCSIDriverAssetFunc(..., clusterCSIDriverInformer.Lister(), ...)` (L124–129)
- `WithSecretRotationDaemonSetHook(clusterCSIDriverInformer.Lister(), providerName)` (L155–158)
- `optionalInformers: []factory.Informer{clusterCSIDriverInformer.Informer()}` (L149) — **event-driven DaemonSet resync on CR change**

### 1.3 Dead Code / Traps for the Planner

- **`DaemonSetHookFunc` receives generic `*opv1.OperatorSpec`, not `DriverConfig`** — existing implementation correctly closes over `clusterCSIDriverLister` in `WithSecretRotationDaemonSetHook` (`pkg/operator/rotation.go:127–166`). Do not refactor hooks to read rotation config from the `opSpec` parameter.
- **`csidriver.yaml` is split into its own controller** (Option (b) from the enhancement proposal) — already done. Do not merge it back into the shared static-resources controller.
- **`resourceapply.ApplyCSIDriver`** (vendored `library-go`) handles hash-based delete+recreate — used unchanged via `StaticResourceController`; dynamic asset func only supplies correct YAML bytes.
- **CRD validation (FR-006 immutability, FR-008 bounds) lives in `openshift/api` CEL rules** — operator code intentionally does **not** re-implement; see comments in `getTokenRequests` (`pkg/operator/csidriver_asset.go:52–55`).
- **`formatRotationInterval` preserves `"2m"` not `"2m0s"`** for whole-minute defaults (`pkg/operator/rotation.go:87–91`) — critical for FR-003/FR-012 no-op upgrades; do not revert to raw `time.Duration.String()`.
- **When `ClusterCSIDriver` is NotFound**, rotation hook leaves DaemonSet args untouched (`rotation.go:133–137`); dynamic CSIDriver asset returns static manifest without dynamic fields (`csidriver_asset.go:138–142`). Planner must preserve this upgrade-safe behavior in any follow-up changes.

## 2. Directory Structure

```
cmd/secrets-store-csi-driver-operator/main.go   — cobra CLI only
pkg/operator/starter.go                          — RunOperator: controller composition
pkg/operator/rotation.go                         — getSecretRotationConfig, setArg, WithSecretRotationDaemonSetHook
pkg/operator/csidriver_asset.go                  — getRequiresRepublish, getTokenRequests, NewDynamicCSIDriverAssetFunc
pkg/operator/rotation_test.go                    — unit tests for rotation config + hook (469 lines)
pkg/operator/csidriver_asset_test.go             — unit tests for CSIDriver asset + tokenRequests matrix (323 lines)
pkg/operator/starter_test.go                     — getOperatorSyncState tests only
assets/assets.go                                 — //go:embed *.yaml rbac/*.yaml network-policy/*.yaml
assets/node.yaml                                 — DaemonSet; still has static --enable-secret-rotation=true, --rotation-poll-interval=2m (hook overrides at runtime)
assets/csidriver.yaml                            — base CSIDriver (no requiresRepublish/tokenRequests; injected dynamically)
assets/rbac/*.yaml                               — includes serviceaccounts/token create (see §11.1 UNVERIFIED)
config/manifests/stable/                         — OLM CSV v5.0.0, image-references, samples
hack/e2e.sh                                      — extended with rotation toggle, custom interval, WIF audience tests (~522 lines)
vendor/github.com/openshift/api/operator/v1/     — SecretsStore types present (types_csi_cluster_driver.go)
```

## 3. Feature-Specific Analysis

### 3.1 Feature Presence on This Branch

**IMPLEMENTED (DELTA).** Evidence:

| Capability | Implementation | Tests |
|---|---|---|
| Rotation enable/disable + interval (FR-001–003, FR-011) | `getSecretRotationConfig`, `WithSecretRotationDaemonSetHook` | `rotation_test.go`; `hack/e2e.sh` `test_rotation_toggle`, `test_rotation_custom_interval` |
| `requiresRepublish` mirrors rotation (FR-011) | `getRequiresRepublish` → `NewDynamicCSIDriverAssetFunc` | `csidriver_asset_test.go` |
| WIF token audiences (FR-004–007) | `getTokenRequests` → dynamic CSIDriver asset | `csidriver_asset_test.go` preservation matrix; `hack/e2e.sh` WIF tests |
| Preserve existing tokenRequests when omitted (FR-005, FR-012) | `getTokenRequests` nil-path + live CSIDriver lister read | `csidriver_asset_test.go` |
| Split CSIDriver controller | `starter.go` L110–139 | indirect via asset tests |
| Event-driven DaemonSet resync | `optionalInformers` includes `clusterCSIDriverInformer` | hook tests with fake lister |

`make test-unit` (2026-07-10): **PASS** — `ok github.com/openshift/secrets-store-csi-driver-operator/pkg/operator 1.157s`.

### 3.2 Current Hardcoded Defaults (baseline for FR-003/FR-012)

| Setting | Static template value | Runtime when unconfigured |
|---|---|---|
| Rotation enabled | `true` (`assets/node.yaml:45`) | Hook applies same default via `getSecretRotationConfig` |
| Rotation poll interval | `2m` (`assets/node.yaml:46`) | Hook renders `2m` via `formatRotationInterval` (not `2m0s`) |
| `requiresRepublish` | absent in `assets/csidriver.yaml` | Dynamic asset sets `true` when rotation enabled (default path) |
| `tokenRequests` | absent | Dynamic asset preserves live CSIDriver's existing values |

### 3.3 Related Existing RBAC

- `assets/rbac/secretproviderclasses_role.yaml`: node SA has `serviceaccounts/token` **create** — purpose relative to kubelet-driven `CSIDriver.spec.tokenRequests` remains **UNVERIFIED** (§11.1).
- CSV operator `clusterPermissions` already include `clustercsidrivers` get/list/watch/update/patch — sufficient for reading `driverConfig.secretsStore`; **no CSV RBAC change observed as needed**.

## 4. Configuration & Extension Points

### 4.1 Configurable Fields (vendored `openshift/api`)

| Field | Status on branch |
|---|---|
| `driverConfig.driverType: SecretsStore` | Present in vendored API (`SecretsStoreDriverType`) |
| `driverConfig.secretsStore.secretRotation` | `type: None\|Custom`, `custom.minimumRefreshAge` (1–31560000s) |
| `driverConfig.secretsStore.tokenRequests` | `type: Managed\|Unmanaged`, `managed.audiences[]` |

### 4.2 Hook / Pipeline Points

| # | Hook point | Current registrations | Error behavior |
|---|---|---|---|
| 1 | `WithCSIDriverNodeService` DaemonSet hooks | `WithCABundleDaemonSetHook`, **`WithSecretRotationDaemonSetHook`** | Hook error → `getDaemonSet` returns wrapped error → `Degraded=true` |
| 2 | `SecretsStoreDynamicCSIDriverController` AssetFunc | **`NewDynamicCSIDriverAssetFunc`** on `["csidriver.yaml"]` | AssetFunc error → static resource controller degraded |
| 3 | `SecretsStoreConditionalStaticResourcesController` | `replaceNamespaceFunc` on 7 static files | unchanged |

## 5. Reusable Assets (Anti-Duplication)

- **`getSecretRotationConfig`** (`rotation.go:55–74`): single source of truth for rotation enable + interval; reused by `getRequiresRepublish` — do not duplicate nil-path logic.
- **`setArg` / `formatRotationInterval`** (`rotation.go:87–109`): tested utilities for DaemonSet arg mutation — extend, don't reimplement.
- **`getTokenRequests`** (`csidriver_asset.go:56–84`): full preservation/managed matrix — extend with new cases only via table tests in `csidriver_asset_test.go`.
- **`NewDynamicCSIDriverAssetFunc`** (`csidriver_asset.go:110–161`): pattern for dynamic static resources — model for any future per-CR asset enrichment.
- **`resourceapply.ApplyCSIDriver`** (vendored): hash-recreate for CSIDriver spec changes — do not write custom apply logic.
- **`WithCABundleDaemonSetHook`** (vendored `helpers.go`): reference pattern for informer-closing hooks — rotation hook already follows this.
- **`replaceNamespaceFunc`** (`starter.go:174–182`): wrap, don't duplicate, for any new asset funcs needing `${NAMESPACE}` substitution.
- **`docs/*.md`**: security/performance/error-handling/testing guidelines — follow for any delta changes.

## 6. Architectural Guardrails

- **Structural**: Controllers only in `RunOperator`; no business logic in `cmd/`.
- **API / Schema**: Types live in vendored `openshift/api` — bump via `go mod tidy && go mod vendor`, never hand-edit vendored generated code.
- **Build / Tooling**: Go 1.25; FIPS via `GOEXPERIMENT=strictfipsruntime` in CI — local builds may warn without FIPS-capable Go.
- **Deployment**: Rotation/WIF driven by `ClusterCSIDriver` reads in hooks/asset funcs — **not** operator-pod env vars (unlike `${DRIVER_IMAGE}`).
- **Security**: Do not change DaemonSet privileged/SCC posture for this feature.
- **Testing**: Table-driven, stdlib `testing` only, no testify/ginkgo — match `rotation_test.go` / `starter_test.go` patterns.

## 7. Change Cascade Checklist

| When you change... | You must also... | Verify with... |
|---|---|---|
| `pkg/operator/rotation.go` or `csidriver_asset.go` | Update matching `_test.go` tables | `make test-unit` |
| `assets/node.yaml` rotation arg defaults | Ensure hook still finds/replaces by prefix; check `formatRotationInterval` for no-op upgrade path | `make test-unit`; inspect `TestDefaultPathMatchesPreFeatureBaseline` if present |
| `assets/csidriver.yaml` base fields | Ensure `NewDynamicCSIDriverAssetFunc` still round-trips via `resourceread.ReadCSIDriverV1OrDie` | `make test-unit` |
| `pkg/operator/starter.go` controller wiring | Confirm informer passed to both hook/asset func **and** `optionalInformers` | `make test-unit`; manual CR patch + observe DaemonSet resync |
| `vendor/github.com/openshift/api` bump | `go mod tidy && go mod vendor` | `make verify`, `go build ./...` |
| New file under `assets/` subdirectory | Update `//go:embed` in `assets/assets.go` | `make build` |
| `hack/e2e.sh` | Requires live OpenShift cluster + `oc` | `make test-e2e` (not sandbox-runnable) |
| Go formatting | — | `make update-gofmt`, `make verify` |

No `make generate` / `make manifests` in this repo's Makefile.

## 8. Test & CI Reference

### 8.1 Test Structure

- **Unit**: `pkg/operator/*_test.go` — stdlib `testing`, table-driven, fakes from `k8s.io/client-go/kubernetes/fake` and dynamic unstructured objects for `ClusterCSIDriver`.
- **E2E**: `hack/e2e.sh` bash — rotation toggle, custom interval, WIF single/multi audience, upgrade-preservation comments/scenarios.
- No ginkgo/testify in `go.mod`.

### 8.2 How to Run Tests Locally

```bash
make test-unit          # go test ./pkg/... ./cmd/...
make verify             # go vet, gofmt, go version
make check              # verify + test-unit (preflight)
make test-e2e           # hack/e2e.sh — requires live cluster + oc
make update-gofmt       # fix formatting
```

### 8.3 CI Pipeline

- External Prow config in `openshift/release`.
- PR gates: `make verify`, `make test-unit`.
- FIPS builds in CI (`.ci-operator.yaml`: `rhel-9-release-golang-1.26-openshift-5.0`).

### 8.4 Test Coverage Gaps

- **API integration tests** for CRD CEL validation (FR-006 immutability, discriminated unions) are **not in this repo** — live in `openshift/api` or cluster-level test suites.
- **E2E WIF tests** verify CSIDriver audience propagation + secret mount continuity — **not** full cloud-provider STS/Azure AD federation (`hack/e2e.sh` comments acknowledge this).
- **Downgrade behavior** (`specs.md` `[NEEDS CLARIFICATION]`) has **no test** anywhere in repo.
- **`getOperatorSyncState`** only coverage in `starter_test.go` — rotation/WIF paths have dedicated tests elsewhere.

## 9. Developer Workflow

### 9.1 Key Commands Reference

| Command | Purpose |
|---|---|
| `make build` | Build operator binary |
| `make test-unit` | Unit tests |
| `make verify` | Static checks |
| `make check` | Preflight before PR |
| `make test-e2e` | Cluster E2E |
| `make metadata VERSION=X.Y` | OCP version bump (not needed for this feature) |

### 9.2 Version Variables

- `go.mod`: Go 1.25.0; `openshift/api` at `580f1c1ba691` (includes SecretsStore).
- CSV `5.0.0` — feature ships within this line; no CSV bump required for functional delivery.

### 9.3 Local Development Setup

- Go 1.25+, GNU Make; `yq` auto-fetched by Makefile.
- No cluster required for unit tests/verify.

### 9.4 Common Development Scenarios

**How rotation + WIF were added (observed pattern on this branch):**

1. Add pure functions in `pkg/operator/rotation.go` and `csidriver_asset.go` with table tests.
2. Split `csidriver.yaml` into second `WithConditionalStaticResourcesController` with `NewDynamicCSIDriverAssetFunc`.
3. Register `WithSecretRotationDaemonSetHook` + wire `clusterCSIDriverInformer` into `optionalInformers`.
4. Extend `hack/e2e.sh` with `oc patch clustercsidriver` scenarios and arg/CSIDriver assertions.
5. Run `make check` before push.

## 10. Platform & Environment Integration

### 10.1 Security Context & Permissions

- Node DaemonSet uses `privileged` SCC via existing RBAC — unchanged.
- Feature modifies container **args** and CSIDriver **spec** only.

### 10.2 Proxy & Network Configuration

- CA bundle hook unchanged — do not disturb when editing hook list.

### 10.3 Cloud Provider Integration

- Operator responsibility ends at setting `CSIDriver.spec.tokenRequests`; kubelet mints tokens; upstream driver binary authenticates to cloud APIs — **no cloud SDK in this repo**.

### 10.4 Build & Compliance Constraints

- FIPS: no new crypto in feature code — low risk.

### 10.6 Packaging & Lifecycle

- OLM CSV `v5.0.0`, stable channel.
- `ClusterCSIDriver` CRD not owned by this repo — schema from `openshift/api`/CVO.

## 11. Risks & Downstream Impacts

- **Downgrade behavior undefined** (from `specs.md` / `validation.json`) — no code or test models this; `plan.md` must either scope out or get product decision before claiming completeness.
- **E2E WIF is propagation-only** — SC-003/SC-004 full cloud federation not verified in `hack/e2e.sh`; risk of false confidence if plan assumes full WIF E2E.
- **Managed tokenRequests cleanup** uses explicit empty audience list (`hack/e2e.sh` restore helper) — cannot revert to Unmanaged per FR-006; document in runbooks.
- **Dynamic CSIDriver recreate window** — brief CSIDriver absence on spec hash change (library-go behavior); running pods unaffected but planners should note for support docs.

### 11.1 Assessment Limitations / UNVERIFIED Items

- **`serviceaccounts/token: create` RBAC** purpose vs kubelet-driven tokenRequests — verify against upstream driver, not assumed here.
- **CRD CEL rules as installed on cluster** — assessment based on vendored Go types + enhancement proposal; live cluster schema not inspected in this session.
- **Diff vs upstream `openshift/secrets-store-csi-driver-operator`** — this fork branch may contain commits not yet merged upstream; `plan.md` should confirm target PR base and any delta still required for upstream acceptance.

## 12. Quick Reference Card

### Preflight Checklist (run before every PR)

```
1. make verify
2. make test-unit
   (or: make check)
3. make update-gofmt   # if verify reports fmt issues
```

### Key File Quick-Nav

| I want to... | Look at... |
|---|---|
| Change rotation config logic | `pkg/operator/rotation.go` + `rotation_test.go` |
| Change CSIDriver tokenRequests/requiresRepublish | `pkg/operator/csidriver_asset.go` + `csidriver_asset_test.go` |
| Wire controllers/informers | `pkg/operator/starter.go` |
| Change static DaemonSet template defaults | `assets/node.yaml` (hook overrides at reconcile) |
| Change base CSIDriver manifest | `assets/csidriver.yaml` + dynamic asset func |
| Add E2E scenario | `hack/e2e.sh` (follow `test_rotation_toggle` pattern) |
| Bump API types | `go.mod` → `go mod tidy && go mod vendor` → `make verify` |
