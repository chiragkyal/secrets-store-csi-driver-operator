# Technical Implementation Plan
**Feature:** Configurable Secret Rotation and Workload Identity Federation for the Secrets Store CSI Driver

## 0. Inputs acknowledged

| Input | Status |
|-------|--------|
| Spec source | SSCSI-254 / `csi-secrets-store-rotation-and-wif` (`specs.md`, approved) |
| Repo assessment pin | working-folder mode — `/Users/ckyal/go/src/github.com/chiragkyal/secrets-store-csi-driver-operator`, branch `openspec-cursor-agent-sonnet5`, commit `573b5a09` (tooling_status: FULL) |
| `agents.md` | PROVIDED — root `AGENTS.md`/`CLAUDE.md` (resolved ahead of `openspec/inputs/agents.md` per its own pointer). No explicit "Planning Stage Hints" section or agent-ID routing table exists in it. Per the template rule for this case, this plan uses the **provisional capability taxonomy** (API, OperatorController, ManifestsBindata, WebhookTLS, RBACSecurity, OLMRelease, Testing, Docs), cross-referenced against `AGENTS.md`'s own Package Structure / Code Ownership boundaries (`cmd/`, `pkg/operator/`, `pkg/version/`, `pkg/dependencymagnet/`, `assets/`) so phase capability labels stay consistent with the repo's real ownership model. |
| `spec_validator_results.json` | PROVIDED — `validation.json` (Stage 0), overall_score 87%, PASS, no blockers |
| `constitution.md` | PROVIDED — `openspec/inputs/constitution.md` v1.0.0 (schema inputs/ lookup tier; not found in target-repo root or change inputs/). `AgentRoutingMode: PROVIDED` (from its metadata comment) — mirrored above: agents.md is provided but without a formal routing table, hence the provisional-taxonomy fallback. |

**Constitution drift note (not a plan blocker):** Constitution §"Additional Constraints" states the CI image registry as `.../ocp/4.22:...`. `repo-assessment.md` §0/§10.6 found the actual current state is `ocp/5.0` (CSV `v5.0.0`, `.ci-operator.yaml`, `Makefile`). This plan uses the repo-assessment's `5.0` finding as ground truth per the input-precedence rule ("repo_assessment.md wins for repository facts"); the constitution document itself should be refreshed separately, outside this change's scope.

## 1. Architectural strategy

**Repo-grounded reality check:** `repo-assessment.md` §3.1 states this feature is **GREENFIELD** — no rotation-configuration or WIF/`tokenRequests` code exists anywhere on the pinned branch (`starter.go` has zero references to `SecretsStore`, `secretRotation`, `tokenRequests`, or `requiresRepublish`; `node.yaml` hardcodes rotation args as literal strings; `csidriver.yaml` has no dynamic fields). Every phase below is framed as **new implementation**, not hardening or completion of partially-working code.

This feature integrates into the existing architecture along the two extension points the codebase already supports, per Constitution Principle I (Single Controller Pattern — library-go `CSIControllerSet` only, no new reconciler loop):

1. **Rotation control (`secretRotation`)** is expressed as a new `csidrivernodeservicecontroller.DaemonSetHookFunc`, registered as an additional variadic argument to the existing `WithCSIDriverNodeService(...)` call in `starter.go`. This mirrors the existing `WithCABundleDaemonSetHook` pattern exactly: a factory function that closes over a way to read the live `ClusterCSIDriver` (not the hook's `*opv1.OperatorSpec` parameter, which is the generic spec and does not carry `DriverConfig` — repo-assessment §1.3) and mutates the `csi-driver` container's `--enable-secret-rotation=`/`--rotation-poll-interval=` args in place.
2. **WIF token configuration (`tokenRequests`)** is expressed as a new, dedicated `AssetFunc` for `assets/csidriver.yaml`, split into its **own** `WithConditionalStaticResourcesController` call (the method explicitly supports being called multiple times) rather than branching inside the currently-shared `replaceNamespaceFunc`. This avoids the blast-radius risk repo-assessment §11 flags (a bug in a shared function would affect RBAC/SA/ConfigMap/NetworkPolicy rendering too). The dynamic `AssetFunc` sets `requiresRepublish`/`tokenRequests` on the rendered `CSIDriver` object before it flows through the **existing, unmodified** `resourceapply.ApplyCSIDriver` hash-based recreate logic (`vendor/.../resourceapply/storage.go:141` — already reusable, no new recreate logic needed).

**Constitution compliance:**
- **Principle I**: both mechanisms above are `CSIControllerSet` hooks/`AssetFunc`s, not a new reconciler — compliant.
- **Principle III (No Custom CRD Types)**: this feature reads new *fields* on the existing `ClusterCSIDriver` singleton (owned upstream by `openshift/api`), not a new CRD defined by this repo — compliant, but see §4/§7/§8 for the upstream dependency this creates.
- **Principle IV (Managed/Unmanaged/Removed mandatory)**: both new mechanisms MUST gate on `getOperatorSyncState` (or the equivalent gating each parent controller already performs) — this is an explicit phase requirement below, not optional.
- **Principle VIII (CA bundle hook mandatory)**: the new rotation `DaemonSetHookFunc` is **added alongside**, never in place of, the existing `WithCABundleDaemonSetHook` — Phase 4 includes an explicit regression check for this.
- **Principle VI (RBAC is asset-driven)**: if Phase 3/4 verification determines new RBAC is needed, it MUST be added as YAML in `assets/rbac/` and registered in the static-resources file list — never granted inline/dynamically.

## 2. Persistence & state

- **Kubernetes objects:**
  - `ClusterCSIDriver` (operator.openshift.io/v1, singleton `secrets-store.csi.k8s.io`) — **source of truth**, external schema (see §4 blocker).
  - `CSIDriver` (storage.k8s.io/v1) — **derived/reconciled** object; `spec.requiresRepublish`/`spec.tokenRequests` become dynamically computed from `ClusterCSIDriver` on each sync, applied via the existing `resourceapply.ApplyCSIDriver` hash-recreate path.
  - `DaemonSet` (`secrets-store-csi-driver-node`) — **derived/reconciled**; `--enable-secret-rotation=`/`--rotation-poll-interval=` container args become dynamically computed from `ClusterCSIDriver` on each sync via the new hook.
- **Operand config/state:** no new ConfigMaps/Secrets. Rotation/WIF config flows entirely through container args (DaemonSet) and a CR-derived object spec (CSIDriver) — no bindata generation step is introduced (Constitution Principle II: assets are plain embedded YAML, never hand-regenerated).
- **External/platform-injected state:** unchanged — the existing CNO-managed trusted CA bundle (`cabundle_cm.yaml` + `WithCABundleDaemonSetHook`) is untouched by this feature.
- **N/A:** no new persistent storage, database, or long-lived cache is introduced by this feature.

## 3. Interfaces & contracts (operator-native)

### 3.1 Kubernetes APIs (CRDs/CRs)

- `ClusterCSIDriver` — **no new CRD**; requires an **upstream, external** schema addition in `openshift/api` (`CSIDriverType` enum gains `"SecretsStore"`; `CSIDriverConfigSpec` gains a `SecretsStore *SecretsStoreCSIDriverConfigSpec` field; new `SecretsStoreSecretRotation`/`SecretsStoreTokenRequests` discriminated-union types with CEL immutability rules) per `openspec/inputs/ep.md` §"API Extensions". This repo only **consumes** the resulting Go types after a `go.mod`/`vendor` bump — it does not, and must not, hand-author these types (Constitution Principle X: never modify `vendor/` directly).
- `CSIDriver` (`storage.k8s.io/v1`, built-in Kubernetes type, not a CRD) — `spec.requiresRepublish` (bool) and `spec.tokenRequests` ([]TokenRequest) become dynamically set; both fields already exist in the upstream Kubernetes API (no schema change needed here), only this operator's *rendering* of them is new.
- Immutability: the "cannot revert `tokenRequests.type` from `Managed`" rule is enforced by a CEL validation rule on `ClusterCSIDriver` **in the CRD schema** (owned by `openshift/api`) — this operator must not re-implement that check in Go (repo-assessment §11).

### 3.2 Controller/runtime interfaces (internal)

Illustrative names only (not final until Phase 1 confirms the actual merged upstream type names — see §8 Open Question #2):

- New file `pkg/operator/rotation.go`: a factory function (e.g. `WithSecretRotationDaemonSetHook(...) csidrivernodeservicecontroller.DaemonSetHookFunc`) modeled directly on `csidrivernodeservicecontroller.WithCABundleDaemonSetHook` (`vendor/.../csidrivernodeservicecontroller/helpers.go:32`), plus a small local `setArg(args []string, prefix, value string) []string` helper (no equivalent helper exists in this repo or its vendored dependencies today — must be written new and unit-tested directly).
- New file `pkg/operator/csidriver_asset.go`: a dynamic `resourceapply.AssetFunc`-compatible function that reads the live `ClusterCSIDriver` (via whichever lister/getter Phase 1 determines is simplest against the real merged types — options include the existing `dynamicInformers`/`dynamicClient` already constructed in `starter.go`, or a new typed lister from an `operatorv1` informer factory) and the live `CSIDriver` object (to implement the "preserve existing `tokenRequests` when omitted" behavior, FR-005), then serializes the mutated manifest bytes for `StaticResourceController`/`ApplyDirectly` to apply.
- Both new files need read access to the live `ClusterCSIDriver` object with its new `DriverConfig.SecretsStore` field — **the exact lister mechanism is UNVERIFIED until Phase 1 lands real types** (repo-assessment §11.1); do not guess a specific `Lister` type name before that.
- Status conditions: no new condition types are introduced. Both extended controllers already emit `<name>Degraded`/`<name>Available`/`<name>Progressing` via the existing `WithSyncDegradedOnError`/`StaticResourceController.Sync` machinery — reuse as-is.

### 3.3 Webhooks / admission (if applicable)

N/A — no webhook exists in this repo and none is introduced. All new validation (bounds, immutability, discriminated-union enforcement) lives in the upstream CRD's OpenAPI/CEL schema, not in an admission webhook owned by this operator.

### 3.4 RBAC / security boundaries (if applicable)

- The operator's own `ClusterRole` (via CSV `clusterPermissions`) already grants `get/list/watch/update/patch` on `clustercsidrivers`/`clustercsidrivers/status` — **sufficient** to read the new `driverConfig.secretsStore` fields once they exist; no operator-side RBAC change is anticipated.
- The node-plugin RBAC (`assets/rbac/secretproviderclasses_role.yaml`) already grants `serviceaccounts/token: create`, whose relationship to the new kubelet-driven `tokenRequests` mechanism is **UNVERIFIED** per repo-assessment §3.3/§11.1 (kubelet, not the driver, normally mints the token for CSI `tokenRequests`). **Do not add or remove RBAC based on assumption** — Phase 3/4 includes an explicit verification step before any RBAC YAML change; if a change is needed, it MUST be a new/edited file under `assets/rbac/` registered in the static-resources file list (Constitution Principle VI), never inline.
- Blast radius: both new mechanisms only read `ClusterCSIDriver` (already-permitted) and write to `CSIDriver`/`DaemonSet` (already-permitted via existing static-resource/DaemonSet-service controllers) — no new cluster-scoped write permissions are expected.

### 3.5 Packaging / OLM (if applicable)

- No CSV/package version bump is required for this feature by itself (repo-assessment §10.6) — do not invoke `hack/update-metadata.sh` as part of this change.
- Whether the upstream `driverConfig.secretsStore` fields ship behind a `TechPreviewNoUpgrade` FeatureGate (per `openshift/api`'s own convention that "All APIs should start as tech preview," observed in its `AGENTS.md` during repo-assessment) is **undetermined by any input to this plan** — tracked as Open Question #3 (§8). If gated, feature-gate enforcement happens API-server-side; this operator's code does not need to check the gate explicitly, but CSV Tech-Preview annotations *might* be relevant depending on the outcome.

## 4. Dependencies & sequencing graph

**Critical path summary:**
1. **(EXTERNAL, BLOCKING)** `openshift/api` merges the `SecretsStore` driver-config type extension (ep.md §"API Extensions"). Nothing in Phase 2/3 can compile against real types until this lands and this repo bumps `go.mod`/`vendor/github.com/openshift/api`.
2. Once vendored (Phase 1 complete): Phase 2 (rotation hook) and Phase 3 (CSIDriver dynamic asset) can proceed **in parallel** — they touch disjoint files and disjoint controllers.
3. Phase 4 (wire-up + regression guard) depends on **both** Phase 2 and Phase 3 completing.
4. Phase 5 (e2e) depends on Phase 4 producing a deployable operator+driver.
5. Phase 6 (docs, optional) depends on Phase 4.

**Parallelizable workstreams:** Phase 2 and Phase 3 (once Phase 1 lands) — different files (`pkg/operator/rotation.go` + `assets/node.yaml`-adjacent DaemonSet path vs. `pkg/operator/csidriver_asset.go` + `assets/csidriver.yaml`-adjacent static-resource path), different unit test files, no shared state beyond both reading `ClusterCSIDriver`.

**Explicit blockers / external dependencies:**
- The `openshift/api` PR/merge is entirely outside this operator repo's control and is the single hard blocker for all functional implementation work (Phases 2–6). Test-writing against hand-written fakes/mocks of the *proposed* shape could theoretically start early, but any such tests would need to be re-verified once real types land (see §8 Open Question #2) — this plan does not recommend front-loading that risk given the small size of the eventual real test surface.

## 5. Implementation phases (logical sequence; NOT tasks)

### Phase 1: Vendor the SecretsStore API Extension

- **Goal:** Make `ClusterCSIDriver.Spec.DriverConfig.SecretsStore` (and `CSIDriverType` including `"SecretsStore"`) available to this repo's Go code.
- **Dependencies:** External — the `openshift/api` PR implementing ep.md's API Extensions section must merge upstream first. This phase is otherwise the first phase of this change; nothing else can start meaningfully before it, though it can be tracked in parallel with earlier stages of this workflow.
- **Target files:** `go.mod` (bump `github.com/openshift/api` version), `go.sum`, `vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go` and any related `zz_generated.deepcopy.go`/apply-configuration files under `vendor/github.com/openshift/client-go/operator/applyconfigurations/` — all regenerated by `go mod vendor`, never hand-edited (Constitution Principle X).
- **Required capabilities:** API (provisional taxonomy — no explicit `agents.md` routing exists for dependency/vendor bumps).
- **Verification hooks:** `go build ./...`; `make verify` (runs `build-machinery-go`'s `verify-deps`, confirming `vendor/` matches `go.mod`); manual confirmation that the new `SecretsStore`-related types compile and are importable from `pkg/operator/`.

### Phase 2: Secret Rotation DaemonSet Hook

- **Goal:** Implement FR-001, FR-002, FR-003, FR-011, FR-012 (rotation enable/disable, custom interval within 1s–~1yr bounds, default preservation for unconfigured clusters, stop-refresh-when-disabled behavior).
- **Dependencies:** Phase 1 (needs real `SecretRotation` types to read).
- **Target files:** new `pkg/operator/rotation.go` (no existing file to extend — repo-assessment §2 confirms `pkg/operator/` currently has only `starter.go`/`starter_test.go`); `pkg/operator/starter.go` (register the new hook as an additional `optionalDaemonSetHooks` argument to `WithCSIDriverNodeService`, and — per repo-assessment §1.3 trap — add a `ClusterCSIDriver`-derived informer to the currently-`nil` `optionalInformers` slice if event-driven resync is desired rather than relying on the controller's 1-minute `ResyncEvery`); new `pkg/operator/rotation_test.go`.
- **Required capabilities:** OperatorController (provisional taxonomy).
- **Verification hooks:** Unit — `pkg/operator/rotation_test.go`, table-driven per `docs/testing-guidelines.md` (`v1helpers.NewFakeOperatorClientWithObjectMeta`, `t.Run`/`t.Fatalf`), covering: nil `driverConfig`, nil `secretsStore`, nil `secretRotation` (→ defaults `true`/`2m`), `type: None` (→ `false`, stop refresh), `type: Custom` with a custom interval (→ interval reflected in `--rotation-poll-interval=`), and arg-replacement-by-prefix correctness. `make test-unit`.

### Phase 3: Dynamic CSIDriver Object (`requiresRepublish` + `tokenRequests`)

- **Goal:** Implement FR-004 through FR-010 (WIF audience configuration, preservation of existing `tokenRequests` when unconfigured, clearing via explicit empty list, validation-bounds rejection surfaced correctly, propagation without manual restart).
- **Dependencies:** Phase 1.
- **Target files:** new `pkg/operator/csidriver_asset.go`; `pkg/operator/starter.go` (split `"csidriver.yaml"` out of the shared 8-file `WithConditionalStaticResourcesController` call into its **own** call with the new dynamic `AssetFunc`, per repo-assessment §1.3 Option (b) / §11 risk mitigation); `assets/csidriver.yaml` (base manifest content unchanged — it becomes the *starting point* the dynamic `AssetFunc` mutates, not a file that itself needs new fields); new `pkg/operator/csidriver_asset_test.go`.
- **Required capabilities:** OperatorController + ManifestsBindata (provisional taxonomy).
- **Verification hooks:** Unit — table-driven tests covering the full nil-path matrix from `openspec/inputs/ep.md`'s Test Plan (nil `driverConfig`, nil `secretsStore`, nil `tokenRequests` → preserve existing live `CSIDriver.spec.tokenRequests`; `type: Managed` with audiences → set exactly those; `type: Managed` with empty `managed.audiences` → clear all; `type: Unmanaged` → preserve existing), plus confirmation that `requiresRepublish` mirrors `secretRotation.type` correctly (per `specs.md` FR-011/Edge Cases). `make test-unit`.

### Phase 4: Wire-Up, Management-State Compliance, and Regression Guard

- **Goal:** Integrate Phase 2 and Phase 3 into `RunOperator`; explicitly verify both new mechanisms respect `getOperatorSyncState` (Constitution Principle IV) and that `WithCABundleDaemonSetHook` remains registered and functional (Constitution Principle VIII); verify the FR-003/FR-012 "no behavior change when unconfigured" baseline from repo-assessment §3.2 holds via a regression test asserting the rendered DaemonSet args and `CSIDriver` spec are byte-for-byte identical to today's static output when `driverConfig` is absent.
- **Dependencies:** Phase 2, Phase 3.
- **Target files:** `pkg/operator/starter.go`.
- **Required capabilities:** OperatorController.
- **Verification hooks:** Unit — a regression test (new or added to `rotation_test.go`/`csidriver_asset_test.go`) asserting default-path output matches the pre-feature baseline table in repo-assessment §3.2 exactly (`--enable-secret-rotation=true`, `--rotation-poll-interval=2m`, no `requiresRepublish`/`tokenRequests` set). Manual — `make check` (chains `verify` + `test-unit`, per Constitution Principle V, mandatory before every PR). Integration — N/A, this repo has no integration test tier (repo-assessment §8.1).

### Phase 5: E2E Coverage

- **Goal:** Extend `hack/e2e.sh` to cover SC-001 through SC-004/SC-006/SC-007 from `specs.md` (rotation toggle, custom interval, single- and multi-audience WIF authentication success, invalid-config rejection, managed-immutability rejection) and the upgrade-preservation scenario repo-assessment §11 flags as the highest-impact untested path today.
- **Dependencies:** Phase 4 (requires a real, deployable operator+driver build to test against a live cluster).
- **Target files:** `hack/e2e.sh` (extend the existing `test_*` bash-function style — `test_prechecks`, `test_setup`, `test_pod_with_secret`, etc. — repo-assessment §8.4 confirms zero existing rotation/WIF assertions today).
- **Required capabilities:** Testing.
- **Verification hooks:** E2E — `make test-e2e` (requires a live OpenShift cluster with the operator/driver/e2e-provider already deployed and `oc` in `$PATH`; **not runnable in this planning/authoring environment**, consistent with repo-assessment §8.2 and this repo's own conventions).

### Phase 6: Documentation and Sample Manifests (optional)

- **Goal:** Improve discoverability of the new configuration surface; not required by any FR in `specs.md`.
- **Dependencies:** Phase 4.
- **Target files:** `README.md`; optionally a new `config/manifests/stable/sscsi-sample-*.yaml` demonstrating rotation/WIF configuration (repo-assessment §10.5 notes existing samples are all `SecretProviderClass` examples, not `ClusterCSIDriver` examples).
- **Required capabilities:** Docs.
- **Verification hooks:** Manual review only — N/A for automated `make test-unit`/`test-e2e` coverage (docs are not executable).

## 6. Verification matrix (maps to spec acceptance)

| Category | Coverage | Files / Suites |
|----------|----------|----------------|
| Unit | Rotation config extraction (nil/enabled/disabled/custom paths, FR-001–003/011/012); DaemonSet arg replace-by-prefix; CSIDriver dynamic-asset field mapping and full tokenRequests nil-path preservation matrix (FR-004–010); default-path regression guard (byte-identical to pre-feature baseline) | `pkg/operator/rotation_test.go` (new), `pkg/operator/csidriver_asset_test.go` (new) |
| Integration | N/A — this repo has no integration test tier distinct from unit/e2e (repo-assessment §8.1) | - |
| E2E | Rotation toggle (US1), custom interval (US3), single-audience WIF auth success (US2), multi-audience WIF (US4), invalid-config rejection (SC-006), managed-immutability rejection (SC-007), upgrade preservation | `hack/e2e.sh` (extended, Phase 5) |
| Manual / Cluster | `oc get csidriver secrets-store.csi.k8s.io -o yaml` (verify `requiresRepublish`/`tokenRequests`); `oc get ds -n openshift-cluster-csi-drivers secrets-store-csi-driver-node -o jsonpath='{.spec.template.spec.containers[?(@.name=="csi-driver")].args}'` (verify rotation args) | Runbook commands per `openspec/inputs/ep.md` §"Support Procedures"; carried into `README.md` if Phase 6 is executed |
| N/A | Webhook/admission tests (no webhook exists or is introduced); FIPS-specific tests (no new cryptography is introduced by this feature) | - |

## 7. Risks, migrations, and operational follow-ups

- **Upstream API drift (CRITICAL, repo-assessment §11 #1):** `openshift/api` may merge with field/type names or validation semantics that differ from `openspec/inputs/ep.md`'s draft. This plan's §3.2 type/function names are **illustrative only**. Phase 1 must re-verify the actual merged types before Phase 2/3 begin; do not treat this plan's names as final contracts for `tasks.md`.
- **DaemonSet hook informer wiring (repo-assessment §11 #2):** if the `ClusterCSIDriver`-derived informer is not added to `optionalInformers`, config changes still apply within the controller's existing 1-minute `ResyncEvery` — not a correctness bug, but a UX/testing-latency risk during manual verification. Document the chosen tradeoff in the eventual PR description.
- **Static-resource `AssetFunc` blast radius (repo-assessment §11 #3):** Phase 3 MUST use the split-controller approach (Option (b)), not a conditional branch inside the currently-shared `replaceNamespaceFunc`, to avoid risking regressions in RBAC/SA/ConfigMap/NetworkPolicy rendering for an unrelated change.
- **Immutability-rule duplication:** do not re-implement the CEL "cannot revert from Managed" rule in Go — trust the upstream CRD's admission-time validation; this operator only reads already-validated objects.
- **Downgrade behavior is undefined** (carried forward from `specs.md`'s one `[NEEDS CLARIFICATION]` marker and repo-assessment §11 #5) — no existing downgrade-handling pattern exists in this repo to model from. This plan does not invent behavior; see §8 Open Question #1.
- **RBAC purpose ambiguity** (`serviceaccounts/token: create`, repo-assessment §3.3/§11.1): Phase 3/4 must include an explicit verification step (reading the upstream driver's actual token-request flow) before assuming no RBAC change is needed — do not assume either way without checking.
- **Upgrade/migration:** per `specs.md` FR-003/FR-005/FR-010/FR-012 and repo-assessment §3.2's baseline table, existing clusters with no `driverConfig.secretsStore` set must see **zero behavior change** post-upgrade; existing manually-patched `CSIDriver.spec.tokenRequests` (e.g., pre-existing Azure WIF) must be read from the live object and preserved when the new config is omitted. Phase 4's regression guard exists specifically to catch a violation of this.
- **Compatibility (OpenShift/Hypershift/MicroShift):** per `openspec/inputs/ep.md` §"Topology Considerations", Hypershift/standalone are N/A (no special handling), and the Secrets Store CSI Driver Operator is not yet part of MicroShift — no MicroShift-specific work is in scope.
- **Constitution drift (non-blocking):** the `ocp/4.22` reference in `constitution.md`'s "Additional Constraints" table is stale relative to the repo's actual `5.0` state; flag for a separate constitution-refresh, not part of this feature's scope.

## 8. Open questions / SME decisions

| # | Question | Who can answer | Assumption if unanswered before Task Creation |
|---|---|---|---|
| 1 | What is the correct behavior when an operator is **downgraded** to a pre-feature version after `tokenRequests.type` was already set to `"Managed"`? | API/product SME (`openshift/api` reviewers) or a constitution/spec amendment | Implementation does **not** special-case downgrade; the limitation is documented in `implementation-report.md` as a known gap, not silently resolved. |
| 2 | What are the **exact** merged type/field names and validation semantics for `SecretsStoreCSIDriverConfigSpec`, `SecretsStoreSecretRotation`, and `SecretsStoreTokenRequests` once the `openshift/api` PR lands? | `openshift/api` PR author/reviewers | Phase 1 re-verifies against the actual merged code before Phase 2/3 begin; this plan's illustrative names (§3.2) are not binding. |
| 3 | Will the new `driverConfig.secretsStore` fields ship behind a `TechPreviewNoUpgrade` FeatureGate (per `openshift/api`'s "all new APIs start as tech preview" convention observed during repo-assessment)? | `openshift/api` PR / API architects | Assume no operator-side code change is needed either way (feature-gate enforcement, if any, is API-server-side); revisit CSV Tech-Preview annotations only if the merged PR confirms gating. |
| 4 | Is the pre-existing `serviceaccounts/token: create` RBAC grant (`assets/rbac/secretproviderclasses_role.yaml`) relevant to, sufficient for, or unrelated to the new WIF `tokenRequests` mechanism? | Implementation-time verification against the upstream `secrets-store-csi-driver` driver's actual token-consumption code (a separate repository) | No RBAC change is planned by default; Phase 3/4 verification may revise this if evidence is found. |
