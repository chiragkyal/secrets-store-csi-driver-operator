This file provides guidance to AI agents working with the **secrets-store-csi-driver-operator** for OpenShift — a Go operator that installs and manages the [Secrets Store CSI Driver](https://secrets-store-csi-driver.sigs.k8s.io/) on OpenShift clusters. It is a **pure library-go operator** using `CSIControllerSet`; there is no controller-runtime, no custom CRD, and no feature gate mechanism.

## Repository Layout

```
cmd/secrets-store-csi-driver-operator/main.go   # Entrypoint — wires controllercmd to RunOperator
pkg/operator/starter.go                          # ALL reconciliation logic — CSIControllerSet chain
pkg/operator/starter_test.go                     # Unit tests — library-go fake client pattern
pkg/version/version.go                           # Version info
pkg/dependencymagnet/dependencymagnet.go         # Blank-import dep pin
assets/assets.go                                 # Go embed — //go:embed *.yaml rbac/*.yaml network-policy/*.yaml
assets/node.yaml                                 # DaemonSet (CSI node plugin — 3 containers)
assets/node_sa.yaml                              # ServiceAccount for the node DaemonSet
assets/csidriver.yaml                            # CSIDriver object (secrets-store.csi.k8s.io)
assets/cabundle_cm.yaml                          # Trusted CA bundle ConfigMap
assets/rbac/privileged_role.yaml                 # ClusterRole — SCC privileged use
assets/rbac/node_privileged_binding.yaml         # ClusterRoleBinding for privileged_role
assets/rbac/secretproviderclasses_role.yaml      # ClusterRole — secrets, pods, SecretProviderClass CRDs
assets/rbac/secretproviderclasses_binding.yaml   # ClusterRoleBinding for secretproviderclasses_role
assets/network-policy/allow-ingress-to-metrics-operand.yaml  # NetworkPolicy — port 8095 (metrics)
config/manifests/                                # OLM package + CSV manifests
config/metadata/                                 # OLM bundle metadata
hack/create-bundle                               # Build OLM bundle + index images
hack/e2e.sh                                      # E2E test runner (uses openshift-tests)
hack/update-metadata.sh                          # Bump OCP version in CSV + package.yaml
```

## Architecture — One Pattern Only

This operator is **entirely library-go**. There is no controller-runtime, no `ctrl.Manager`, no Server-Side Apply via `client.Apply`, and no kubebuilder markers. All reconciliation is delegated to library-go's `CSIControllerSet`.

**Never apply controller-runtime or cert-manager-operator addon patterns here.**

### CSIControllerSet Chain (in `starter.go`)

```
csicontrollerset.NewCSIControllerSet(operatorClient, eventRecorder)
  .WithLogLevelController()
  .WithManagementStateController(operandName, removable=true)
  .WithConditionalStaticResourcesController("SecretsStoreConditionalStaticResourcesController", ...)
  .WithCSIConfigObserverController("SecretsStoreDriverCSIConfigObserverController", configInformers)
  .WithCSIDriverNodeService("SecretsStoreDriverNodeServiceController", ..., "node.yaml", ...)
```

| Controller | Library-go package | Purpose |
|------------|--------------------|---------|
| `LogLevelController` | `operator/loglevel` | Propagates `spec.logLevel` to operand |
| `ManagementStateController` | `operator/management` | Handles Managed/Unmanaged/Removed transitions |
| `ConditionalStaticResourcesController` | `operator/resource/resourceapply` | Applies/deletes 8 static assets based on management state |
| `CSIConfigObserverController` | `operator/csi/csidrivercontroller` | Observes cluster config (Infrastructure, Proxy) |
| `CSIDriverNodeService` | `operator/csi/csidrivernodeservicecontroller` | Manages the DaemonSet with CA bundle hook |

### Management State Machine

`getOperatorSyncState(operatorClient)` returns one of three states that control whether static resources are applied or deleted:

| State | Trigger | Effect on static resources |
|-------|---------|---------------------------|
| `Managed` | `spec.managementState: Managed` | Apply all 8 assets |
| `Unmanaged` | `spec.managementState: Unmanaged` | Do not sync (leave as-is) |
| `Removed` | `spec.managementState: Removed` OR deletion timestamp set | Delete conditional resources |

`WithManagementStateController(operandName, true)` — the `true` flag marks this operator as **removable** (allows CR deletion to trigger cleanup).

## Static Assets (Managed by ConditionalStaticResourcesController)

All assets are embedded via Go embed in `assets/assets.go`. `assets.ReadFile(name)` reads them. The `replaceNamespaceFunc(namespace)` function replaces every `${NAMESPACE}` token before applying.

| Asset | Kind | Notes |
|-------|------|-------|
| `node_sa.yaml` | ServiceAccount | Node DaemonSet SA |
| `csidriver.yaml` | CSIDriver | `secrets-store.csi.k8s.io`, ephemeral-only, `csi.openshift.io/managed: "true"` |
| `cabundle_cm.yaml` | ConfigMap | `config.openshift.io/inject-trusted-cabundle: "true"` — CNO injects CA |
| `rbac/privileged_role.yaml` | ClusterRole | SCC `privileged` use for node plugin |
| `rbac/node_privileged_binding.yaml` | ClusterRoleBinding | Binds privileged_role to node SA |
| `rbac/secretproviderclasses_role.yaml` | ClusterRole | Broad RBAC: secrets, pods, SA tokens, SecretProviderClass + Status CRDs |
| `rbac/secretproviderclasses_binding.yaml` | ClusterRoleBinding | Binds secretproviderclasses_role |
| `network-policy/allow-ingress-to-metrics-operand.yaml` | NetworkPolicy | Allows ingress to port 8095 (metrics) on DaemonSet pods |

**To add a new static asset:** create the YAML in `assets/` (or a subfolder already covered by the `//go:embed` glob), add its filename to the `WithConditionalStaticResourcesController` asset list in `starter.go`.

## Node DaemonSet (`node.yaml`)

The CSI driver runs as a **DaemonSet** (not a Deployment) — CSI requires a node plugin on every node.

Three containers:
| Container | Image env var | Purpose |
|-----------|--------------|---------|
| `csi-driver` | `DRIVER_IMAGE` | Main CSI driver (provider interaction, secret rotation) |
| `csi-node-driver-registrar` | `NODE_DRIVER_REGISTRAR_IMAGE` | Registers CSI plugin socket with kubelet |
| `csi-liveness-probe` | `LIVENESS_PROBE_IMAGE` | Health probe sidecar |

Key DaemonSet settings (do not change without understanding impact):
- `priorityClassName: system-node-critical` — prevents eviction on node pressure
- `cluster-autoscaler.kubernetes.io/enable-ds-eviction: "false"` — protects from scale-down
- `tolerations: [{operator: Exists}]` — runs on all nodes including tainted masters
- `updateStrategy: RollingUpdate, maxUnavailable: 10%`
- Provider volume mounts: `/etc/kubernetes/secrets-store-csi-providers` + `/var/run/secrets-store-csi-providers`

**CA bundle** is mounted via `WithCABundleDaemonSetHook(operatorNamespace, "secrets-store-csi-driver-trusted-ca-bundle", configMapInformer)` — this adds a projected volume from the CNO-injected ConfigMap into the DaemonSet pod.

## Key Operator Wiring (`starter.go`)

```go
// API object
gvr := opv1.SchemeGroupVersion.WithResource("clustercsidrivers")
gvk := opv1.SchemeGroupVersion.WithKind("ClusterCSIDriver")
operatorClient, dynamicInformers, _ := goc.NewClusterScopedOperatorClientWithConfigName(
    clock.RealClock{}, cfg, gvr, gvk,
    "secrets-store.csi.k8s.io",  // singleton name — the providerName
    extractOperatorSpec, extractOperatorStatus,
)
```

`extractOperatorSpec` / `extractOperatorStatus` convert the unstructured `ClusterCSIDriver` to typed apply-configurations using `applyoperatorv1.ExtractClusterCSIDriver`.

**There is no custom CRD.** `ClusterCSIDriver` is a generic operator API owned by `openshift/api`. Do not attempt to add kubebuilder markers or run `make generate` for type changes — the types are upstream.

## Image Environment Variables

All three image env vars are set at operator startup (not in spec):

| Env var | Container in node.yaml | Description |
|---------|------------------------|-------------|
| `DRIVER_IMAGE` | `csi-driver` | Secrets Store CSI driver image |
| `NODE_DRIVER_REGISTRAR_IMAGE` | `csi-node-driver-registrar` | Node registrar sidecar |
| `LIVENESS_PROBE_IMAGE` | `csi-liveness-probe` | Liveness probe sidecar |

These are injected as environment variables on the operator pod and referenced via `${DRIVER_IMAGE}` etc. in `node.yaml`.

## OLM / Bundle

OLM metadata lives in `config/`. Bump OCP version with:
```bash
make metadata VERSION=4.20
# or directly:
./hack/update-metadata.sh 4.20
```
This updates `config/manifests/secrets-store-csi-driver-operator.package.yaml` and the CSV (`config/manifests/stable/*.clusterserviceversion.yaml`).

Build bundle + index images:
```bash
cd hack && ./create-bundle <driver-image> <operator-image> <bundle-repo> <index-repo>
```

## Common Mistakes

1. **Do NOT use controller-runtime patterns** — no `ctrl.Manager`, no `reconcile.Reconciler`, no SSA `client.Apply`
2. **Do NOT create per-resource reconciler files** — all reconciliation is in the `CSIControllerSet` chain; modify `starter.go`
3. **Do NOT create a custom CRD type** — `ClusterCSIDriver` is from `openshift/api`, not this repo
4. **Do NOT run `make generate` or `make manifests`** — no code generation for CRD types in this repo
5. **Do NOT add feature gates** — this operator has no feature gate mechanism
6. **Do NOT add webhooks** — no admission webhooks; CA bundle is handled by CNO injection
7. **Do NOT change `priorityClassName` or tolerations** on the DaemonSet — these are required for node-critical operation
8. **Do NOT add assets outside the embedded glob paths** — the `//go:embed` only covers `*.yaml`, `rbac/*.yaml`, `network-policy/*.yaml`; add a new subfolder pattern if needed

---

## Per-Task Testing (Code Generation Eval Gate)

During `/opsx-apply`, every code generation task is verified with **real command execution**.

| Task type | Verification command | Test strategy |
|-----------|---------------------|---------------|
| `starter.go` wiring changes | `go build ./... && go vet ./...` | Build + vet |
| New static asset + starter wiring | `go build ./... && make check` | Build + unit + verify |
| Asset YAML changes only | `go build ./...` | Build (embed picks up YAML) |
| OLM/CSV/metadata changes | `./hack/update-metadata.sh && go build ./...` | Script + build |
| Unit test additions | `go test ./pkg/... ./cmd/... -v -count=1` | Full unit suite |
| E2E tests | `hack/e2e.sh` (requires live cluster) | E2E runner |

**`make check`** runs `make verify && make test-unit` (see Makefile).

### Unit Test Pattern (`starter_test.go`)

Tests use `v1helpers.NewFakeOperatorClientWithObjectMeta` — a library-go fake (not counterfeiter). Pattern:

```go
// Table-driven, t.Run, no external mock framework
cases := []struct {
    name          string
    operator      *FakeOperator  // local struct embedding ObjectMeta + Spec + Status
    expectedState opv1.ManagementState
}{
    {
        name: "should return managed when the operator state is managed",
        operator: &FakeOperator{
            ObjectMeta: metav1.ObjectMeta{Name: providerName},
            Spec:       opv1.OperatorSpec{ManagementState: opv1.Managed},
        },
        expectedState: opv1.Managed,
    },
    // ...
}
for _, tc := range cases {
    t.Run(tc.name, func(t *testing.T) {
        operatorClient := v1helpers.NewFakeOperatorClientWithObjectMeta(
            &tc.operator.ObjectMeta, &tc.operator.Spec, &tc.operator.Status, nil,
        )
        state := getOperatorSyncState(operatorClient)
        if state != tc.expectedState {
            t.Fatalf("expected %v, got %v", tc.expectedState, state)
        }
    })
}
```

**First test added for a new function**: follow the `FakeOperator` + `NewFakeOperatorClientWithObjectMeta` pattern. No counterfeiter. No third-party mock framework. Standard `t.Fatalf` for failure.

---

## Execution Agent Routing

Use these **Assigned Agent** IDs in `tasks.md` §3 when **`AgentRoutingMode: PROVIDED`**.

| Agent ID | Scope | OAPE / execution |
|----------|-------|-----------------|
| **OperatorController_Agent** | `pkg/operator/starter.go` — CSIControllerSet chain wiring, new controller hooks, `getOperatorSyncState`, `replaceNamespaceFunc`, image env vars | `api-implement` |
| **ManifestsAssets_Agent** | `assets/*.yaml`, `assets/rbac/*.yaml`, `assets/network-policy/*.yaml` — YAML manifest content changes, new static asset files | Manual — edit YAML, `go build ./...` |
| **OLMRelease_Agent** | `config/manifests/`, `config/metadata/` — CSV, package manifest, `make metadata`, `hack/create-bundle` | Manual |
| **Testing_Agent** | E2E tests — `hack/e2e.sh`, test fixture setup; unit tests in `pkg/operator/starter_test.go` | `e2e-generate` for E2E; `api-implement` for unit tests |
| **Docs_Agent** | `README.md`, `must-gather/` docs | Manual |

### Routing Rules

- **Single-file operator logic**: tasks touching `starter.go` go to `OperatorController_Agent` via `api-implement`.
- **Asset-only changes** (YAML content, new manifests): `ManifestsAssets_Agent` — manual, no OAPE command needed; just edit + `go build ./...` to verify embed.
- **Both starter.go + assets**: split into two tasks — assets first (no compile dependency), then wiring in starter.go.
- **OLM bumps** (`make metadata`, CSV updates): always `OLMRelease_Agent` — separate task from code changes.
- **No `api-generate` tasks**: there are no custom CRD types to generate.
- **No `api-generate-tests` tasks**: no API validation testsuites (no kubebuilder CRDs).

---

## Stage-Specific Hints

### Repo-Assessment Stage

**Architecture (§1):**
- Document the single-file operator pattern: all reconciliation in `starter.go` via `CSIControllerSet`. There are no per-resource reconciler files.
- Call out that `ClusterCSIDriver` is a generic upstream API — **not** a custom CRD implemented in this repo.
- Document the Managed/Unmanaged/Removed state machine and the removable operator behavior.
- The DaemonSet is the primary workload — document why it is a DaemonSet (CSI node plugin requirement) and its node-critical settings.

**Anti-patterns (forbidden without branch evidence):**
- Claiming there are feature gates — there are none.
- Claiming there are admission webhooks — there are none.
- Claiming controller-runtime is used — it is not.
- Claiming per-resource reconciler files exist (`serviceaccounts.go`, `deployments.go`, etc.) — they do not.

**Asset inventory (§2 / §5):** List all 8 managed assets with their Kind and purpose. Note the `${NAMESPACE}` substitution pattern.

**Testing (§8):**
- Unit tests: `go test ./pkg/... ./cmd/... -count=1` or `make test-unit`
- Verify: `make verify` (runs build-machinery-go checks)
- Combined: `make check` (= verify + test-unit)
- E2E: `hack/e2e.sh` — requires live OpenShift cluster + `openshift-tests` in PATH; not runnable locally

**Build (§9):**
- FIPS: `GOEXPERIMENT=strictfipsruntime` + `CGO_ENABLED=1` + `-tags strictfipsruntime,openssl` when supported
- Non-FIPS local build: `CGO_ENABLED=1 go build` (warns, not for CI/production)

### Planning Stage

Prefer CSI-native thinking:
- DaemonSet lifecycle (rolling update, node-critical priority, tolerations)
- CSI driver registration socket path changes (`/var/lib/kubelet/plugins/csi-secrets-store/`)
- Provider volume mount paths (`/etc/kubernetes/secrets-store-csi-providers`, `/var/run/secrets-store-csi-providers`)
- Secret rotation interval (`--rotation-poll-interval=2m`) and provider health check settings
- CA bundle injection via CNO ConfigMap (`secrets-store-csi-driver-trusted-ca-bundle`)
- OLM CSV changes when RBAC or images change
- `make metadata VERSION=x.y` for OCP version bumps

### Validation Stage

CSI ecosystem pillars to evaluate when a spec touches this operator:
- **CSI driver registration**: socket path, kubelet plugin dir, `csidriver.yaml` spec compatibility
- **Provider integration**: provider volume paths, provider health check, `--provider-health-check-interval`
- **Secret rotation**: `--enable-secret-rotation=true`, `--rotation-poll-interval=2m` — impact on spec fields
- **RBAC blast radius**: `secretproviderclasses_role.yaml` grants wide secrets access — evaluate any expansion carefully
- **Network policy**: metrics port 8095 ingress — any new ports need NetworkPolicy updates
- **Management state**: Managed/Removed semantics — does the spec handle operator removal correctly?
- **CA bundle**: `WithCABundleDaemonSetHook` — any new containers need the CA mount
- **OLM upgrade**: `olm.skipRange`, `olm.maxOpenShiftVersion` — do CSV changes respect upgrade edges?
