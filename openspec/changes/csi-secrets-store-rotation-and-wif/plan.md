# Technical Implementation Plan
**Feature:** Configurable Secret Rotation and Workload Identity Federation

## 0. Inputs acknowledged

| Input | Status |
|-------|--------|
| Spec source | `SSCSI-254` / Configurable Secret Rotation and Workload Identity Federation |
| Repo assessment pin | `/Users/ckyal/go/src/github.com/chiragkyal/secrets-store-csi-driver-operator`, branch `openspec-cursor-agent-gpt5-4`, commit `60ee14a2c706e7d09cdf4bee480bff73ab619719` (tooling_status: FULL) |
| `agents.md` | PROVIDED — `AGENTS.md` is available in the target repo. It does not define an explicit execution-agent matrix, so the phase capability labels below use repo-grounded functional capabilities derived from the repo and AGENTS guidance: `DependencyVendoring`, `OperatorController`, `AssetManifests`, `RBACSecurity`, `Testing`, `OLMRelease`. |
| `spec_validator_results.json` | PROVIDED — `validation.json` approved with non-blocking concerns about omitted-field default clarity, documentation scope, and upgrade verification expectations |
| `constitution.md` | PROVIDED — resolved from `openspec/inputs/constitution.md`; AgentRoutingMode is `PROVIDED` |

## 1. Architectural strategy

This feature must be implemented as an extension of the existing single `library-go` `CSIControllerSet` architecture, not as a new reconciler or new CRD. The operator already uses `ClusterCSIDriver` as the singleton control surface, applies embedded YAML assets through `WithConditionalStaticResourcesController`, and manages the node DaemonSet through `WithCSIDriverNodeService`. The implementation strategy is therefore to: (1) consume the upstream `ClusterCSIDriver` API expansion once vendored, (2) derive runtime rotation/WIF state in repo-local helper logic under `pkg/operator/`, (3) transform the currently static `CSIDriver` asset into a dynamically rendered desired object, and (4) inject administrator-selected rotation flags into the existing node-service reconciliation path while preserving management-state gating and the trusted CA bundle hook.

**Repo-grounded reality check:** `repo-assessment.md` confirms that this pinned branch does **not** already implement rotation/WIF runtime support. `pkg/operator/starter.go` currently consumes only generic `OperatorSpec` state, `assets/node.yaml` hardcodes `--enable-secret-rotation=true` and `--rotation-poll-interval=2m`, and `assets/csidriver.yaml` omits both `requiresRepublish` and `tokenRequests`. The plan is therefore **greenfield repo-local implementation** on top of existing controller/asset patterns, not hardening of existing feature code.

The sequencing must respect the constitutional constraints: no new CRD, no controller-runtime, no bypass of `getOperatorSyncState()`, no loss of `WithCABundleDaemonSetHook(...)`, and no manual edits to vendored dependencies or generated packaging outputs. The plan also keeps the validation findings explicit by treating default rotation behavior, documentation scope, and CSIDriver recreation safety as verification concerns rather than silent assumptions.

## 2. Persistence & state

- **Kubernetes objects:** The source-of-truth object remains the cluster-scoped `ClusterCSIDriver` singleton named `secrets-store.csi.k8s.io`. The derived/reconciled objects relevant to this feature are the managed `CSIDriver` object and the node DaemonSet in the operator namespace.
- **Operand config/state:** Rotation behavior is currently represented by hardcoded driver container args in `assets/node.yaml`; the plan moves those values under hook-driven derivation while keeping the DaemonSet itself managed by `WithCSIDriverNodeService`. WIF-related token audience behavior will become reflected in the desired `CSIDriver` spec rather than in a new persisted object.
- **External/platform-injected state:** Trusted CA bundle propagation remains mandatory through `cabundle_cm.yaml` and `WithCABundleDaemonSetHook(...)`. Release-payload image injection remains externalized through image placeholders and OLM/image-reference flow.
- **Upgrade/migration state:** Existing live `CSIDriver` token request state must be treated as migratable runtime state. When operator-managed token requests are not enabled, repo-local logic must preserve live `CSIDriver` token request behavior during reconciliation rather than overwriting it.
- **N/A reason:** No new repo-local persistence store, database, or custom CRD-backed state is introduced by this plan.

## 3. Interfaces & contracts (operator-native)

### 3.1 Kubernetes APIs (CRDs/CRs)
- The control API remains `operator.openshift.io/v1.ClusterCSIDriver`; no new CRD or repo-local API package is allowed by the constitution.
- The implementation depends on an upstream expansion of the `ClusterCSIDriver` config surface to add Secrets Store-specific driver configuration for:
  - secret rotation mode and optional custom minimum refresh age
  - token request ownership mode
  - managed audience entries with optional expiration
- Contract expectations:
  - omitted rotation config preserves platform default behavior
  - omitted token request management preserves existing live token request behavior
  - operator-managed token request ownership is a one-way transition
  - invalid values must be rejected before the operator replaces current behavior
- Admission and immutability enforcement are expected to live in the upstream API/CRD layer, not in a repo-local webhook.

### 3.2 Controller/runtime interfaces (internal)
- `RunOperator()` remains the composition root for the feature.
- New repo-local helper logic should provide:
  - extraction/derivation of effective Secrets Store config from `ClusterCSIDriver`
  - mapping from desired config to effective `CSIDriver` fields (`requiresRepublish`, `tokenRequests`)
  - mapping from desired config to DaemonSet driver args (`--enable-secret-rotation`, `--rotation-poll-interval`)
  - preservation logic for existing live `CSIDriver` token requests when the operator is not yet authoritative
- The static-resource path should evolve from raw `replaceNamespaceFunc()` bytes for `csidriver.yaml` into a dynamic asset rendering path that starts from the embedded base manifest and overlays runtime-derived fields.
- The node-service path should gain an additional DaemonSet hook layered with the existing CA bundle hook rather than replacing the node-service controller model.

### 3.3 Webhooks / admission (if applicable)
N/A — the repo does not contain a repo-local webhook framework today, and the required validation/immutability behavior is expected to come from the upstream `ClusterCSIDriver` API surface rather than a new webhook in this repository.

### 3.4 RBAC / security boundaries (if applicable)
- Existing node-plugin RBAC already grants:
  - secret CRUD for rotation/sync
  - `serviceaccounts/token` create
  - `csidrivers` get/list/watch
- The plan assumes these existing permissions remain sufficient for operand-side behavior, while the operator-side CSV/static-resource permissions are reviewed for any new `CSIDriver` update/delete requirements.
- Any RBAC delta must remain asset-driven (`assets/rbac/*.yaml`) and packaging-aligned; no dynamic runtime role creation is allowed.
- Security boundary constraints:
  - preserve least privilege
  - preserve privileged SCC only for the node DaemonSet ServiceAccount
  - preserve CA bundle injection and existing hostPath/privileged assumptions unless the change explicitly requires otherwise

### 3.5 Packaging / OLM (if applicable)
- OLM packaging remains follow-on to repo-local runtime changes.
- Stable CSV and image references may need updates only if runtime permissions or image expectations change; they are not the primary implementation surface.
- If the upstream API/vendor change updates release expectations, the plan should include vendoring and packaging validation, but not manual scattershot version edits. Any version/metadata update must continue to use `hack/update-metadata.sh`.

## 4. Dependencies & sequencing graph

- **Critical path summary:**
  1. Upstream `openshift/api` exposes the required Secrets Store config surface and repo dependency pin is updated.
  2. Repo-local helper logic is added to interpret the new config and preserve existing live behavior.
  3. `CSIDriver` desired-state rendering becomes dynamic while staying within the static-resource controller model.
  4. DaemonSet rotation args move under an additional node-service hook while preserving CA bundle propagation.
  5. Unit coverage is added for config derivation, preservation logic, and hook behavior.
  6. E2E/manual upgrade verification confirms default behavior, custom behavior, disablement, and token-request preservation/management transitions.
  7. Packaging/RBAC follow-through is validated only after runtime behavior is correct.

- **Parallelizable workstreams:**
  - Once the upstream API/vendor step is complete, unit-test scaffolding and helper logic can progress in parallel with e2e scenario design.
  - Packaging/RBAC review can run in parallel with final verification after the runtime shape stabilizes.
  - Documentation follow-up about safe refresh-interval guidance can proceed in parallel with verification if product/SME teams want it in-scope.

- **Explicit blockers / external dependencies:**
  - Upstream `openshift/api` availability for the new Secrets Store config fields is the primary external blocker.
  - Exact QE expectation for CSIDriver delete/recreate upgrade verification remains an SME/QE alignment item.
  - If product requires explicit documentation as part of mitigation scope, that needs confirmation before tasking downstream work.

## 5. Implementation phases (logical sequence; NOT tasks)

### Phase 1: Upstream API readiness and repo pin alignment
- **Goal:** Establish a consumable upstream API surface for Secrets Store rotation and token-request configuration, then align the repo’s dependency pin so repo-local code can compile against it.
- **Dependencies:** Must wait for the upstream API design in the approved spec to be implemented or otherwise vendorable.
- **Target files:** `go.mod`, `vendor/` (generated follow-on only), `pkg/operator/starter.go` for import/usage adaptation once the new fields exist.
- **Required capabilities:** `DependencyVendoring`, `OperatorController`, `RBACSecurity`
- **Verification hooks:** `make verify`; `make test-unit`; direct compile proof that repo-local code can reference the vendored config surface without speculative type aliases.

### Phase 2: Config derivation and compatibility-preservation layer
- **Goal:** Introduce repo-local helper logic that converts `ClusterCSIDriver` Secrets Store configuration into effective runtime values while preserving pre-existing token-request behavior when operator management is not enabled.
- **Dependencies:** Must wait for Phase 1 so the config surface exists locally.
- **Target files:** `pkg/operator/secrets_store_config.go` (new), `pkg/operator/secrets_store_config_test.go` (new), `pkg/operator/starter.go`
- **Required capabilities:** `OperatorController`, `Testing`
- **Verification hooks:** Table-driven unit tests for omitted/default rotation behavior, custom intervals, disabled rotation, managed/unmanaged token-request ownership, empty managed audiences, and preservation of existing live token requests; `make verify`; `make test-unit`.

### Phase 3: Dynamic CSIDriver rendering and node-service hook wiring
- **Goal:** Replace the current static `CSIDriver` behavior with a dynamically rendered desired object and layer a rotation-argument hook into the existing node-service controller path without violating constitution guardrails.
- **Dependencies:** Must wait for Phase 2 helper logic and the confirmed upstream API field shape.
- **Target files:** `pkg/operator/starter.go`, `assets/csidriver.yaml`, `assets/node.yaml` (only if ownership/default documentation needs adjustment), `assets/assets.go` (only if asset layout changes)
- **Required capabilities:** `OperatorController`, `AssetManifests`, `RBACSecurity`
- **Verification hooks:** Unit tests covering dynamic `CSIDriver` desired-state rendering, preservation vs managed overwrite behavior, and driver-arg mutation; `make verify`; `make test-unit`.

### Phase 4: Runtime failure signaling, packaging alignment, and security review
- **Goal:** Confirm that runtime failure modes remain operator-visible, RBAC remains least-privilege, and packaging surfaces continue to represent the resulting runtime behavior correctly.
- **Dependencies:** Must wait for Phase 3 so the final runtime shape is known.
- **Target files:** `assets/rbac/secretproviderclasses_role.yaml` (only if a permission gap is proven), `config/manifests/stable/secrets-store-csi-driver-operator.clusterserviceversion.yaml` (follow-on validation/update only if needed), `config/manifests/stable/image-references` (validation only unless image expectations change), `README.md` or related docs if documentation scope is confirmed
- **Required capabilities:** `RBACSecurity`, `OLMRelease`, `OperatorController`
- **Verification hooks:** `make verify`; `make test-unit`; targeted review against `docs/security-guidelines.md`; packaging sanity checks against stable CSV/image references.

### Phase 5: E2E and upgrade-behavior verification
- **Goal:** Validate administrator-visible behavior across default rotation, custom rotation, disabled rotation, managed token audiences, empty managed audiences, multi-audience behavior, and upgrade-safe preservation paths.
- **Dependencies:** Must wait for Phases 2 through 4 so runtime behavior and packaging are stable enough to exercise.
- **Target files:** `hack/e2e.sh`, `pkg/operator/secrets_store_config_test.go` (if additional coverage gaps appear), `pkg/operator/starter_test.go` (if management-state interactions need extra assertions)
- **Required capabilities:** `Testing`, `OperatorController`, `RBACSecurity`
- **Verification hooks:** `make test-e2e`; `make verify`; `make test-unit`; cluster-side checks such as `oc get csidriver secrets-store.csi.k8s.io -o yaml` and `oc get ds -n openshift-cluster-csi-drivers secrets-store-csi-driver-node -o jsonpath='{.spec.template.spec.containers[?(@.name=="csi-driver")].args}'`.

### Phase 6: Final integration hardening and PR-ready validation
- **Goal:** Reconfirm that the implementation respects constitutional rules, repo guardrails, and approved spec behavior before downstream tasking/implementation closure.
- **Dependencies:** Must wait for all earlier phases to complete.
- **Target files:** `pkg/operator/starter.go`, new helper/test files in `pkg/operator/`, `assets/`, `hack/e2e.sh`, and any packaging/doc surfaces actually touched by the implementation
- **Required capabilities:** `OperatorController`, `Testing`, `OLMRelease`
- **Verification hooks:** `make check`; `make test-e2e` when environment is available; targeted manual review of management-state gating, CA bundle hook preservation, and upgrade preservation behavior.

## 6. Verification matrix (maps to spec acceptance)

| Category | Coverage | Files / Suites |
|----------|----------|----------------|
| Unit | Config derivation, default/disabled/custom rotation behavior, managed/unmanaged token-request ownership, empty audience handling, preservation of existing live token requests, and DaemonSet arg mutation | `pkg/operator/secrets_store_config_test.go` (new), `pkg/operator/starter_test.go` |
| Integration | N/A — no repo-local integration test harness distinct from unit/e2e was evidenced in the repo assessment; behavior should be covered by unit tests plus cluster-backed e2e/manual checks | N/A |
| E2E | Default rotation behavior, custom interval behavior, disabled rotation behavior, managed audience propagation, empty managed audience clearing, multi-audience WIF behavior, and upgrade-safe preservation of pre-existing token requests | `hack/e2e.sh` |
| Manual / Cluster | Inspect effective `CSIDriver` and node DaemonSet state after applying config changes; confirm degraded/error visibility when invalid configuration is attempted; confirm upgrade-safe behavior in a cluster with pre-existing token requests | `oc get csidriver secrets-store.csi.k8s.io -o yaml`; `oc get ds -n openshift-cluster-csi-drivers secrets-store-csi-driver-node -o jsonpath='{.spec.template.spec.containers[?(@.name=="csi-driver")].args}'`; operator status/degraded inspection via `ClusterCSIDriver` |
| N/A | No new webhook/admission test suite is planned because the repo does not contain a repo-local webhook surface for this feature | N/A |

## 7. Risks, migrations, and operational follow-ups

- **Upgrade/migration:** The plan must preserve existing live `CSIDriver` token requests until the administrator explicitly opts into operator-managed ownership. Because `CSIDriver` spec changes can require delete/recreate behavior, upgrade verification must prove there is no unintended disruption when old token requests exist.
- **Compatibility (OpenShift/MicroShift/Hypershift):** The repo inputs indicate this operator is cluster-scoped and standard-cluster oriented; unsupported or not-yet-supported footprints remain out of scope unless a follow-on requirement expands them.
- **Upstream API drift risks:** The final upstream type names, validation semantics, and applyconfig extraction behavior may drift from the enhancement draft; Phase 1 must validate the vendored API shape before repo-local code structure is finalized.
- **Management-state risk:** New logic must not bypass `getOperatorSyncState()`. If helper logic or dynamic rendering attempts to read/write resources outside the managed path, the operator can violate constitutional behavior.
- **CA bundle regression risk:** Any new DaemonSet hook composition must preserve `WithCABundleDaemonSetHook(...)`; losing it would regress trusted CA propagation and possibly break provider communication in proxy/FIPS environments.
- **Documentation scope follow-up:** The validation artifact called out that “choose a wise value” is too soft for a mitigation. If product wants this mitigation in-scope, a docs follow-up should be explicitly added rather than assumed.
- **Operational follow-up:** If the feature lands with upgrade-sensitive behavior, release notes or operator documentation may need a short operator-admin explanation of preserved vs operator-managed token audience ownership.

## 8. Open questions / SME decisions

| Question | Owner | Default assumption if unanswered before Task Creation |
|---|---|---|
| Which exact upstream `openshift/api` commit or release introduces the Secrets Store config fields this repo should vendor? | Upstream API maintainers / feature owner | Block repo-local implementation planning at the dependency step until a vendorable commit is identified |
| Is safe-refresh-interval documentation part of the required deliverable for this feature, or is code/test behavior sufficient for the first implementation slice? | Product owner / docs SME | Treat documentation as a non-blocking follow-up unless explicitly added to implementation scope |
| What level of upgrade-proof evidence is required for the brief `CSIDriver` recreate window when spec changes occur? | QE / feature SME | Add explicit e2e and manual checks for upgrade preservation, but do not introduce a bespoke migration controller or alternate rollout mechanism unless evidence shows current behavior is insufficient |
