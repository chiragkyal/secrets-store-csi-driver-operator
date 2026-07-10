# RBAC Relevance Finding — `serviceaccounts/token: create`

**Change:** csi-secrets-store-rotation-and-wif  
**Task:** T3_1  
**Date:** 2026-07-10  
**Reviewer:** RBACSecurity_Agent  
**Plan reference:** §8 #3 (RBAC relevance)  
**Asset:** `assets/rbac/secretproviderclasses_role.yaml` (lines 34–39)

## Question

Is the node operand ClusterRole grant `serviceaccounts/token: create` **required** for the new operator-managed WIF flow that sets `CSIDriver.spec.tokenRequests` via `ClusterCSIDriver.spec.driverConfig.secretsStore`?

## Conclusion

**No RBAC change is required for this feature.**

The `serviceaccounts/token: create` rule is **not relevant** to kubelet-driven `CSIDriver.spec.tokenRequests`. It is a **legacy vestige** of the pre-v1.6.0 in-tree rotation controller. This rotation/WIF feature operates correctly without adding, modifying, or depending on that permission.

| Decision | Detail |
|----------|--------|
| **This change** | No edit to `assets/rbac/secretproviderclasses_role.yaml` |
| **Optional follow-up** | Separate cleanup PR to remove the vestigial rule (out of scope here per Constitution Principle VI) |

---

## Mechanism: how WIF tokens flow

```mermaid
sequenceDiagram
    participant Admin
    participant Operator
    participant APIServer
    participant Kubelet
    participant CSIDriverPod as CSI node plugin

    Admin->>APIServer: ClusterCSIDriver driverConfig.secretsStore.tokenRequests
    Operator->>APIServer: Reconcile CSIDriver.spec.tokenRequests + requiresRepublish
    Note over Kubelet: On mount/remount
    Kubelet->>APIServer: TokenRequest for pod SA + audience
    Kubelet->>CSIDriverPod: NodePublishVolume volume_context with token
    Note over CSIDriverPod: Uses token from context; does not call serviceaccounts/token
```

**Sources:**
- [Kubernetes CSI Token Requests documentation](https://kubernetes-csi.github.io/docs/token-requests.html) — kubelet mints tokens and injects them into `VolumeContext` (`csi.storage.k8s.io/serviceAccount.tokens`).
- [KEP 1855: CSI Driver Service Account Token](https://github.com/kubernetes/enhancements/blob/master/keps/sig-storage/1855-csi-driver-service-account-token/README.md) — design intent is to avoid granting TokenRequest API access to the CSI driver's service account.

**Operator scope:** This repo sets `CSIDriver.spec.tokenRequests` and `requiresRepublish` via `pkg/operator/csidriver_asset.go`. The operator's own CSV RBAC already includes `clustercsidrivers` get/list/watch/update/patch — sufficient. The **node** SA does not need `serviceaccounts/token: create` for the operator to configure audiences.

---

## Evidence: legacy permission vs current upstream

| Evidence | Finding |
|----------|---------|
| OpenShift asset comment | `# for CSI driver token requests` on `serviceaccounts/token` rule — predates kubelet-native mechanism |
| Upstream maintainer (May 2026) | [kubernetes-sigs/secrets-store-csi-driver#1976](https://github.com/kubernetes-sigs/secrets-store-csi-driver/issues/1976#issuecomment-4383075406): permission was for removed rotation controller; **no longer required** since v1.6.0 `requiresRepublish` model |
| Upstream chart (post-v1.6.0) | `rbac-secretprovidertokenrequest.yaml` removed; current `role.yaml` has no `serviceaccounts/token` (confirmed in prior archived discovery T3_5 via `gh api`) |
| AWS/GCP/Azure providers | Provider auth docs reference tokens from CSI `VolumeContext`, not driver-initiated `serviceaccounts/token` create ([AWS #400](https://github.com/aws/secrets-store-csi-driver-provider-aws/issues/400)) |

---

## OpenShift asset analysis

Current rule in `secretproviderclasses_role.yaml`:

```yaml
- apiGroups: # for CSI driver token requests
  - ""
  resources:
  - serviceaccounts/token
  verbs:
  - create
```

| RBAC rule | Relevant to new WIF? | Notes |
|-----------|---------------------|-------|
| `serviceaccounts/token: create` | **No** | Kubelet performs TokenRequest; driver receives token in mount context |
| `secrets` CRUD | **Unrelated to WIF** | Secret rotation/sync feature (matches upstream optional `role-syncsecret.yaml`) |
| `csidrivers` get/list/watch | **Yes (read-only)** | Driver reads its CSIDriver object; unchanged |

---

## Risk if removed (follow-up only)

Removing `serviceaccounts/token: create` in a **separate** change should be safe for clusters on current driver versions using `requiresRepublish` + `tokenRequests`, but:

- Requires confirmation against the **exact OpenShift-pinned driver image** version and release note review.
- Should not be bundled into this operator feature PR to avoid widening blast radius (Constitution Principle VI).

---

## Downstream handoff (T5_2 / implementation-report)

- **RBAC conclusion:** No change needed for rotation/WIF feature merge.
- **Plan §8 #3:** Resolved — permission is legacy, not required for kubelet-driven tokenRequests.
- **No ad-hoc RBAC task** spawned before T5_2 unless product/security mandates explicit removal in this release (SME decision).
