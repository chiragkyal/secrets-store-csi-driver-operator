# Upgrade Preservation Manual Runbook (SC-005)

**Change:** csi-secrets-store-rotation-and-wif  
**Task:** T4_3  
**Date:** 2026-07-10  
**Scope:** Upgrade-only (downgrade after `tokenRequests.type: Managed` is **out of scope** — Plan §8 #1)

## Automation decision

| Option | Status | Rationale |
|--------|--------|-----------|
| Automated `hack/e2e.sh` upgrade test | **Not implemented** | Requires swapping operator image/version mid-test; no upgrade harness in this repo |
| Manual runbook | **Accepted** | Per T4_3 acceptance criteria and task Non-goals |
| Unit-level guarantees | **Present** | See cross-reference table below |

Embedded copy also lives in `hack/e2e.sh` (comment block before rotation tests).

---

## Requirements traced

| ID | Verification |
|----|--------------|
| SC-005 | Upgrade retains rotation cadence + manual tokenRequests |
| FR-003 | Default rotation when unconfigured |
| FR-005 | Preserve existing tokenRequests when omitted |
| FR-010 | Retain config across upgrades |
| FR-012 | No change for non-opted-in clusters |

---

## Prerequisites

- OpenShift cluster with Secrets Store CSI Driver operator installed (pre-feature or feature-enabled with `driverConfig.secretsStore` unset)
- `oc` CLI with cluster admin or sufficient permissions
- Ability to upgrade operator via OLM channel (normal release path)

Environment variables (match `hack/e2e.sh`):

```bash
export PROVISIONER_NAME="secrets-store.csi.k8s.io"
export E2E_PROVIDER_NAMESPACE="openshift-cluster-csi-drivers"
export DS_NAME="secrets-store-csi-driver-node"
export DS_CONTAINER_NAME="csi-driver"
```

---

## Manual verification steps

### Step 1 — Record pre-upgrade baseline

On a cluster **without** `driverConfig.secretsStore` configured:

```bash
# Simulate pre-feature manual WIF configuration (FR-005)
oc patch csidriver ${PROVISIONER_NAME} --type=merge \
  -p '{"spec":{"tokenRequests":[{"audience":"pre-existing-manual-audience"}]}}'

# Record rotation DaemonSet args (expect baseline: enabled @ 2m)
oc get ds -n ${E2E_PROVIDER_NAMESPACE} ${DS_NAME} \
  -o jsonpath='{.spec.template.spec.containers[?(@.name=="'"${DS_CONTAINER_NAME}"'")].args}{"\n"}'

# Confirm driverConfig unset
oc get clustercsidriver ${PROVISIONER_NAME} -o jsonpath='{.spec.driverConfig}{"\n"}'
```

Save outputs for step 3 comparison.

### Step 2 — Upgrade operator

Upgrade the operator to the version containing this feature via OLM (cluster upgrade or subscription channel change). **Do not** patch `ClusterCSIDriver.spec.driverConfig.secretsStore` during upgrade.

Wait for operator pod rollout and one reconciliation cycle.

### Step 3 — Post-upgrade assertions

Re-run read commands from step 1:

| Check | Expected | FR/SC |
|-------|----------|-------|
| `CSIDriver.spec.tokenRequests` contains `pre-existing-manual-audience` | Unchanged | FR-005, FR-010, SC-005 |
| DaemonSet `--enable-secret-rotation=` / `--rotation-poll-interval=` | Identical to step 1 | FR-003, FR-012, SC-005 |
| Workloads with existing mounts | No forced restart required | FR-009 |

**Pass criteria:** All three assertion rows hold.

---

## Unit / E2E cross-reference (offline guarantees)

| Guarantee | Offline test |
|-----------|--------------|
| Default DaemonSet args byte-for-byte baseline | `TestDefaultPathMatchesPreFeatureBaseline` (`rotation_test.go`) |
| tokenRequests preservation on nil/Unmanaged paths | `TestGetTokenRequests`, `TestNewDynamicCSIDriverAssetFunc` |
| Unmanaged preserves manual CSIDriver audiences | `test_wif_unmanaged_preservation` in `hack/e2e.sh` (single-version cluster; not full upgrade) |

---

## Live cluster status (this session)

| Check | Result |
|-------|--------|
| Manual runbook documented | **PASS** |
| Automated upgrade e2e | **N/A** (by design) |
| Cluster execution | **BLOCKED** — kubeconfig API unreachable (same blocker as T4_1/T4_2) |

### Reproduction when cluster available

Execute steps 1–3 above after OLM upgrade, or follow the embedded runbook in `hack/e2e.sh`.

---

## Known limitations

- **Downgrade** after setting `tokenRequests.type: Managed` — undefined; document in T5_2 / `implementation-report.md` (Plan §8 #1).
- Manual runbook confirms **upgrade** path only; it does not replace CI automation for future release pipelines.
