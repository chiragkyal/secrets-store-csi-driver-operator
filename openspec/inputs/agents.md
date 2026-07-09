This file provides guidance to AI agents working with the **cert-manager-operator** for OpenShift — a Go operator managing cert-manager and addon operands. It uses library-go controllers for cert-manager core and controller-runtime controllers for addons.

## Repository Layout

```
api/operator/v1alpha1/       # CRD types (all operands), features.go, conditions.go, meta.go
bindata/<addon>/resources/   # Static YAML manifests per addon (go-bindata embedded)
config/                      # Kustomize, CRDs, RBAC, samples
bundle/manifests/            # OLM bundle (CSV, CRDs)
hack/                        # Code generation & manifest update scripts
pkg/controller/certmanager/  # cert-manager core (library-go — DO NOT use for addons)
pkg/controller/<addon>/      # Addon controller (controller-runtime)
pkg/controller/common/       # Shared: client.go, errors.go, reconcile_result.go, utils.go, validation.go, constants.go
pkg/operator/starter.go      # Entrypoint, feature gate wiring
pkg/operator/setup_manager.go # Unified ctrl.Manager for all addons
pkg/features/features.go     # Feature gate parsing + cluster FeatureSet discovery
test/e2e/                    # Ginkgo/Gomega e2e tests (build tag: e2e)
```

## Two Controller Patterns — Never Mix

| Aspect | cert-manager core (`certmanager/`) | Addons (`<addon>/`) |
|--------|-----------------------------------|---------------------|
| Framework | library-go, `v1helpers`, informers | controller-runtime, `ctrl.Manager`, SSA |
| API spec | Embeds `operatorv1.OperatorSpec` | Custom domain-specific spec — **never embed operatorv1** |
| RBAC | Library-go patterns | kubebuilder markers on Reconciler |
| Apply | `resourceapply` | Server-Side Apply (`client.Apply`) |

**All addon work uses controller-runtime.** Never follow `certmanager/` patterns for addons.

## Shared `pkg/controller/common/` — Never Duplicate

| Symbol | Use for |
|--------|---------|
| `NewClient(mgr)` → `CtrlClient` | All client operations (Get/List/Create/Patch/StatusUpdate/Exists) |
| `NewIrrecoverableError()`, `FromClientError()`, `NewRetryRequiredError()` | Error classification |
| `HandleReconcileResult()` | Status condition state machine + requeue logic |
| `DecodeObjBytes[T](codecs, gv, bytes)` | Deserialize bindata YAML |
| `UpdateName/Namespace/ResourceLabels()` | Set object metadata |
| `ValidateLabelsConfig/AnnotationsConfig/NodeSelectorConfig/TolerationsConfig/ResourceRequirements/AffinityRules()` | CR spec validation |
| `fakes/FakeCtrlClient` | Unit test mocking (counterfeiter) |

## Unified Manager (`setup_manager.go`)

All addons share **one** `ctrl.Manager`. Never create separate managers.
- `ControllerConfig` has `Enable<Name>` booleans
- Each addon defines `<name>ManagedResources` slice for cache label filtering
- Managed resources use `common.ManagedResourceLabelKey` ("app") with value `"cert-manager-<addon>"`
- Cache merges label selectors via `labels.In` when GVKs overlap; ConfigMaps are unfiltered

## Feature Gates

**Definition** (`api/operator/v1alpha1/features.go`): `Feature<Name>` constant + `OperatorFeatureGates` entry.

**Runtime** (`pkg/features/features.go`):
- GA: `DefaultFeatureGate.Enabled(Feature<Name>)` only
- TechPreview: requires BOTH operator gate AND `FeatureGateState.passesClusterPreviewGating()` — discovers `featuregates.config.openshift.io/cluster`, checks `spec.featureSet` against `TechPreviewNoUpgrade`/`CustomNoUpgrade`/`DevPreviewNoUpgrade`/`OKD`, retries 3x with 30s backoff, **fails closed**

**Wiring** (`starter.go`): GA → `features.Is<Name>FeatureGateEnabled()`, TechPreview → `featureStatus.Is<Name>FeatureGateEnabled()`

## Adding a New Addon Controller

### 1. API Types (`api/operator/v1alpha1/<name>_types.go`)

Register via `init()` → `SchemeBuilder.Register()`. No changes to `groupversion_info.go` or `doc.go`.

Required markers on the CRD type:
```go
// +genclient  +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true  +kubebuilder:subresource:status
// +kubebuilder:resource:path=<plural>,scope=Cluster,categories={cert-manager-operator},shortName=<short>
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].message"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'cluster'",message="singleton"
// +operator-sdk:csv:customresourcedefinitions:displayName="<DisplayName>"
```

**Spec**: domain-specific config struct + `ControllerConfig` (labels/annotations only). **Status**: embed `ConditionalStatus` + observed state fields. **Conditions**: `Ready`/`Degraded` via `SetCondition()` → `HandleReconcileResult()`.

**CEL patterns**: singleton (`self.metadata.name == 'cluster'`), immutable (`oldSelf == '' || self == oldSelf`), conditional required, mutually exclusive. No admission/conversion webhooks.

**Enums**: typed string constants (`Enabled`/`Disabled`) + `+kubebuilder:validation:Enum` + `+kubebuilder:default`.

### 2. Validation Tests (`api/operator/v1alpha1/tests/<plural>.operator.openshift.io/<name>.testsuite.yaml`)

YAML-based `onCreate`/`onUpdate` tests covering all CEL rules, defaults, enums, immutability.

### 3. Feature Gate

Add `Feature<Name>` to `features.go`. For TechPreview: `{Default: false, PreRelease: "TechPreview"}`.

### 4. Bindata (`bindata/<name>/resources/`)

Create `hack/update-<name>-manifests.sh` (helm template → yq relabel → split). Add `<NAME>_VERSION` to Makefile. Run `make update-bindata`.

### 5. Controller Package (`pkg/controller/<name>/`)

**`constants.go`**: `ControllerName` (exported), `RequestEnqueueLabelValue` (exported), image env var (`RELATED_IMAGE_CERT_MANAGER_<UPPERCASE>`), version env var, resource name constants, asset name constants, `fieldOwner`, `controllerDefaultResourceLabels`, `defaultRequeueTime`.

**`utils.go`**: local `scheme`/`codecs` (register only needed API groups), `updateStatus()` with retry-on-conflict, `addFinalizer()`/`removeFinalizer()` via `controllerutil`, `validateConfig()`, `getResourceLabels/Annotations()`, `managedMetadataModified()`, helper predicates.

**`controller.go`**: `Reconciler` struct embedding `common.CtrlClient` + ctx/log/eventRecorder/scheme. `New(mgr)` constructor. `SetupWithManager()` with watches:
- Primary CR: `For()` with `GenerationChangedPredicate`
- Managed resources: `Watches()` with `controllerManagedResourcePredicates` (label-based)
- Deployments/Certificates: `withIgnoreStatusUpdatePredicates` (generation + label + annotation)
- External resources: dedicated name/namespace predicates

`Reconcile()` flow: fetch CR → deletion? cleanUp+removeFinalizer : addFinalizer+processReconcileRequest → `HandleReconcileResult()`.

**Reconciler files** (one per resource kind): `serviceaccounts.go`, `rbacs.go`, `services.go`, `deployments.go`, `certificates.go`, `webhooks.go`, `configmaps.go` (if needed).

Each follows: decode bindata → set metadata → `r.Exists()` → `<resource>Modified()` → `r.Patch(ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership)` → event. Return `common.FromClientError()` on failure.

**`install_<name>.go`**: ordered sequence — validateConfig → (configmaps) → serviceAccounts → RBAC → services → issuer → certificate → deployment → webhooks → updateStatusObservedState.

### 6. Wiring

In `setup_manager.go`: add `Enable<Name>` to `ControllerConfig`, `setup<Name>Controller(mgr)`, `<name>ManagedResources` slice, CR cache entry.
In `starter.go`: feature gate check → pass to `NewControllerManager()`.

### 7. RBAC

`+kubebuilder:rbac` markers on Reconciler for all managed GVKs. For TechPreview: add `featuregate_clusterrole.yaml`/`featuregate_clusterrole_binding.yaml` to `config/rbac/`. Operand RBAC comes from bindata (applied by `rbacs.go`).

### 8. OLM Bundle

Add CRDs to `bundle/manifests/`. Update CSV: owned CRD, env vars (`RELATED_IMAGE_CERT_MANAGER_<NAME>`, `<NAME>_OPERAND_IMAGE_VERSION`), `relatedImages`, RBAC. Sample CR to `config/samples/tech-preview/`.

### 9. Tests

**Unit**: `*_test.go` per reconciler, `test_utils.go` for fixtures, `FakeCtrlClient` for mocking, table-driven, test both no-op and update paths.
**E2E**: `<name>_test.go` + `<name>_helpers_test.go`, build tag `e2e`, `Ordered` + `Label("Feature:<Name>")`, mirror constants (don't import internals), typed client for CR, `kubernetes.Clientset` for resources.

### 10. Code Generation

Run after type changes: `make generate && make manifests`. Never hand-edit `zz_generated.deepcopy.go`, `config/crd/bases/*.yaml`, `pkg/operator/{clientset,informers,listers,applyconfigurations}/`, `assets/bindata.go`.

## Adding a Feature to an Existing Controller

### Tier 1: Deployment Arg Change

Scope: new spec field → deployment container args/env.

Modify: `<name>_types.go` (field) → `deployments.go` (apply arg) → `deployments_test.go` → testsuite YAML. Run `make generate && make manifests`. Do NOT touch `controller.go`, `install_<name>.go`, or `setup_manager.go`.

### Tier 2: Cross-Cutting (Multiple Reconcilers)

Scope: spec field affects RBAC + deployment + other resources.

Modify: `<name>_types.go` (field + status) → multiple `*.go` reconcilers → `utils.go` (predicate helper like `myFeatureEnabled()`) → all corresponding `*_test.go` → `test_utils.go` → `install_<name>.go` (if signatures change) → testsuite YAML → e2e tests.

Keep conditional logic in the resource builder function, not scattered across the reconcile flow. Add observed status field + `updateStatusObservedState()`.

### Tier 3: New Managed Resource Kind

Scope: controller manages a GVK it didn't before.

Create: `<resource>.go` + `<resource>_test.go` (standard SSA reconciler pattern).
Modify: `<name>_types.go` → `constants.go` (names, asset paths) → `install_<name>.go` (add call in correct order) → `controller.go` (add `Watches()`) → `utils.go` (scheme registration, comparison func) → `setup_manager.go` (add to `ManagedResources` unless unfiltered like ConfigMap) → RBAC markers → testsuite → e2e.

If from bindata: add manifest, `make update-bindata`, add asset constant. If programmatic: build in `get<Resource>Object()`, still apply via SSA.

**Install order**: ConfigMaps/SA → RBAC → Services → Issuer/Certificate → Deployment → Webhooks.
**Watch predicates**: managed-label → `controllerManagedResourcePredicates`; external → name/ns predicate; status-heavy → `withIgnoreStatusUpdatePredicates`.

### General Rules (All Tiers)

- New spec fields: `+optional` + `+kubebuilder:default` + `+kubebuilder:validation:Enum` (if enum). Immutable: CEL `oldSelf == '' || self == oldSelf`.
- New observable state → status field + `updateStatusObservedState()`
- Always: CRD validation tests + unit tests (enabled/disabled) + e2e test
- OLM: update CSV if RBAC/CRDs/samples change. `make manifests` to regenerate.
- Feature gating within a controller: use spec `Enabled`/`Disabled` policy enum, not operator-level feature gate

## Webhook TLS

Addons with webhooks use cert-manager for TLS: self-signed Issuer → Certificate → webhook annotation `cert-manager.io/inject-ca-from: <ns>/<cert>`. Never self-sign or custom-generate certs.

## Environment Variables

Per addon: `RELATED_IMAGE_CERT_MANAGER_<UPPERCASE_NAME>` (image) + `<UPPERCASE_NAME>_OPERAND_IMAGE_VERSION` (version labels). Always prefix with `CERT_MANAGER_`.

## Makefile

Per addon: `<NAME>_VERSION ?= <ver>` + `RELATED_IMAGE_CERT_MANAGER_<NAME>` + `<NAME>_OPERAND_IMAGE_VERSION` in local-run. Add `hack/update-<name>-manifests.sh $(<NAME>_VERSION)` to `update-manifests` target.

## Common Mistakes

1. Do NOT embed `operatorv1.OperatorSpec` in addon types — use domain-specific specs
2. Do NOT create per-controller `client.go`/`errors.go` — use `common/`
3. Do NOT create separate `ctrl.Manager` — register in `setup_manager.go`
4. Do NOT use create-or-update — use SSA (`client.Apply`)
5. Do NOT install CRDs at runtime — OLM handles it
6. Do NOT name env var `RELATED_IMAGE_<NAME>` — use `RELATED_IMAGE_CERT_MANAGER_<NAME>`
7. Do NOT implement full cleanup for TechPreview — warning event only, defer to GA
8. Do NOT add NetworkPolicies unless specifically required
9. Do NOT bypass cluster FeatureGate discovery for TechPreview
10. Do NOT hand-edit generated files
11. Do NOT use generic overrides (overrideArgs/Env/Replicas) — use domain-specific config
12. Do NOT create conversion/admission webhooks — CEL handles validation
13. Do NOT check cert-manager health as prerequisite — startup ordering handles it

---

## Per-task testing during `/opsx-apply` (code generation eval gate)

During implementation, each code generation task is verified with **real command execution** (not agent assertions). See **[`stage-gate/CODE_GENERATION_EVAL_PROMPT.md`](stage-gate/CODE_GENERATION_EVAL_PROMPT.md)** for the full protocol and **[`unit-tests-code-gen.md`](../../unit-tests-code-gen.md)** for design rationale.

| Task type | Verification | Test strategy |
|-----------|-------------|--------------|
| API types | `go build`, `go vet` | Build-only |
| Codegen (`make generate/manifests`) | `make generate && make manifests && make verify` | Consistency check |
| Controller logic (`pkg/controller/`) | `go build`, `go vet` | Co-generated `_test.go` + `go test` (IstioCSR exemplar) |
| Bindata / manifests | `make update-bindata && make verify` | `make verify` |
| OLM bundle | `make bundle && hack/verify-bundle.sh` | Bundle scripts |
| Feature gates | `go build`, `go vet` | `go test ./pkg/features/... -run TestFeatureGates` |

---

## Execution agent routing

Use these **Assigned Agent** IDs in `tasks.md` §3 when **`AgentRoutingMode: PROVIDED`**. Each task gets exactly one primary agent. Map work to paths below; split mixed tasks.

| Agent ID | Scope | Route when task touches | OAPE / execution |
|----------|-------|-------------------------|------------------|
| **API_Agent** | CRD/API types, markers, `.testsuite.yaml` | `api/operator/v1alpha1/`, `test/apis/` | `api-generate` (implementation) or `api-generate-tests` (verification-only) |
| **OperatorController_Agent** | Reconciliation, deployments, operator wiring | `pkg/controller/certmanager/`, `pkg/controller/istiocsr/`, `pkg/controller/trustmanager/`, `pkg/operator/starter.go`, `pkg/operator/setup_manager.go` | `api-implement` |
| **ManifestsBindata_Agent** | Operand YAML, CRDs in bindata, version pins | `bindata/`, `hack/update-cert-manager-manifests.sh`, `Makefile` operand version vars | Manual — `make update` / `make update-manifests` |
| **WebhookTLS_Agent** | Webhook TLS, CA bundles, serving certs | Webhook deployments, trusted CA ConfigMap wiring | Manual |
| **RBACSecurity_Agent** | RBAC, SCC, CredentialsRequest, network policies | `config/rbac/`, `pkg/controller/*/credentials`, NP controllers | Manual |
| **OLMRelease_Agent** | OLM bundle, CSV, relatedImages, catalog | `config/`, `bundle/`, `deploy/` | Manual — `make bundle`, `make deploy` |
| **Testing_Agent** | E2E and integration test authoring | `test/e2e/`, `make test-e2e` | `e2e-generate` when task is e2e |
| **Docs_Agent** | User-facing docs | `README.md`, `docs/` | Manual |

### Controller routing rules

- **Core cert-manager operand** (`pkg/controller/certmanager/`): **library-go** static-resources / sync patterns — do not apply addon controller-runtime SSA patterns here.
- **Addons** (IstioCSR, trust-manager): **controller-runtime** + SSA; register on the **single** unified manager in `pkg/operator/setup_manager.go` — do not create separate managers.
- **API before controller**: tasks that add CRD fields must complete (and pass `make update` / `test-apis`) before controller tasks that reconcile those fields.

### Verification pairing

- API changes → pair with `test/apis` or `.testsuite.yaml` tasks (`API_Agent`, verification-only).
- Controller / status changes → pair with unit tests (`make test-unit`) and e2e when user-visible (`Testing_Agent`).
- Bindata / operand version bumps → pair with `make verify` and relevant e2e smoke paths.

---

## Stage-Specific Agent Guidance

The sections below provide cert-manager-operator-specific hints that each pipeline stage
agent MUST incorporate when processing this repository. Templates remain generic;
this file is the single source of project-specific depth.

---

### Repo-Assessment Stage Hints

#### Exemplar Reference (format only — not content to copy)

When assessing `cert-manager-operator`, use this merged assessment as a **quality bar**
for structure, depth, and section completeness (not as text to paste):

`python-scripts/stage2_constitute/output/repo-assessment.md`

Patterns that exemplars demonstrate and your output MUST match:
- §1 before §2 (architecture before file lists)
- Explicit "dead code / do not edit" traps called out in §1.3 and §2
- §4.2 as a numbered hook/pipeline table with error behavior column
- §5 entries state WHAT to reuse and WHEN (not just file paths)
- §6 guardrails grouped by category (Structural, API, Build, Deployment, Codegen, Security)
- §7 change-cascade table with real `make` / `hack/verify-*` commands from the Makefile
- §8.2 copy-paste-ready test commands
- §9.4 at least one "How to add..." walkthrough tied to actual files
- §11.1 honest UNVERIFIED list (including branch-specific absences)
- §12 preflight checklist + quick-nav table

#### cert-manager-operator Deep-Dive Requirements

When the repo is `cert-manager-operator` (detected via `operator.openshift.io` CRDs for
CertManager/IstioCSR, bindata directories for cert-manager/istio-csr, or
`openshift/jetstack-cert-manager` replace directive in go.mod), apply ALL the generic
Kubernetes/OpenShift operator hints from the template AND these additional requirements.

**Branch verification required:** TrustManager (`pkg/controller/trustmanager/`) and
`test/apis/` are NOT present on all branches (e.g. absent on `cert-manager-1.18`).
Verify in the target branch before documenting — state absence in §11.1 if not found.
When the validated spec describes TrustManager but the branch lacks it, document
**greenfield implementation** following the IstioCSR addon pattern (controller-runtime
reconciler, namespaced CR, singleton name `default` — NOT `cluster`).

**Anti-patterns (forbidden without branch evidence):**
- Claiming TrustManager code, bindata, or feature gates exist when `grep`/tree shows absence.
- Framing work as "verify/harden existing controller" when the branch requires greenfield build.
- Using `make test-unit` if Makefile only defines `make test`.

**Architecture (§1):**
- Document the dual-controller architecture explicitly: core cert-manager uses OpenShift
  library-go factory controllers (`StaticResourceController`, `DeploymentController`);
  IstioCSR uses a controller-runtime reconciler via a shared manager (TrustManager only
  if present on the branch).
- The kubebuilder `CertManagerReconciler` in `certmanager_controller.go` is a **dead RBAC
  placeholder** — call this out in §1.3 AND §2 with "Do not edit for reconciliation logic."
- Document `RunOperator` bootstrap in `pkg/operator/starter.go`: creates clients, informers,
  builds `CertManagerControllerSet` (8 library-go controllers), starts
  `DefaultCertManagerController`, conditionally starts controller-runtime manager for
  IstioCSR when `FeatureIstioCSR` is enabled.
- Document `DefaultCertManagerController` which auto-creates the singleton `CertManager`
  CR named `cluster` with `managementState: Managed`.

**Controllers & Reconciliation (§4.2):**
- Read `generic_deployment_controller.go` and document deployment hooks in **exact**
  execution order (17 hooks + optional cloud credentials when Infrastructure API present):
  1. withOperandImageOverrideHook → 2. withLogLevel →
  3. withPodLabelsOverrideHook → 4. withPodLabelsValidateHook →
  5. withContainerArgsOverrideHook → 6. withContainerArgsValidateHook →
  7. withContainerEnvOverrideHook → 8. withContainerEnvValidateHook →
  9. withDeploymentReplicasOverrideHook → 10. withContainerResourcesOverrideHook →
  11. withContainerResourcesValidateHook → 12. withPodSchedulingOverrideHook →
  13. withPodSchedulingValidateHook → 14. withUnsupportedArgsOverrideHook →
  15. withProxyEnv → 16. withCAConfigMap → 17. withSABoundToken →
  18. withCloudCredentials *(conditional, controller deployment only)*.
- Format §4.2 as a table: # | Hook | Purpose | On error.
- Document IstioCSR install sequence from `install_istiocsr.go`: validateConfig →
  networkPolicies → services → serviceAccounts → RBAC → certificates → deployments →
  addProcessedAnnotation. IstioCSR uses create-or-update with deep equality checks.
- If TrustManager exists on branch: document its install sequence and SSA field owner
  `trust-manager-controller` — otherwise state "Not on this branch" in §4.2.

**Configuration Surface (§4.1):**
- List ALL fields for `CertManager` spec in a table: managementState, logLevel,
  operatorLogLevel, unsupportedConfigOverrides (controller.args/webhook.args/cainjector.args),
  controllerConfig/webhookConfig/cainjectorConfig (each with overrideArgs, overrideEnv,
  overrideLabels, overrideResources, overrideReplicas, overrideScheduling),
  defaultNetworkPolicy, networkPolicies[].
- List ALL fields for `IstioCSR` spec in a table (see exemplar assessment).
- Document operator runtime flags from `pkg/cmd/operator/cmd.go`:
  `--trusted-ca-configmap`, `--cloud-credentials-secret`, `--unsupported-addon-features`.
- Document arg validation allowlists from `deployment_overrides_validation.go`: controller
  allows ACME solver args, DNS01 nameservers, metrics address, ambient credentials,
  backoff; webhook and cainjector allow only `--v`. Env allowlist: controller allows
  HTTP_PROXY/HTTPS_PROXY/NO_PROXY; webhook/cainjector allow none.

**Image Resolution (§4.3):**
- Document `related_images.go` RELATED_IMAGE env var mapping (controller, webhook,
  cainjector, acmesolver from `imageEnvMap`; istio-csr from `istiocsr/constants.go`).
- Note ACME solver special case: injected as `--acme-http01-solver-image=` controller
  arg via `withOperandImageOverrideHook`, not a container image field.
- Note: missing `RELATED_IMAGE_CERT_MANAGER_ISTIOCSR` causes IrrecoverableError.
  TrustManager RELATED_IMAGE only if TrustManager exists on branch.

**Status & Conditions (§4.4):**
- Document TWO condition systems when both exist: core cert-manager uses OpenShift
  `OperatorStatus` with per-component `{instance}Available/Progressing/Degraded`;
  IstioCSR uses custom `Ready/Degraded` on the IstioCSR CR (set in `controller.go`).
- Document error classification in `pkg/controller/istiocsr/errors.go` (NOT
  `pkg/controller/common/errors.go` — that path may not exist):
  IrrecoverableError → permanent Degraded=True, no requeue; recoverable → requeue ~30s;
  FromClientError → 401/403/Invalid treated as irrecoverable.

**Feature Gates (§4.5):**
- IstioCSR: GA, default on (`api/operator/v1alpha1/features.go`), runtime parsing in
  `pkg/features/features.go`, enabled/disabled via `--unsupported-addon-features`
  (e.g. `IstioCSR=false`). Does NOT require cluster TechPreview FeatureSet on GA branches.
- TrustManager gates: only document if TrustManager code exists on the target branch.
- Gate definitions live in `api/operator/v1alpha1/features.go`; runtime in `pkg/features/features.go`.

**Dual CRD (§1 or §11):**
- `config.openshift.io_certmanagers.yaml` is an empty stub CRD (untyped spec/status).
  It is NOT bundled, NOT installed, and has NO controller code. RBAC exists but is unused.
  State this definitively — do not leave it as an open question.

**Cloud Credentials (§10.3):**
- `credentials_request.go` mounts cloud secrets: AWS at `/.aws` with `AWS_SDK_LOAD_CONFIG=1`;
  GCP at `/.config/gcloud/application_default_credentials.json`.
  Azure mount is NOT implemented (returns error in default case).
- Only applied to the controller deployment, not webhook or cainjector.
- CredentialsRequest YAMLs are in `test/e2e/testdata/credentials/` — admin applies them
  externally; the operator only mounts the resulting secrets.

**Proxy & Trusted CA (§10.2):**
- `withProxyEnv` hook uses `operator-framework/operator-lib/proxy` to propagate
  OLM-injected proxy vars to operand deployments.
- `withCAConfigMap` mounts trusted CA at `/etc/pki/tls/certs/cert-manager-tls-ca-bundle.crt`
  from CNO-injected ConfigMap (`config.openshift.io/inject-trusted-cabundle: "true"`).
- Runtime flag: `--trusted-ca-configmap`.

**FIPS (§10.4):**
- `hack/go-fips.sh` sets `GOEXPERIMENT=strictfipsruntime` + tags `strictfipsruntime,openssl`.
  No boringcrypto — uses OpenSSL via strictfipsruntime. CGO_ENABLED=1 required.
- Operand built from `openshift/jetstack-cert-manager` fork (not upstream jetstack).

**Route Integration (§10.1 or §10.5):**
- The operator grants ACME HTTP-01 solver RBAC for `routes/custom-host` but does NOT
  deploy a dedicated routes controller. Route TLS is via upstream cert-manager's solver.

**ClusterOperator Status (§10.6 or §11):**
- RBAC exists for `clusteroperators.config.openshift.io` but there is NO code that
  creates or updates a ClusterOperator resource. Status is only on the CertManager CR.

**Console (§10.5):**
- Verify on target branch — ConsoleYAMLSample/QuickStart may or may not be present.
  If not found in `config/` or `bundle/`, state in §11.1 rather than asserting counts.

**Testing (§8):**
- Unit (pkg/ + api/, testify + counterfeiter) and e2e (test/e2e/, Ginkgo + live cluster)
  are always expected. API integration (test/apis/) is branch-dependent — verify before documenting.
- CI is in openshift/release, not in-repo. Default e2e filter from Makefile:
  `Platform: isSubsetOf {AWS}` (configurable via `E2E_GINKGO_LABEL_FILTER`).
- In-repo verify: `make verify` runs deepcopy, clientgen, bundle checks; other
  `hack/verify-*.sh` scripts may run in external CI separately.
- Coverage gaps: no unit tests for main reconcile controllers, network policy controllers,
  or operator bootstrap (starter.go, setup_manager.go).

#### Quality Checklist Addition

When self-checking the repo-assessment output, also verify:
- [ ] For cert-manager-operator: compare structure against exemplar at
      `python-scripts/stage2_constitute/output/repo-assessment.md` (format only)

---

### Planning Stage Hints

#### Agent Role Scoping

The Technical Planning Agent operates as a planner for the **cert-manager ecosystem**
(OpenShift cert-manager-operator, managed operands such as cert-manager core / istio-csr /
trust-manager, and related packaging, tests, and docs).

#### Cert-Manager Planning Content Expectations

Prefer operator-native thinking:
- CRDs/API evolution, validation, immutability, conversion notes
- Controller reconciliation boundaries and status conditions
- Bindata/helm generation scripts and embedded manifests
- Webhooks/TLS/CABundle patterns
- RBAC blast radius (especially secrets / cluster-scoped writes)
- OLM/CSV/bundle constraints and upgrade edges
- CI/e2e matrix impacts and MicroShift/OpenShift differences when spec mentions them

#### Default Repo Pin (User Message Template)

When no explicit repo is provided, default to:

```
primary_repo: "https://github.com/openshift/cert-manager-operator"
branch: "master"
commit: "<sha|unknown>"
```

---

### Validation Stage Hints

#### Ecosystem Evaluation Trigger

cert-manager ecosystem items are mandatory to evaluate when the spec touches
operators/operands/CRDs/webhooks/TLS/RBAC/networking/monitoring/upgrades/OpenShift/MicroShift/Hypershift.

#### cert-manager Ecosystem Pillars

When evaluating a spec for this project, assess the following pillars
(if absent → `missing_elements` and/or `cert_manager_ecosystem.gaps`):

- API & CRD lifecycle (scope, defaults, immutability, validation, migration/deprecation if relevant)
- Install / uninstall / reconcile semantics (including CR delete behavior)
- RBAC & blast radius (cluster-wide writes, secrets, cross-namespace effects)
- Webhooks & TLS (how secured, issuance path, failure modes)
- Platform matrix (OpenShift vs MicroShift; FeatureGates/FeatureSets if relevant; Hypershift/hosted notes)
- Observability (metrics/readiness/status conditions if relevant)
- Upgrade / downgrade / version skew

#### JSON Schema Extension

The validation output JSON MUST include a `cert_manager_ecosystem` object:

```json
"cert_manager_ecosystem": {
  "api_lifecycle_complete": true,
  "rbac_blast_radius_documented": true,
  "webhook_tls_and_failure_modes": true,
  "install_uninstall_semantics_clear": true,
  "platform_matrix_addressed": true,
  "observability_status_documented": true,
  "upgrade_skew_addressed": true,
  "gaps": ["string"]
}
```

Rules for booleans: set true ONLY if the spec text substantively covers that area;
otherwise false. Put questions and missing details in `gaps` (even when boolean is false).

#### Few-Shot Calibration Examples

##### Example 1: Well-Written Spec (PASS)

**Input spec text:**
> **Title**: Add certificate rotation support for webhook serving certs
>
> **Motivation**: Cluster admins currently must manually restart the operator pod when the
> serving certificate expires, causing 15-minute outages on average. This impacts all
> clusters running cert-manager-operator v1.13+.
>
> **User Persona**: Cluster administrator managing OpenShift 4.14+ clusters with
> cert-manager-operator installed.
>
> **Acceptance Criteria**:
> 1. Given a webhook serving cert within 30 days of expiry, When the reconciler runs,
>    Then a new Certificate CR is created targeting the serving-cert Secret.
> 2. Given cert-manager issues a new certificate, When the Secret is updated, Then the
>    webhook configuration is patched with the new CA bundle within 60 seconds.
> 3. Given cert-manager is unavailable, When certificate rotation is attempted, Then the
>    operator logs a warning event, sets a Degraded status condition, and continues serving
>    with the existing cert.
>
> **Scope**: Only webhook serving certs. Mutual TLS and client certs are out of scope.
> No changes to the CRD API.
>
> **Dependencies**: Requires cert-manager v1.12+ with Certificate CRD. No database migrations.
>
> **Impacted Repos**: openshift/cert-manager-operator (operator logic),
> openshift/cert-manager-operator (e2e tests).
>
> **RBAC**: Operator ServiceAccount needs `get/create/update` on
> `certificates.cert-manager.io` in the operator namespace. No cluster-wide secret access added.
>
> **Upgrade**: Existing clusters without rotation get it automatically on operator upgrade.
> No migration needed. Downgrade: rotation CRs are ignored by older operator versions
> (no cleanup needed).

**Expected scores:** completeness_score: 92, quality_score: 88, overall_score: 90,
overall_status: PASS. `cert_manager_ecosystem.platform_matrix_addressed`: false with gap
"Platform matrix: Does this apply to MicroShift or only full OpenShift? Are there
FeatureGate requirements?"

##### Example 2: Contradictory Spec (BLOCKED)

**Input spec text:**
> **Title**: Add new TrustPolicy CRD for cross-namespace certificate issuance
>
> **Description**: Add a cluster-scoped TrustPolicy CRD that allows namespaces to reference
> issuers from other namespaces. The CRD should be namespace-scoped so tenants can manage
> their own policies. On uninstall, all TrustPolicy CRs must be preserved for audit. The
> operator finalizer must delete all TrustPolicy CRs on uninstall to avoid orphans.

**Expected scores:** overall_score: 39, overall_status: BLOCKED.
Blockers: "CRD scope contradiction: spec says both cluster-scoped and namespace-scoped",
"Uninstall semantics contradiction: spec says both preserve and delete CRs".
All `cert_manager_ecosystem` booleans false, with gaps covering API lifecycle contradiction,
RBAC blast radius, install/uninstall contradiction, webhooks, platform matrix, observability,
and upgrade path.
