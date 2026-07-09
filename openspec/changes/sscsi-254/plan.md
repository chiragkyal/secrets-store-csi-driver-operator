# Technical Implementation Plan
**Feature:** Configurable Secret Rotation and Workload Identity Federation for the Secrets Store CSI Driver

## 0. Inputs acknowledged

| Input | Status |
|-------|--------|
| Spec source | SSCSI-254 — `specs.md` (approved, 3 user stories: P1 rotation control, P1 WIF token audiences, P2 upgrade-safety preservation) |
| Repo assessment pin | `https://github.com/openshift/secrets-store-csi-driver-operator.git`, branch `openspec-ai-helpers-composer`, commit `953f4aee6f71a886390db4fc1e7aa931f450bb93` (tooling_status: OK / FULL — working-folder mode, direct checkout access) |
| `agents.md` | **PROVIDED** — resolved to the target repo's root `AGENTS.md`. This file is comprehensive repo documentation but contains **no discrete Agent-ID capability taxonomy** (no `API_Agent`/`OperatorController_Agent`-style routing table). Per the resolved `constitution.md`'s own `AgentRoutingMode: PROVIDED` declaration and its evidence-grounded **Code Ownership** table (§"Code Ownership" — Controller logic / Static assets / OLM-release / Tests / Docs, each with concrete key paths), this plan uses that table as the concrete capability set for phase routing below, rather than the generic provisional taxonomy. This substitution is flagged explicitly here and again in §8 — it is the most repo-grounded option available, but is not a strict "AGENTS.md-defined Agent ID" match and should be reconciled if the user later wants formal `tasks.md` Agent-ID enforcement. |
| `spec_validator_results.json` | PROVIDED — `validation.json`, 93% PASS, 0 blockers, 3 non-blockers (all resolved as `specs.md` Assumptions A-004/A-005/A-006). |
| `constitution.md` | PROVIDED (not placeholder) — resolved from `openspec/inputs/constitution.md`, 10 numbered principles + Additional Constraints + Code Ownership table, fully evidence-cited against this repo. `AgentRoutingMode: PROVIDED`. |

## 1. Architectural strategy

This feature extends the operator's **existing, single-pattern architecture** (library-go `CSIControllerSet`, Constitution Principle I) — no new controller framework, no new CRD, no new manager. The `ClusterCSIDriver` singleton (`secrets-store.csi.k8s.io`) remains the sole configuration surface (Principle III), extended with a new `SecretsStore` variant on the existing `CSIDriverConfigSpec` discriminated union — the same structural pattern already used by `AWSCSIDriverConfigSpec`/`AzureCSIDriverConfigSpec`/etc. Two existing library-go extension points absorb the new behavior: the `ConditionalStaticResourcesController`'s `AssetFunc` (for the `CSIDriver` object's `requiresRepublish`/`tokenRequests` fields) and a new `csidrivernodeservicecontroller.DaemonSetHookFunc` (for the DaemonSet's rotation args) — both are extension mechanisms the operator already uses today (Principle I, II).

**Repo-grounded reality check (greenfield vs. delta)**: This is a **mixed** effort with one hard **greenfield, cross-repository blocker**. `repo-assessment.md` §0/§1.3/§2/§11 confirms — by reading the vendored source directly, not by trusting the EP's assumptions — that `vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go`'s `CSIDriverType` enum is `"";AWS;Azure;GCP;IBMCloud;vSphere` **with no `SecretsStore` value**, and `CSIDriverConfigSpec` has no `SecretsStore` field. This is not an "existing feature needs hardening" situation — the API surface this feature depends on does not exist yet in this repo's dependency graph, and this repo cannot add it directly (`repo-assessment.md` §2, §7). Everything downstream of that API extension (the read path, the `AssetFunc`, the `DaemonSetHookFunc`) is **greenfield within this repo too** — `repo-assessment.md` §1.3/§4.2/§11 confirms **no existing code path reads `ClusterCSIDriver.Spec.DriverConfig` today**, and **zero existing unit test coverage** touches anything this feature needs (§8.4). The one delta/reuse element is the DaemonSet-hook *pattern* itself — `WithCABundleDaemonSetHook` (`repo-assessment.md` §5) is a proven, working exemplar to structurally copy, even though the specific rotation-args hook is net-new code.

## 2. Persistence & state

- **Kubernetes objects (source of truth vs. derived)**:
  - `ClusterCSIDriver` (`secrets-store.csi.k8s.io`) — **source of truth**, administrator-edited. New fields live under `spec.driverConfig.secretsStore.{secretRotation,tokenRequests}` (external API addition, §3.1).
  - `CSIDriver` (`storage.k8s.io/v1`, name `secrets-store.csi.k8s.io`) — **derived/reconciled**. `spec.requiresRepublish` and `spec.tokenRequests` become dynamically generated from `ClusterCSIDriver` instead of the fully static manifest today (`assets/csidriver.yaml`). Per `resourceapply.ApplyCSIDriver` (already vendored, `repo-assessment.md` §5), changes to this object's spec are applied via **delete+recreate** (annotation-based spec-hash), not in-place patch — this is existing library-go behavior, not new code.
  - `DaemonSet` (`secrets-store-csi-driver-node`) — **derived/reconciled**. `--enable-secret-rotation`/`--rotation-poll-interval` args become hook-set instead of the two fully-hardcoded flag values in `assets/node.yaml:45-46` today.
- **Operand config/state**: No new ConfigMaps/Secrets are introduced. No bindata/codegen pipeline exists in this repo (`repo-assessment.md` §6 "Code Generation" guardrail) — `assets/csidriver.yaml`/`assets/node.yaml` remain hand-edited plain YAML per Constitution Principle II.
- **External/platform-injected state**: N/A beyond the existing CA-bundle ConfigMap mechanism (Constitution Principle VIII), which is unaffected by this feature and MUST be preserved unchanged when the new DaemonSet hook is added (hooks are additive/variadic, not replacing).
- **Upgrade-safety state (User Story 3)**: the operator must read the **live** `CSIDriver` object's existing `spec.tokenRequests` (not just the desired `ClusterCSIDriver` config) to preserve pre-existing, externally-configured audiences until an administrator opts in to `Managed` mode. This is a new read-before-write pattern with no precedent elsewhere in this codebase (`repo-assessment.md` §11 risk) — it must be scoped as its own well-tested unit, not folded silently into the general config-mapping logic.

## 3. Interfaces & contracts (operator-native)

### 3.1 Kubernetes APIs (CRDs/CRs)

- **External dependency (blocking, not implementable in this repo)**: `github.com/openshift/api`'s `operator/v1` package must add:
  - `SecretsStoreDriverType CSIDriverType = "SecretsStore"` (new enum value alongside `AWS`/`Azure`/`GCP`/`IBMCloud`/`VSphere`).
  - `SecretsStore *SecretsStoreCSIDriverConfigSpec` field on `CSIDriverConfigSpec`, following the existing per-driver-type pointer-field pattern (Constitution Principle III evidence: `types_csi_cluster_driver.go:131-159`).
  - `SecretsStoreCSIDriverConfigSpec` with `secretRotation`/`tokenRequests` discriminated-union sub-fields, immutability CEL rule for `tokenRequests.type` once `Managed` (per `specs.md` FR-007), and validation bounds (rotation interval, audience count/length, token validity duration per `specs.md` FR-005).
  - This CRD is **not owned or generated by this repo** — no `make generate`/`make manifests` exists here for it (Constitution Principle III: "No Custom CRD Types"). This repo only consumes the vendored Go types once available.
- **Immutability/validation enforcement**: entirely at the API layer (CEL rules in `openshift/api`), not in this operator's Go code — this repo has no admission-webhook or validation-allowlist code today (`repo-assessment.md` §4.1) and this feature does not introduce one.
- **`CSIDriver` (storage.k8s.io/v1)**: not owned by this repo either (upstream Kubernetes type), but its `spec.requiresRepublish`/`spec.tokenRequests` fields become the operator's dynamic-write target (§2).

### 3.2 Controller/runtime interfaces (internal)

- **New shared read-path helper** (name/location TBD at task-creation time; `repo-assessment.md` §4.2/§11 confirms no existing equivalent) — a small internal component responsible for: fetching the live `ClusterCSIDriver` (typed client or informer/lister — open question, §8), extracting `driverConfig.secretsStore.{secretRotation,tokenRequests}` with full nil-safety across every level (`driverConfig` absent → `driverType != SecretsStore` → `secretsStore` nil → sub-field nil), and returning resolved rotation/token-audience values with built-in defaults matching today's hardcoded behavior (rotation enabled, 2-minute interval, no audiences). This helper is used by **both** interfaces below — do not duplicate the read/nil-safety logic in two places (`repo-assessment.md` §11 mitigation).
- **New `AssetFunc` wrapper for `csidriver.yaml`**: replaces the current byte-level-only `replaceNamespaceFunc` usage for this one file — reads `ClusterCSIDriver` via the shared helper, additionally reads the **live** `CSIDriver` object's existing `tokenRequests` (for preservation, §2), then sets `requiresRepublish`/`tokenRequests` on the decoded manifest before re-serializing. Registered in place of (or wrapping) the existing entry in the `WithConditionalStaticResourcesController(...)` file list in `pkg/operator/starter.go:79-100`.
- **New `DaemonSetHookFunc`**: follows the `WithCABundleDaemonSetHook` structural pattern (`repo-assessment.md` §5) — reads rotation config via the shared helper, sets/replaces `--enable-secret-rotation=`/`--rotation-poll-interval=` by flag-prefix match on the `csi-driver` container, preserving the existing hardcoded defaults when config is unset (upgrade safety). Registered as an additional variadic argument to `WithCSIDriverNodeService(...)` in `pkg/operator/starter.go:104-116`, alongside (not replacing) the existing CA-bundle hook.
- **Status/health**: no new condition types — continues using the single existing `OperatorStatus` system (`Available`/`Progressing`/`Degraded`) via the generic operator client (`repo-assessment.md` §4.4). No custom error-classification system exists in this repo and this feature does not introduce one.

### 3.3 Webhooks / admission

N/A — this operator has no webhook/admission code (no controller-runtime, `repo-assessment.md` §1.1/§1.3), and validation for the new fields is fully delegated to `openshift/api`'s CEL rules (§3.1). No webhook work is required in this repo.

### 3.4 RBAC / security boundaries

- Per `repo-assessment.md` §6/§7/§11 and Constitution Principle VI: RBAC for `clustercsidrivers` (get/list/watch/update/patch) and `csidrivers` (storage.k8s.io — full create/get/list/watch/update/delete) **already covers** everything this feature is currently expected to need. **No new RBAC is planned by default.**
- Contingent verification step (§8 open question): if the shared read-path helper (§3.2) uses a mechanism other than the already-permitted `clustercsidrivers` get/list/watch, re-verify RBAC verbs against that specific access pattern before implementation (Constitution Principle VI: RBAC is asset-driven in `assets/rbac/`, never granted inline/dynamically — any genuinely new verb must be added there **and** mirrored in the CSV per `repo-assessment.md` §7 "two independent sources of truth" finding).
- No secrets/cluster-scoped-write blast-radius change: this feature does not touch `assets/rbac/secretproviderclasses_role.yaml`'s secrets permissions — it only reads/writes `ClusterCSIDriver` (already permitted) and `CSIDriver` (already permitted, cluster-scoped but not a Secret).

### 3.5 Packaging / OLM

Per `repo-assessment.md` §10.6: **no CSV/bundle changes are anticipated** — no new owned CRDs, no new `relatedImages`, no new install modes. If the RBAC verification in §3.4 surfaces a genuine gap, the CSV RBAC block (`config/manifests/stable/secrets-store-csi-driver-operator.clusterserviceversion.yaml`) would need a matching update (Constitution Principle IX governs version-bump mechanics only, not RBAC — RBAC edits there are manual per `repo-assessment.md` §7). No feature-gate/TechPreview marker is needed — Constitution "No feature gates" constraint and `specs.md` A-002 both confirm this targets GA directly, and any TechPreview gating for the API field itself would live in `openshift/api`'s own FeatureGate system, not here.

## 4. Dependencies & sequencing graph

**Critical path summary**:
1. `github.com/openshift/api` PR adding `SecretsStore` to `CSIDriverConfigSpec` merges and is tagged (**external, blocking, not sequenced by this repo**).
2. This repo bumps `go.mod`/`go.sum`/`vendor/` to consume it (Phase 1).
3. Shared read-path helper is built (Phase 2) — this is the single dependency both consumer phases need.
4. `CSIDriver` dynamic-generation (Phase 3) and DaemonSet rotation hook (Phase 4) can each start once Phase 2 lands.
5. Unit tests (Phase 5) proceed alongside Phases 3–4 (test-as-you-build, per Constitution Principle V "Verification-First").
6. RBAC verification (Phase 6) is a short confirmation step, not blocking implementation, but must complete before PR submission.
7. E2E scenarios (Phase 7) require Phases 3–4 complete and a live cluster.
8. Docs (Phase 8) can proceed any time after Phase 3/4 behavior is stable enough to describe accurately.

**Parallelizable workstreams**: Once Phase 2 (shared read-path helper) is done, Phase 3 (`CSIDriver` generation) and Phase 4 (DaemonSet hook) touch different files (`starter.go`'s `WithConditionalStaticResourcesController` call vs. its `WithCSIDriverNodeService` call, plus separate new hook/helper files) and can proceed concurrently. Phase 8 (docs) can run in parallel with Phases 5–7 once behavior is stable.

**Explicit blockers / external dependencies**:
- **Hard blocker**: the `openshift/api` PR (owner: API approvers, `@JoelSpeed` per the EP frontmatter) — this repo's Phase 1 cannot start (beyond speculative design) until it merges and is tagged. `repo-assessment.md` §11 recommends confirming timeline with API approvers before committing to a delivery date; a local `go.mod` `replace` directive against a fork/branch may unblock **development-time iteration** (Phases 2–5 design/coding) but must not be merged.
- No other cross-team/cross-repo dependencies identified.

## 5. Implementation phases (logical sequence; NOT tasks)

### Phase 1: Vendor the Upstream API Extension

- **Goal:** Consume the new `SecretsStore` driver-type API surface once it lands in `github.com/openshift/api`.
- **Dependencies:** External `openshift/api` PR merged and tagged (§4 hard blocker). May use a temporary `go.mod` `replace` directive against the upstream branch for earlier local iteration, removed before merge.
- **Target files:** `go.mod`, `go.sum`, `vendor/modules.txt`, `vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go` (regenerated via `go mod vendor`, never hand-edited — Constitution Principle X).
- **Required capabilities:** `ControllerLogic` (Constitution Code Ownership: `pkg/operator/starter.go` owner also handles the dependency bump in practice, since nothing in `pkg/` compiles against the new types until this lands) — see §0 for the capability-taxonomy substitution note.
- **Verification hooks:** `go mod tidy && go mod vendor && make verify` (confirms the vendored tree is consistent); no unit test applies to this phase itself.

### Phase 2: Shared `ClusterCSIDriver.Spec.DriverConfig` Read Path

- **Goal:** Build the single, shared helper (§3.2) that both the `CSIDriver` `AssetFunc` and the DaemonSet hook use to read and nil-safely resolve `driverConfig.secretsStore.*`, with defaults matching today's hardcoded behavior.
- **Dependencies:** Phase 1 (needs the new vendored types to compile against).
- **Target files:** New file under `pkg/operator/` (exact name UNVERIFIED — `repo-assessment.md` §11.1 flags that no existing file in this package serves this purpose today; a discovery/naming step is needed at task creation, e.g. `pkg/operator/secretsstoreconfig.go`); `pkg/operator/starter.go` (wiring the new informer/typed-client access — §8 open question on exact mechanism).
- **Required capabilities:** `ControllerLogic` (Constitution Code Ownership: `pkg/operator/starter.go`).
- **Verification hooks:** Unit tests (new `_test.go` next to the new file, following the `v1helpers.NewFakeOperatorClientWithObjectMeta` pattern from `starter_test.go` — `repo-assessment.md` §5/§8.1); `make test-unit`.

### Phase 3: Dynamic `CSIDriver` Object Generation (Rotation + WIF Fields)

- **Goal:** Replace the fully-static application of `assets/csidriver.yaml` with a dynamic `AssetFunc` that sets `spec.requiresRepublish` (mirroring `secretRotation.type`, per `specs.md` edge cases) and `spec.tokenRequests` (from `tokenRequests.managed.audiences`, or preserved from the live object when `Unmanaged`/omitted — User Story 3).
- **Dependencies:** Phase 2 (shared read path) plus a live-object read of the current `CSIDriver` for preservation (§2).
- **Target files:** `assets/csidriver.yaml` (becomes a base template read by the new `AssetFunc`, not applied byte-for-byte); `pkg/operator/starter.go:79-100` (`WithConditionalStaticResourcesController` file/AssetFunc wiring).
- **Required capabilities:** `ControllerLogic` + `StaticAssets` (Constitution Code Ownership: `pkg/operator/starter.go` and `assets/`).
- **Verification hooks:** Unit tests for the field-mapping and preservation nil-safety cascade (specs.md FR-006/FR-007/FR-008, Edge Cases); `make test-unit`. Manual verification command from `repo-assessment.md` §12: `oc get csidriver secrets-store.csi.k8s.io -o yaml`.

### Phase 4: DaemonSet Rotation-Argument Hook

- **Goal:** Implement a new `DaemonSetHookFunc` that sets `--enable-secret-rotation=`/`--rotation-poll-interval=` on the `csi-driver` container based on `secretRotation` config, preserving the existing hardcoded defaults (`true`, `2m`) when config is unset.
- **Dependencies:** Phase 2 (shared read path).
- **Target files:** New file under `pkg/operator/` for the hook (naming/location alongside Phase 2's helper or separate — task-creation decision); `pkg/operator/starter.go:104-116` (register as additional variadic arg to `WithCSIDriverNodeService`, alongside the existing CA-bundle hook — must not replace it).
- **Required capabilities:** `ControllerLogic` (Constitution Code Ownership: `pkg/operator/starter.go`).
- **Verification hooks:** Unit tests for arg replacement-by-prefix and the "container not found" error path (mirrors `specs.md`'s Test Plan-equivalent scenarios carried over from the source EP); `make test-unit`. Manual verification command from `repo-assessment.md` §12: `oc get ds -n openshift-cluster-csi-drivers secrets-store-csi-driver-node -o jsonpath='{...args}'`.

### Phase 5: Unit Test Completion Pass

- **Goal:** Close the coverage gap identified in `repo-assessment.md` §8.4 (zero existing coverage touches anything this feature needs) with a complete table-driven suite covering: config extraction (all nil-safety branches), `CSIDriver` field mapping, preservation-on-upgrade (all 5 nil-check levels per the source EP), DaemonSet hook arg replacement and error handling.
- **Dependencies:** Phases 2–4 substantially complete (tests are written alongside, per Constitution Principle V, but this phase ensures full closure before PR submission).
- **Target files:** `pkg/operator/starter_test.go` (extend) and/or new `_test.go` files co-located with Phase 2/4's new files (Constitution/testing-guidelines: tests live alongside the code they test, same package).
- **Required capabilities:** `Testing` (Constitution Code Ownership: `pkg/operator/*_test.go`).
- **Verification hooks:** `make test-unit`; `make verify` (formatting/vet).

### Phase 6: RBAC Verification

- **Goal:** Confirm empirically that no new RBAC verbs are required once the exact Phase 2 read mechanism is finalized (§3.4/§8 open question) — or, if a gap is found, add the minimal necessary verb to both `assets/rbac/` and the CSV RBAC block.
- **Dependencies:** Phase 2 mechanism finalized.
- **Target files:** `assets/rbac/*.yaml` (only if a gap is found) and `config/manifests/stable/secrets-store-csi-driver-operator.clusterserviceversion.yaml` (only if a gap is found — these are two independent sources of truth per `repo-assessment.md` §7 and must be updated together).
- **Required capabilities:** `StaticAssets` + `OLMRelease` (Constitution Code Ownership).
- **Verification hooks:** Manual RBAC diff review (no automated CSV/RBAC-sync tooling exists in this repo, `repo-assessment.md` §7); `make verify`.

### Phase 7: E2E Test Scenarios

- **Goal:** Implement end-to-end coverage for the scenarios the source EP's Test Plan already enumerates in detail (rotation enable/disable/custom-interval, WIF single/multi-audience, upgrade preservation of pre-existing manually-patched audiences, no-`driverConfig` default-behavior parity).
- **Dependencies:** Phases 3–4 complete; requires a live OpenShift cluster.
- **Target files:** UNVERIFIED exact structure — `repo-assessment.md` §11.1 explicitly flags that e2e test file organization was not opened/enumerated at file level (only `hack/e2e.sh` and its documented behavior were confirmed). **Discovery step required at task creation**: read `hack/e2e.sh` in full before scoping these tasks.
- **Required capabilities:** `Testing` (Constitution Code Ownership: `hack/e2e.sh`).
- **Verification hooks:** `make test-e2e` (requires live cluster + `openshift-tests`/`oc` in `PATH` — not runnable in most local/CI-unit environments, per `repo-assessment.md` §8.2/§8.3).

### Phase 8: Documentation

- **Goal:** Update any user-facing documentation that references the operator's configuration surface, if the new `driverConfig.secretsStore` fields warrant it (e.g. `README.md` quick-start example).
- **Dependencies:** Phases 3–4 behavior stable.
- **Target files:** `README.md` (only if warranted — no other doc changes anticipated; `docs/*-guidelines.md` are contributor-facing conventions, not user docs, and are not expected to need edits since this feature follows existing conventions exactly, per §1).
- **Required capabilities:** `Docs` (Constitution Code Ownership: `README.md`, `must-gather/`).
- **Verification hooks:** Manual review only — no automated doc verification exists in this repo.

## 6. Verification matrix (maps to spec acceptance)

| Category | Coverage | Files / Suites |
|----------|----------|----------------|
| Unit | Config extraction nil-safety (all levels), `CSIDriver` field mapping, tokenRequests preservation-on-upgrade, DaemonSet hook arg replacement + error handling — maps to `specs.md` FR-001–FR-011 and all Edge Cases | `pkg/operator/starter_test.go` (extended) + new `_test.go` files from Phases 2/4; run via `make test-unit` |
| Integration | N/A — this repo has no separate integration test tier (`repo-assessment.md` §8.1: only unit + e2e exist) | - |
| E2E | Rotation enable/disable/custom-interval (User Story 1), WIF single/multi-audience (User Story 2), upgrade preservation of manually-patched audiences and no-`driverConfig` default parity (User Story 3) — maps to `specs.md` SC-001–SC-007 | `hack/e2e.sh`-driven suite (exact file structure UNVERIFIED — discovery step in Phase 7); run via `make test-e2e` (requires live cluster) |
| Manual / Cluster | Post-deploy verification of `CSIDriver.spec.requiresRepublish`/`tokenRequests` and DaemonSet args | `oc get csidriver secrets-store.csi.k8s.io -o yaml`; `oc get ds -n openshift-cluster-csi-drivers secrets-store-csi-driver-node -o jsonpath='{.spec.template.spec.containers[?(@.name=="csi-driver")].args}'` (both from `repo-assessment.md` §12) |
| N/A | Webhook/admission testing — no webhooks exist or are introduced (§3.3) | - |

## 7. Risks, migrations, and operational follow-ups

- **Cross-repository API dependency (highest risk, carried from `repo-assessment.md` §11):** the entire feature is blocked on an external `openshift/api` change. Impact: schedule risk outside this repo's control. Mitigation: confirm timeline with API approvers early; use a temporary `go.mod replace` for development-time iteration only (never merged).
- **No existing typed read path for `driverConfig` (carried from `repo-assessment.md` §11):** Phase 2 is genuinely new plumbing with no in-repo precedent. Impact: higher implementation risk/time than a typical "add a field, read it" change. Mitigation: build it as one shared, well-tested helper (§3.2) rather than duplicating fetch logic across the `AssetFunc` and the `DaemonSetHookFunc`.
- **`CSIDriver.spec` immutability / delete-recreate window (carried from `repo-assessment.md` §11):** every rotation/WIF config change triggers a brief window where the `CSIDriver` object doesn't exist (existing `ApplyCSIDriver` behavior, not new). Impact assessed as low (kubelet caches driver info; EP's own analysis concurs), but this repo has no existing test exercising this path. Mitigation: Phase 5/7 must include a test specifically for the mutation-triggered recreate, not just the field values.
- **Upgrade/migration:** Per `specs.md` FR-010 and Constitution's "Verification-First" ethos, upgrade with no `driverConfig` set MUST be behaviorally identical to pre-feature defaults — this is a hard regression risk if the shared helper's default-resolution logic has any gap. Mitigation: Phase 5 must explicitly table-test the "no `driverConfig`" and "`driverConfig` present but `SecretsStore` nil" paths as first-class cases, not incidental ones.
- **Compatibility (OpenShift/MicroShift/Hypershift):** the source EP states the Secrets Store CSI Driver Operator is not part of MicroShift; no Hypershift-specific concerns were raised in `specs.md`/`repo-assessment.md`. No additional platform-matrix risk identified beyond standard OpenShift.
- **Upstream API drift risk:** if the eventual merged `openshift/api` shape differs from the EP's proposed field names/types (common during API review), Phases 2–4's exact field-access code will need adjustment — this is expected and should not be treated as a plan failure, but `tasks.md` should not hardcode brittle assumptions about exact upstream field names before Phase 1 lands.
- **RBAC assumption is mechanism-contingent (carried from `repo-assessment.md` §11):** the "no new RBAC" conclusion (§3.4) holds for the currently-known access patterns; if Phase 2 chooses a different mechanism, Phase 6 must re-verify before assuming no change is needed.

## 8. Open questions / SME decisions

1. **Upstream `openshift/api` PR status and timeline for the `SecretsStore` addition** — Owner: EP authors / API approvers (`@JoelSpeed` per the EP frontmatter). Assumption if unanswered before Task Creation: treat as an unscheduled, long-lead external blocker; sequence Phase 1 as "blocked — awaiting upstream" and allow Phases 2–5 to proceed only as design/scaffolding work behind a temporary `go.mod replace`, not as mergeable work.
2. **Exact mechanism for the shared `ClusterCSIDriver.Spec.DriverConfig` read path (§3.2/§8 in this doc)** — dedicated typed informer/lister (e.g. via `github.com/openshift/client-go/operator/informers/externalversions`, already transitively vendored) vs. a direct typed-client `Get` call inside each consumer closure. Owner: implementing engineer / code reviewer (`OperatorController`-equivalent capability per §0's capability substitution). Assumption if unanswered before Task Creation: default to a dedicated informer/lister for consistency with this operator's existing informer-driven design (all other controllers here are informer-triggered, not poll-based) — but this is a recommendation based on library-go's exposed API surface, not a pattern already proven in this specific operator (`repo-assessment.md` §11.1), so confirm with a prototype before committing `tasks.md` task descriptions to it.
3. **`AgentRoutingMode: PROVIDED` vs. AGENTS.md's lack of a formal Agent-ID taxonomy (§0 of this doc)** — Owner: repo maintainer / user. Assumption used in this plan: the Constitution's Code Ownership table (`ControllerLogic`, `StaticAssets`, `OLMRelease`, `Testing`, `Docs`) serves as the concrete capability set for phase routing above. If the user wants strict `tasks.md` Agent-ID enforcement matching a formal AGENTS.md table, that table should be added to the root `AGENTS.md` before Task Creation; otherwise `tasks.md` should carry forward this same substitution explicitly (not silently invent a different taxonomy).
