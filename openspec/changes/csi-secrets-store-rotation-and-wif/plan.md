# Technical Implementation Plan
**Feature:** Configurable Secret Rotation and Workload Identity Federation for the Secrets Store CSI Driver

## 0. Inputs acknowledged

| Input | Status |
|-------|--------|
| Spec source | SSCSI-254 / `csi-secrets-store-rotation-and-wif` (`specs.md`, approved) |
| Repo assessment pin | working-folder mode — `/home/spatidar/Downloads/secrets-store-csi-driver-operator`, branch `openspec-cursor-agent-sonnet5`, commit `0b6b5b3a` (tooling_status: FULL) |
| `agents.md` | PROVIDED — root `AGENTS.md`/`CLAUDE.md`. No explicit "Planning Stage Hints" section or agent-ID routing table. This plan uses the **provisional capability taxonomy** (API, OperatorController, ManifestsBindata, WebhookTLS, RBACSecurity, OLMRelease, Testing, Docs), aligned with `AGENTS.md` package ownership (`cmd/`, `pkg/operator/`, `assets/`). |
| `spec_validator_results.json` | PROVIDED — `validation.json` (Stage 0), overall_score 87%, PASS, no blockers |
| `constitution.md` | PROVIDED — `openspec/inputs/constitution.md` v1.0.0 (schema inputs/ lookup). `AgentRoutingMode: PROVIDED`. |

**Constitution drift note:** Constitution "Additional Constraints" references `ocp/4.22` image registry; repo-assessment confirms actual target is **OpenShift 5.0** (`CSV v5.0.0`, `.ci-operator.yaml`). This plan uses repo-assessment as ground truth for repository facts.

## 1. Architectural strategy

**Repo-grounded reality check:** `repo-assessment.md` §3.1 documents **IMPLEMENTED (DELTA)** — not greenfield. On commit `0b6b5b3a`, rotation + WIF logic exists in `pkg/operator/rotation.go`, `pkg/operator/csidriver_asset.go`, and is wired in `pkg/operator/starter.go` (split dynamic CSIDriver controller, `WithSecretRotationDaemonSetHook`, `clusterCSIDriverInformer` in `optionalInformers`). Unit tests pass (`make test-unit` OK); `hack/e2e.sh` includes rotation and WIF scenarios. Vendored `openshift/api` at `580f1c1ba691` already includes `SecretsStore` types.

**Planning posture:** Phases below prioritize **verification against `specs.md` FR/SC acceptance**, **regression guard for upgrade-safe defaults (FR-003/FR-012)**, **gap closure** (downgrade behavior, RBAC ambiguity, upstream diff), and **PR readiness** — not authoring net-new controller logic unless verification exposes a defect.

This feature integrates via the two extension points already implemented, per Constitution Principle I:

1. **Rotation (`secretRotation`)** — `WithSecretRotationDaemonSetHook` mutates `csi-driver` container args from live `ClusterCSIDriver` via dynamic informer lister (not generic `OperatorSpec`).
2. **WIF (`tokenRequests`)** — `NewDynamicCSIDriverAssetFunc` on a **separate** `WithConditionalStaticResourcesController` call sets `requiresRepublish`/`tokenRequests`, flowing through existing `resourceapply.ApplyCSIDriver` hash-recreate.

**Constitution compliance (verified against current wiring):**
- **Principle I:** Both mechanisms are `CSIControllerSet` hooks/AssetFuncs — compliant.
- **Principle III:** Reads upstream `ClusterCSIDriver` fields; no custom CRD in this repo — compliant.
- **Principle IV:** Both controllers gate on `getOperatorSyncState` predicates — compliant.
- **Principle VIII:** `WithCABundleDaemonSetHook` remains registered alongside rotation hook — compliant.
- **Principle X:** API types consumed from vendored `openshift/api` — already bumped; no hand-edited vendor.

## 2. Persistence & state

- **Kubernetes objects:**
  - `ClusterCSIDriver` (singleton `secrets-store.csi.k8s.io`) — **source of truth** (upstream schema in vendored `openshift/api`).
  - `CSIDriver` (`storage.k8s.io/v1`) — **derived**; `requiresRepublish`/`tokenRequests` computed by `NewDynamicCSIDriverAssetFunc`.
  - `DaemonSet` (`secrets-store-csi-driver-node`) — **derived**; rotation args computed by `WithSecretRotationDaemonSetHook`.
- **Operand config/state:** No new ConfigMaps/Secrets. Rotation/WIF via container args + CSIDriver spec only (Constitution Principle II).
- **External/platform-injected state:** Unchanged — CA bundle hook + `cabundle_cm.yaml` preserved.
- **N/A:** No new databases or operator-local persistent cache.

## 3. Interfaces & contracts (operator-native)

### 3.1 Kubernetes APIs (CRDs/CRs)

- `ClusterCSIDriver` — `driverConfig.secretsStore.secretRotation` and `tokenRequests` discriminated unions with CEL immutability (`tokenRequests.type` cannot revert from `Managed`). Enforced upstream in `openshift/api`; operator consumes validated objects only.
- `CSIDriver` — `spec.requiresRepublish` mirrors rotation enable state; `spec.tokenRequests` set/preserved/cleared per `getTokenRequests` matrix.
- Immutability: **not** re-implemented in Go (`csidriver_asset.go` comments).

### 3.2 Controller/runtime interfaces (internal)

Implemented interfaces (verify, do not redesign unless tests fail):

| Component | Location | Responsibility |
|-----------|----------|----------------|
| `getSecretRotationConfig` | `pkg/operator/rotation.go` | Nil-path defaults, None/Custom branches |
| `formatRotationInterval` | `pkg/operator/rotation.go` | Preserve `"2m"` not `"2m0s"` for no-op upgrades |
| `setArg` | `pkg/operator/rotation.go` | Prefix-based arg replacement |
| `WithSecretRotationDaemonSetHook` | `pkg/operator/rotation.go` | DaemonSet reconcile hook |
| `getRequiresRepublish` | `pkg/operator/csidriver_asset.go` | Mirrors rotation enable |
| `getTokenRequests` | `pkg/operator/csidriver_asset.go` | Managed/Unmanaged/omit preservation |
| `NewDynamicCSIDriverAssetFunc` | `pkg/operator/csidriver_asset.go` | Dynamic CSIDriver YAML generation |
| Controller wiring | `pkg/operator/starter.go` | Split controllers, informer, hooks |

Status conditions: existing `<name>Degraded`/`Available`/`Progressing` via library-go — no new condition types.

### 3.3 Webhooks / admission (if applicable)

N/A — validation is CRD CEL in `openshift/api`; no operator webhook.

### 3.4 RBAC / security boundaries (if applicable)

- Operator CSV permissions sufficient for `clustercsidrivers` read/patch — no change anticipated.
- Node SA `serviceaccounts/token: create` in `assets/rbac/secretproviderclasses_role.yaml` — **UNVERIFIED** relevance to kubelet-driven `tokenRequests` (repo-assessment §11.1). Verification phase must confirm before any RBAC edit.
- No new cluster-scoped writes beyond existing CSIDriver/DaemonSet reconcile paths.

### 3.5 Packaging / OLM (if applicable)

- No CSV version bump required for functional delivery (repo-assessment §9.2).
- Feature ships within OCP 5.0 line; CRD schema delivered via cluster/CVO, not this repo's bundle.

## 4. Dependencies & sequencing graph

**Critical path summary:**
1. **Baseline verification** — confirm implementation matches `specs.md` FR/SC via unit tests + code review (`make check`).
2. **Gap closure** — resolve or document downgrade behavior; verify RBAC; confirm upstream diff scope.
3. **Cluster verification** — run extended `hack/e2e.sh` on live OpenShift 5.0 cluster.
4. **PR readiness** — draft PR to fork/upstream with `make check` green.

**Parallelizable workstreams:** Gap-closure tracks (downgrade decision vs RBAC verification vs upstream diff review) can proceed in parallel once baseline verification passes.

**Explicit blockers / external dependencies:**
- `openshift/api` types — **already vendored** on this branch; no compile blocker.
- Live cluster required for E2E (`make test-e2e`) — not available in planning environment.
- Product decision on downgrade behavior — external SME (see §8).

## 5. Implementation phases (logical sequence; NOT tasks)

### Phase 1: Baseline Verification and Spec Traceability

- **Goal:** Confirm existing implementation on `0b6b5b3a` satisfies FR-001–FR-012 and P1 user stories; establish traceability from spec → code → tests.
- **Dependencies:** None (code already present).
- **Target files:** `pkg/operator/rotation.go`, `pkg/operator/csidriver_asset.go`, `pkg/operator/starter.go`, `pkg/operator/rotation_test.go`, `pkg/operator/csidriver_asset_test.go`, `assets/node.yaml`, `assets/csidriver.yaml`, `go.mod`.
- **Required capabilities:** OperatorController, Testing (provisional taxonomy).
- **Verification hooks:** `make check`; manual FR→function mapping review; confirm `TestDefaultPathMatchesPreFeatureBaseline` (or equivalent) exists for FR-003/FR-012 no-op upgrade path.

### Phase 2: Regression and Edge-Case Hardening

- **Goal:** Close any gaps found in Phase 1 — nil-path matrix completeness, `formatRotationInterval` no-op semantics, Managed empty-audience clear (FR-007), hook error when `csi-driver` container missing.
- **Dependencies:** Phase 1 (must identify gaps first).
- **Target files:** `pkg/operator/rotation.go`, `pkg/operator/csidriver_asset.go`, corresponding `*_test.go` — edit only if Phase 1 finds defects.
- **Required capabilities:** OperatorController, Testing.
- **Verification hooks:** `make test-unit`; table-driven tests per `docs/testing-guidelines.md`.

### Phase 3: RBAC and Security Verification

- **Goal:** Resolve UNVERIFIED `serviceaccounts/token: create` RBAC purpose; confirm no RBAC change needed OR add asset-driven RBAC per Constitution Principle VI.
- **Dependencies:** Phase 1.
- **Target files:** `assets/rbac/secretproviderclasses_role.yaml` (read-only unless change required); optionally upstream driver docs/binary (external repo — discovery step UNVERIFIED in-repo).
- **Required capabilities:** RBACSecurity (provisional taxonomy).
- **Verification hooks:** Manual review of upstream driver token flow; document conclusion in `implementation-report.md` if no code change.

### Phase 4: E2E and Cluster Acceptance

- **Goal:** Execute SC-001–SC-007 on live cluster — rotation toggle, custom interval, WIF audience propagation, upgrade preservation, invalid-config rejection (where testable via API server).
- **Dependencies:** Phases 1–2 (stable binary); live OpenShift cluster.
- **Target files:** `hack/e2e.sh` (extend only if scenarios missing vs `specs.md`).
- **Required capabilities:** Testing.
- **Verification hooks:** `make test-e2e`; manual runbook commands from enhancement proposal Support Procedures (`oc get csidriver`, `oc get ds ... jsonpath`).

### Phase 5: Upstream PR and Release Readiness

- **Goal:** Prepare merge-ready change set — diff against `openshift/secrets-store-csi-driver-operator` main, confirm vendor pin, FIPS build in CI, no constitution violations.
- **Dependencies:** Phases 1–4.
- **Target files:** entire operator diff; `go.mod`/`vendor/`; no CSV bump unless release team requires.
- **Required capabilities:** OLMRelease, API (provisional taxonomy).
- **Verification hooks:** `make check`; CI (`make verify` + `make test-unit` per Prow); optional FIPS build if toolchain supports `GOEXPERIMENT=strictfipsruntime`.

### Phase 6: Documentation (optional)

- **Goal:** Improve administrator discoverability — sample `ClusterCSIDriver` with `driverConfig.secretsStore`; rotation interval guidance (A-004).
- **Dependencies:** Phase 5.
- **Target files:** `README.md`; optional `config/manifests/stable/sscsi-sample-*.yaml`.
- **Required capabilities:** Docs.
- **Verification hooks:** Manual review only.

## 6. Verification matrix (maps to spec acceptance)

| Category | Coverage | Files / Suites |
|----------|----------|----------------|
| Unit | Rotation nil/enabled/disabled/custom paths (FR-001–003/011/012); `setArg`/`formatRotationInterval`; CSIDriver `requiresRepublish` + tokenRequests preservation/managed/clear matrix (FR-004–010); default-path regression | `pkg/operator/rotation_test.go`, `pkg/operator/csidriver_asset_test.go` |
| Integration | N/A — no integration tier in this repo | - |
| E2E | Rotation toggle (US1/SC-001), custom interval (US3/SC-002), WIF audiences single/multi (US2/US4/SC-003–004), upgrade preservation (SC-005), managed cleanup via empty audiences | `hack/e2e.sh` |
| Manual / Cluster | Verify DaemonSet args and CSIDriver spec match CR config | `oc get csidriver secrets-store.csi.k8s.io -o yaml`; `oc get ds -n openshift-cluster-csi-drivers secrets-store-csi-driver-node -o jsonpath='{.spec.template.spec.containers[?(@.name=="csi-driver")].args}'` |
| N/A | Full cloud STS/Azure AD federation (SC-003/004 end-to-end with real cloud IAM); CRD CEL integration tests (live in `openshift/api`); downgrade after Managed (undefined) | - |

**FR / P1 story mapping:**

| Requirement | Phase | Verification |
|-------------|-------|--------------|
| FR-001, US1 (rotation on/off) | 1, 2, 4 | `rotation_test.go`, `test_rotation_toggle` |
| FR-002, US3 (custom interval) | 1, 2, 4 | `rotation_test.go`, `test_rotation_custom_interval` |
| FR-003, FR-012 (defaults preserved) | 1, 2 | baseline regression tests |
| FR-004–007, US2/US4 (WIF audiences) | 1, 2, 4 | `csidriver_asset_test.go`, e2e WIF tests |
| FR-008 (bounds validation) | N/A at operator | CRD admission in `openshift/api` |
| FR-009–010 (propagation, persistence) | 1, 4 | e2e + manual cluster checks |
| FR-011 (disabled = no refresh) | 1, 4 | `getRequiresRepublish` tests + e2e |

## 7. Risks, migrations, and operational follow-ups

- **Downgrade behavior undefined** (`specs.md` `[NEEDS CLARIFICATION]`) — no code/test; document as known limitation unless SME decides (§8 #1).
- **E2E WIF scope limited** — tests verify CSIDriver propagation + secret mount continuity, not full cloud federation (repo-assessment §8.4).
- **Fork vs upstream delta unverified** — branch may contain commits not yet on `openshift/secrets-store-csi-driver-operator`; Phase 5 must confirm PR base and scope.
- **Managed immutability** — cleanup only via `managed.audiences: []`; operators cannot revert to Unmanaged (FR-006); document in runbooks.
- **CSIDriver recreate window** — brief object absence on spec hash change (library-go); running pods unaffected.
- **Upgrade/migration:** FR-003/005/010/012 require zero behavior change when `driverConfig` absent; `formatRotationInterval` preserves `"2m"` literal — regression tests are critical guard.
- **Compatibility:** Hypershift/MicroShift N/A per enhancement proposal; standard multi-node clusters only (specs A-007).
- **Constitution drift:** `ocp/4.22` reference stale — separate constitution refresh, out of scope.

## 8. Open questions / SME decisions

| # | Question | Who can answer | Assumption if unanswered before Task Creation |
|---|---|---|---|
| 1 | Downgrade behavior after `tokenRequests.type: Managed`? | API/product SME | Document as known gap in `implementation-report.md`; no operator special-case. |
| 2 | Is fork branch ready for upstream PR, or delta remains vs `openshift/secrets-store-csi-driver-operator` main? | Repo maintainer / PR author | Phase 5 includes explicit upstream diff review before merge. |
| 3 | Is `serviceaccounts/token: create` RBAC sufficient/unrelated for WIF? | Upstream driver maintainer | No RBAC change unless Phase 3 finds evidence. |
| 4 | Will `driverConfig.secretsStore` ship behind TechPreview FeatureGate? | `openshift/api` architects | Assume API-server-side gating only; no operator gate check. |
