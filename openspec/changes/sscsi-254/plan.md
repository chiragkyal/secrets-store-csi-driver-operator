# Technical Implementation Plan
**Feature:** SSCSI-254 — Configurable Secret Rotation and Workload Identity Federation

---

## 0. Inputs Acknowledged

| Input | Status |
|-------|--------|
| Spec source | SSCSI-254 / EP [openshift/enhancements#2012](https://github.com/openshift/enhancements/pull/2012) |
| Repo assessment pin | github.com/openshift/secrets-store-csi-driver-operator, branch `main`, working-folder mode (tooling_status: READY) |
| `agents.md` | PROVIDED — `openspec/schemas/openspec-agile-workflow/agents.md` |
| `constitution.md` | PROVIDED — 7 evidence-backed principles, AgentRoutingMode: PROVIDED |
| `validation.json` | PROVIDED — score 88%, PASS, no blockers |
| AgentRoutingMode | **PROVIDED** — agent IDs from agents.md used throughout |

---

## 1. Architectural Strategy

### Approach

SSCSI-254 extends the existing `CSIControllerSet` chain in `pkg/operator/starter.go` with two new hook mechanisms — a dynamic `AssetFunc` for the `CSIDriver` object and a `DaemonSetHookFunc` for rotation container args — both reading configuration from the `ClusterCSIDriver` operator resource at reconcile time. No new controllers, managers, packages, or CRD types are introduced in this repo.

The feature surface is split across two repositories:
- **openshift/api** (PR #2846): defines the new `SecretsStore` discriminated union in `CSIDriverConfigSpec`, with `SecretRotation` and `TokenRequests` sub-structs, CEL immutability rules, and field validation.
- **this repo**: reads the new API fields via the operator client and propagates them to the `CSIDriver` object and DaemonSet during reconciliation.

This is a clean extension of the existing library-go pattern: the `ConditionalStaticResourcesController` already accepts an `AssetFunc` per asset and `WithCSIDriverNodeService` already accepts variadic `DaemonSetHookFunc` arguments. Both extension points are designed for exactly this use case.

### Repo-Grounded Reality Check

**All SSCSI-254 components are greenfield — none exist on the current branch.**

Per `repo-assessment.md §11.1`:
- Dynamic `AssetFunc` for `csidriver.yaml`: NOT present — greenfield
- `DaemonSetHookFunc` for rotation args: NOT present — greenfield
- `ClusterCSIDriver` informer wired to `NodeService` controller: NOT present (`nil` passed today)
- `ClusterCSIDriver.spec.driverConfig.secretsStore`: NOT present — depends on openshift/api PR #2846

No phase should be framed as "verify" or "harden" existing code. All operator-side work is new implementation following the existing CSIControllerSet hook pattern documented in `repo-assessment.md §4.2–4.5`.

### CSI-Specific Concerns (from agents.md Planning Stage Hints)

- **requiresRepublish coupling**: When `secretRotation.type: None`, both `requiresRepublish: false` (on CSIDriver) and `--enable-secret-rotation=false` (on DaemonSet) must be set together — a partial state where one is updated and the other is not would create inconsistent driver behavior.
- **CSIDriver immutability**: `CSIDriver.spec` is effectively immutable in Kubernetes; `resourceapply.ApplyCSIDriver` handles this via spec-hash annotation and delete+recreate. Any change to `requiresRepublish` or `tokenRequests` triggers a brief window where the object does not exist; this is expected and documented in the EP.
- **tokenRequests preservation**: The Unmanaged default must read the *live* `CSIDriver.spec.tokenRequests` from the cluster to include in the desired spec, preventing hash change on upgrade. This requires a live cluster read, not just the local desired state.
- **DaemonSet rolling update**: Changing container args triggers a DaemonSet rolling update (`maxUnavailable: 10%`). The operator does not need special handling — the CSI node driver remains available during the rollout via the rolling update strategy.
- **No provider volume path changes**: SSCSI-254 does not change provider mount paths or socket paths in `node.yaml`.

---

## 2. Persistence & State

**Kubernetes objects (source-of-truth):**

| Object | Role | Ownership |
|--------|------|-----------|
| `ClusterCSIDriver` `secrets-store.csi.k8s.io` | Source of truth for all operator config including new rotation + WIF fields | Cluster admin writes; operator reads |
| `CSIDriver` `secrets-store.csi.k8s.io` | Derived: `spec.requiresRepublish` + `spec.tokenRequests` set by operator | Operator owns via `resourceapply.ApplyCSIDriver` (hash annotation) |
| DaemonSet `secrets-store-csi-driver-node` | Derived: `--enable-secret-rotation` + `--rotation-poll-interval` args set by hook | Operator owns via `CSIDriverNodeServiceController` |

**Operand config/state (existing — no changes to baseline args or defaults):**

| Field | Current source | After SSCSI-254 |
|-------|---------------|----------------|
| `--enable-secret-rotation` | Hardcoded `true` in `assets/node.yaml` | Set by `DaemonSetHookFunc` from `ClusterCSIDriver`; defaults to `true` when config absent |
| `--rotation-poll-interval` | Hardcoded `2m` in `assets/node.yaml` | Set by hook from `minimumRefreshAge`; defaults to `2m` when config absent |
| `CSIDriver.spec.requiresRepublish` | Not set (absent) in `assets/csidriver.yaml` | Set by dynamic `AssetFunc`; defaults to `true` when config absent |
| `CSIDriver.spec.tokenRequests` | Not set in `assets/csidriver.yaml` | Set by dynamic `AssetFunc`; defaults to preserving live cluster value when unmanaged |

**External/platform-injected state:** CNO continues to inject the trusted CA bundle into `cabundle_cm.yaml` — no changes to this path from SSCSI-254.

---

## 3. Interfaces & Contracts

### 3.1 Kubernetes APIs (CRDs/CRs)

**Source repo: openshift/api (PR #2846) — must land before this operator compiles against new fields.**

New fields added to `ClusterCSIDriver.spec.driverConfig` (discriminated union under `CSIDriverConfigSpec`):

| Field | Type | Validation | Immutability |
|-------|------|-----------|-------------|
| `driverType: SecretsStore` | enum | Must be `SecretsStore` for `secrets-store.csi.k8s.io` (CEL) | — |
| `secretsStore.secretRotation.type` | `None\|Custom` | Required when `secretsStore` present | — |
| `secretsStore.secretRotation.custom.minimumRefreshAge` | `int32` (1–31560000) | `+kubebuilder:validation:Minimum=1,Maximum=31560000` | — |
| `secretsStore.tokenRequests.type` | `Managed\|Unmanaged` | Required when `tokenRequests` present | Immutable once set to `Managed` (top-level CEL) |
| `secretsStore.tokenRequests.managed.audiences` | `[]TokenRequest` (max 10) | `+listType=map, +listMapKey=audience`; `expirationSeconds` 600–315360000 | — |

CEL immutability rule (at `ClusterCSIDriver` level, per EP):
```
oldSelf.spec.?driverConfig.?secretsStore.?tokenRequests.?type.orValue('') != 'Managed' ||
self.spec.?driverConfig.?secretsStore.?tokenRequests.?type.orValue('') == 'Managed'
```

### 3.2 Controller/Runtime Interfaces (Internal)

All internal logic lives in `pkg/operator/starter.go`. New functions to introduce:

| Function | Purpose | Used by |
|----------|---------|---------|
| `getRotationConfig(operatorClient)` | Extract `(enabled bool, interval string)` from `ClusterCSIDriver.spec.driverConfig.secretsStore.secretRotation`, applying built-in defaults when nil | DaemonSet hook and CSIDriver AssetFunc |
| `getTokenRequests(operatorClient, dynamicClient)` | Extract desired `[]storagev1.TokenRequest` — Managed: from CR audiences list; Unmanaged: from live CSIDriver object | CSIDriver AssetFunc |
| `enrichedCSIDriverAssetFunc(operatorClient, dynamicClient, namespace)` | Return `resourceapply.AssetFunc` — for `csidriver.yaml` calls enrichment; for all others calls `replaceNamespaceFunc` | `WithConditionalStaticResourcesController` |
| `rotationArgsDaemonSetHook(operatorClient)` | Return `csidrivernodeservicecontroller.DaemonSetHookFunc` — sets `--enable-secret-rotation` and `--rotation-poll-interval` on `csi-driver` container | `WithCSIDriverNodeService` |

**`requiresRepublish` mapping rule** (from EP resolved open question):
- `secretRotation.type: None` → `requiresRepublish: false`
- `secretRotation` absent or `type: Custom` → `requiresRepublish: true`

### 3.3 Webhooks / Admission

N/A — All validation is enforced via CEL rules in openshift/api at the CRD admission layer. No webhook server is added to this operator (constitution Principle VII).

### 3.4 RBAC / Security Boundaries

No new RBAC is required in this operator for the rotation or tokenRequests configuration features. The existing `secretproviderclasses_role.yaml` already grants `serviceaccounts/token` (create) verbs, which covers the SA token projection mechanism.

**To confirm during Phase 1:** verify that the operator service account does not need additional permissions to read the live `CSIDriver.spec.tokenRequests` (it manages this object, so it should already have `get` on `csidrivers`).

### 3.5 Packaging / OLM

The OLM CSV (`config/manifests/stable/*.clusterserviceversion.yaml`) should be updated to reflect the new configurable fields in `alm-status-descriptors`. This is a documentation/metadata concern and does not gate the functional implementation. Managed via `make metadata` and manual CSV edits — separate from operator logic changes.

---

## 4. Dependencies & Sequencing Graph

**Critical path:**

```
openshift/api PR #2846 (merged + go.mod updated)
  └─→ Config extraction helpers (getRotationConfig, getTokenRequests)
        ├─→ Dynamic CSIDriver AssetFunc (enrichedCSIDriverAssetFunc)
        └─→ DaemonSet rotation args hook (rotationArgsDaemonSetHook)
              └─→ Informer wiring (replace nil with dynamicInformers in WithCSIDriverNodeService)
                    └─→ Unit tests (full §8.4 inventory)
                          ├─→ OLM/CSV update (parallel with E2E)
                          └─→ E2E tests (parallel with OLM)
```

**Parallelizable workstreams (once Phase 2 is complete):**
- Dynamic AssetFunc (Phase 3) and DaemonSet hook (Phase 4) can be developed in parallel since they are independent helper functions — both land in `starter.go` but do not call each other.
- OLM/CSV update (Phase 6) and E2E test authoring (Phase 7) can proceed in parallel after unit tests pass.

**External blockers:**

| Blocker | Owner | Resolution |
|---------|-------|-----------|
| openshift/api PR #2846 not merged | openshift/api maintainers | Gate Phase 1 on merge; use local `replace` directive in `go.mod` during parallel development |

---

## 5. Implementation Phases

### Phase 1: API Availability and Build Baseline

- **Goal:** Ensure `openshift/api` types for `SecretsStoreCSIDriverConfigSpec`, `SecretsStoreSecretRotation`, `SecretsStoreTokenRequests`, and related types are available in `go.mod`. Verify `go build ./...` passes against the new API. Confirm no new RBAC is needed for the live CSIDriver read.
- **Dependencies:** openshift/api PR #2846 merged (or local `replace` directive for parallel development).
- **Target files:** `go.mod`, `go.sum` (dependency update only)
- **Required capabilities:** OperatorController_Agent (dependency update and build verification)
- **Verification hooks:**
  - `go build ./...` passes with new openshift/api types imported
  - `make check` green on baseline (no regressions before new code)
  - Confirm `pkg/operator/starter.go` can import and reference new `opv1.ClusterCSIDriver.Spec.DriverConfig.SecretsStore` without compile error

---

### Phase 2: Config Extraction Helper Functions

- **Goal:** Implement the two helper functions in `starter.go` that translate `ClusterCSIDriver.spec.driverConfig.secretsStore` into the concrete rotation config and tokenRequests desired state. These are pure logic functions that can be unit-tested independently before being wired into the CSIControllerSet hooks.
  - `getRotationConfig`: returns `(requiresRepublish bool, enableRotation bool, pollInterval string)` with full nil-handling chain (absent driverConfig → absent secretsStore → absent secretRotation → built-in defaults of `true`, `true`, `"2m"`).
  - `getTokenRequests`: returns `[]storagev1.TokenRequest` — when Unmanaged or nil, reads live `CSIDriver.spec.tokenRequests` via dynamic client; when Managed, converts `managed.audiences` list.
- **Dependencies:** Phase 1 complete (openshift/api types available).
- **Target files:** `pkg/operator/starter.go` (new helper functions)
- **Required capabilities:** OperatorController_Agent via `api-implement`
- **Verification hooks:**
  - `go build ./... && go vet ./...`
  - Unit tests for all nil-path combinations (see Phase 5 for full test inventory)

---

### Phase 3: Dynamic CSIDriver AssetFunc

- **Goal:** Replace the single `replaceNamespaceFunc` passed to `WithConditionalStaticResourcesController` with `enrichedCSIDriverAssetFunc` — a new `AssetFunc` that:
  1. For `"csidriver.yaml"`: reads base YAML, deserializes to `storagev1.CSIDriver`, calls `getRotationConfig` and `getTokenRequests`, sets `spec.requiresRepublish` and `spec.tokenRequests`, serializes back to JSON bytes.
  2. For all other assets: applies `${NAMESPACE}` substitution as before (behavior unchanged).
  The function must handle the case where `getTokenRequests` needs to read the live CSIDriver from the cluster (Unmanaged path) — this requires the dynamic client to be in scope.
- **Dependencies:** Phase 2 complete (helper functions available).
- **Target files:** `pkg/operator/starter.go` (`enrichedCSIDriverAssetFunc` + updated `WithConditionalStaticResourcesController` call)
- **Required capabilities:** OperatorController_Agent via `api-implement`
- **Verification hooks:**
  - `go build ./... && go vet ./...`
  - Unit tests: dynamic AssetFunc returns correct JSON for all rotation/tokenRequests combinations; namespace substitution still applied to non-CSIDriver assets

---

### Phase 4: DaemonSet Rotation Args Hook and Informer Wiring

- **Goal:** Add the rotation args hook to `WithCSIDriverNodeService` and wire the `ClusterCSIDriver` informer so DaemonSet reconciliation triggers on CR changes:
  1. Implement `rotationArgsDaemonSetHook(operatorClient)` — returns a `DaemonSetHookFunc` that calls `getRotationConfig`, finds the `csi-driver` container by name, and replaces `--enable-secret-rotation=` and `--rotation-poll-interval=` args by prefix match.
  2. Add the hook as a second variadic argument to `WithCSIDriverNodeService` (after the existing CA bundle hook).
  3. Replace `nil` with `dynamicInformers` as the optional informers argument so that `ClusterCSIDriver` changes immediately trigger DaemonSet reconciliation without waiting for the 20-minute resync.
- **Dependencies:** Phase 2 complete (helper functions). May be developed in parallel with Phase 3.
- **Target files:** `pkg/operator/starter.go` (`rotationArgsDaemonSetHook` + updated `WithCSIDriverNodeService` call)
- **Required capabilities:** OperatorController_Agent via `api-implement`
- **Verification hooks:**
  - `go build ./... && go vet ./...`
  - Unit tests: hook sets correct args; hook returns error when `csi-driver` container not found; no-op when config absent (same values as baseline)

---

### Phase 5: Unit Tests

- **Goal:** Full unit test coverage for all new functions in `starter.go`, following the library-go fake pattern from `starter_test.go`. Tests must cover all nil-path permutations to verify upgrade safety.
- **Dependencies:** Phases 2–4 complete (all new functions implemented).
- **Target files:** `pkg/operator/starter_test.go`
- **Required capabilities:** Testing_Agent via `api-implement`
- **Verification hooks:**
  - `go test ./pkg/... ./cmd/... -v -count=1` — all new test cases green
  - `make check` — verify + unit suite passes

  **Required test cases** (from `repo-assessment.md §8.4`):

  | Test | Expected outcome |
  |------|-----------------|
  | nil `driverConfig` → rotation config | `requiresRepublish=true`, enable=true, interval=`"2m"` |
  | nil `secretsStore` → same | Same defaults |
  | `secretRotation.type: None` | `requiresRepublish=false`, enable=false |
  | `secretRotation.type: Custom`, `minimumRefreshAge: 300` | `requiresRepublish=true`, interval=`"5m0s"` |
  | `tokenRequests: nil` + existing live CSIDriver tokenRequests | Existing tokenRequests preserved |
  | `tokenRequests.type: Managed` + audiences | CSIDriver.spec.tokenRequests matches |
  | `tokenRequests.type: Managed` + empty audiences | tokenRequests cleared |
  | Hook: `csi-driver` container not found | Hook returns error |
  | Non-CSIDriver asset through enriched AssetFunc | `${NAMESPACE}` replaced, no enrichment |
  | `tokenRequests: nil` + no live CSIDriver | Returns nil (no error) |

---

### Phase 6: OLM / CSV Metadata Update

- **Goal:** Update the OLM CSV `alm-status-descriptors` to surface the new configurable fields (`secretRotation`, `tokenRequests`) in the OLM UI. Verify the OCP version metadata is consistent.
- **Dependencies:** Phase 5 complete (functional implementation verified). May proceed in parallel with Phase 7.
- **Target files:** `config/manifests/stable/*.clusterserviceversion.yaml` (alm-status-descriptors section)
- **Required capabilities:** OLMRelease_Agent (manual)
- **Verification hooks:**
  - `make metadata` — confirm no version field drift
  - `go build ./...` — no compile-time regressions from CSV edit

---

### Phase 7: E2E Tests

- **Goal:** Author E2E test scenarios covering the rotation and WIF scenarios from the EP test plan, verifiable on a live OpenShift cluster via `hack/e2e.sh`.
- **Dependencies:** Phase 5 complete. May proceed in parallel with Phase 6.
- **Target files:** `hack/e2e.sh` and any test fixture files (path to be confirmed during test authoring — UNVERIFIED: check whether e2e tests are inline in `e2e.sh` or in a separate Go test package)
- **Required capabilities:** Testing_Agent via `e2e-generate`
- **Verification hooks:**
  - `hack/e2e.sh` on live OpenShift cluster (cannot run locally)

  **Required E2E scenarios** (from EP test plan):

  | Scenario | What to verify |
  |----------|---------------|
  | No `driverConfig` set | Defaults: `requiresRepublish=true`, rotation enabled at 2m |
  | `secretRotation.type: None` | `requiresRepublish=false`, DaemonSet `--enable-secret-rotation=false` |
  | `secretRotation.type: Custom`, 300s | `requiresRepublish=true`, DaemonSet `--rotation-poll-interval=5m0s` |
  | Toggle None → Custom | CSIDriver and DaemonSet revert to rotation-enabled |
  | `tokenRequests.type: Managed` + audience | `CSIDriver.spec.tokenRequests` matches; kubelet provides token |
  | `tokenRequests.type: Managed` + empty | `CSIDriver.spec.tokenRequests` cleared |
  | Upgrade: minimal CR + pre-existing manual tokenRequests | Existing tokenRequests preserved; no delete+recreate |
  | Upgrade: minimal CR, no pre-existing tokenRequests | Defaults maintained; no DaemonSet rolling update |
  | Multi-cloud: AWS + Azure audiences simultaneously | Both tokenRequests entries present on CSIDriver |

---

## 6. Verification Matrix

| Category | Coverage | Files / Suites |
|----------|----------|----------------|
| Unit | All nil-path permutations for `getRotationConfig`, `getTokenRequests`; DaemonSet hook arg replacement; dynamic AssetFunc output; error on missing container | `pkg/operator/starter_test.go` |
| Integration (API CEL) | tokenRequests immutability (Managed→Unmanaged rejected); discriminated union validation; minimumRefreshAge range enforcement; expirationSeconds range enforcement | openshift/api testsuite (out-of-scope for this repo) |
| E2E | Rotation enable/disable; custom interval; multi-cloud audiences; upgrade with pre-existing tokenRequests; toggle scenarios | `hack/e2e.sh` (live cluster) |
| Manual / Cluster | Verify `oc get csidriver secrets-store.csi.k8s.io -o yaml` shows correct `requiresRepublish` + `tokenRequests`; verify DaemonSet args via `oc get ds` | Runbook from EP §Support Procedures |
| N/A | No API integration tests in this repo (CEL validation lives in openshift/api) | — |

**Spec FR → Phase coverage:**

| FR | Phase(s) |
|----|---------|
| FR-001 (disable rotation) | 2, 4 |
| FR-002 (configure interval) | 2, 4 |
| FR-003 (propagate to driver args) | 4 |
| FR-004 (propagate to CSIDriver requiresRepublish) | 2, 3 |
| FR-005 (configure token audiences) | 2, 3 |
| FR-006 (multiple audiences) | 2, 3 |
| FR-007 (preserve existing tokenRequests) | 2, 3 |
| FR-008 (Managed irreversible) | 1 — enforced in openshift/api CEL; operator reads the field value |
| FR-009 (built-in defaults on upgrade) | 2, 3, 4 |
| FR-010 (validate rotation interval) | 1 — enforced in openshift/api CEL |
| FR-011 (validate token expiration) | 1 — enforced in openshift/api CEL |
| FR-012 (degraded status on failure) | 3, 4 — library-go controllers set Degraded automatically on hook errors |

---

## 7. Risks, Migrations, and Operational Follow-ups

**Upgrade / migration (highest priority):**

The `tokenRequests` preservation on upgrade is the most critical migration risk. When this operator upgrades:
1. `ClusterCSIDriver` in etcd has no `driverConfig` field — nil at every level.
2. Operator adds `requiresRepublish: true` to the desired `CSIDriver` spec — this changes the spec hash.
3. Without the `getTokenRequests` Unmanaged path, any manually-patched `tokenRequests` on the live CSIDriver would be wiped by the delete+recreate.

Mitigation: `getTokenRequests` must read the live cluster CSIDriver and include existing `tokenRequests` in the desired spec before computing the hash. This is the Unmanaged default path and is explicitly tested in Phase 5.

**CSIDriver delete+recreate window:**

Any hash change triggers `resourceapply.ApplyCSIDriver` to delete and recreate the `CSIDriver`. During this window (typically sub-second) the object does not exist. Running pods are unaffected (they use the mounted volume), but new pod mounts may fail. The EP notes this window is negligible in practice. No additional mitigation is planned.

**DaemonSet rolling update:**

Changing rotation args triggers a rolling update at `maxUnavailable: 10%`. On large clusters, this may take several minutes. No disruption to running pods (CSI is read-only for existing mounts). No migration needed.

**openshift/api merge dependency:**

If PR #2846 is delayed, the operator cannot reference the new types. Mitigation: use a local `replace` directive in `go.mod` during development. Remove before PR submission.

**MicroShift compatibility:**

This feature is not applicable to MicroShift (secrets-store-csi-driver-operator is not available there). No MicroShift-specific handling required.

**Hypershift:**

Not applicable per the EP — Hypershift-specific behavior is out of scope.

**Upstream API drift:**

The upstream Secrets Store CSI Driver v1.6.0 introduced `requiresRepublish`-based rotation. If a future upstream version changes the rotation mechanism, the mapping in `getRotationConfig` may need updating. No immediate risk.

**`alm-status-descriptors` gap:**

If Phase 6 (OLM CSV update) is skipped or delayed, the new fields will not appear in the OLM UI but will be functional. This is a documentation concern only — no functional risk.

---

## 8. Open Questions / SME Decisions

| # | Question | Owner | Plan assumes if unresolved |
|---|---------|-------|--------------------------|
| 1 | **openshift/api PR #2846 merge timeline**: When will the types be available? Is a `go.mod replace` directive acceptable during development? | openshift/api maintainers / team lead | Local `replace` used for development; removed before final PR. |
| 2 | **Live CSIDriver read for tokenRequests preservation**: Should `getTokenRequests` use the dynamic client directly or obtain a typed lister via `dynamicInformers`? The lister approach avoids API server round-trips on every reconcile but requires informer setup. | OperatorController_Agent / code review | Use `operatorClient.GetOperatorState()` for the ClusterCSIDriver fields; for live CSIDriver tokenRequests, use a direct `GET` via `kubeClient` on the initial call since reconciles are infrequent. |
| 3 | **E2E test location**: Are e2e tests inline in `hack/e2e.sh` or in a separate Go test package under `test/`? The `hack/e2e.sh` script delegates to `openshift-tests` — clarify whether new test cases require a separate file. | Testing_Agent / codebase inspection during Phase 7 | Testing_Agent will inspect `hack/e2e.sh` content at task start to determine correct location (UNVERIFIED per repo-assessment §11.1). |
| 4 | **CSV `alm-status-descriptors` scope**: Which of the new `secretsStore` sub-fields should appear in the OLM UI descriptors? All fields, or only top-level (`secretRotation`, `tokenRequests`)? | OLMRelease_Agent / product team | Add descriptors for `secretRotation` and `tokenRequests` at the top level; sub-fields follow naturally in the UI. |
