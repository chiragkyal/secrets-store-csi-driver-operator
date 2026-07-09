# Design Bundle — Task T6_1

**Change:** sscsi-254
**Task:** T6_1 — Verify/close RBAC gaps against the finalized read-path mechanism
**Assigned Agent:** StaticAssets_Agent
**Phase:** Phase 6: RBAC Verification

## Constitution excerpts (binding)

> **Principle VI — RBAC Is Least-Privilege and Asset-Driven:** All RBAC for the operand is defined as explicit YAML manifests in `assets/rbac/`. New RBAC requirements MUST be added as YAML files in `assets/rbac/` and registered in the asset list — never granted inline or dynamically at runtime.

## Task T6_1 Payload (from tasks.md §4)

- **Objective:** Confirm the RBAC verbs already granted for `clustercsidrivers`/`clustercsidrivers/status`/`csidrivers` are sufficient for whatever mechanism `T2_2` finalized; add the minimal necessary verb in both places if a gap is found.
- **Non-goals:** Do not grant RBAC inline/dynamically — any new verb must be an explicit YAML change in `assets/rbac/`, mirrored in the CSV.
- **Implementation notes:** Expected outcome, per `plan.md` §3.4, is **no change** — primarily a verification step.
- **Acceptance criteria:** Documented confirmation that the finalized mechanism's calls are covered by existing RBAC, OR a matching pair of edits if not.

## Finalized mechanism (confirmed via T2_2, T3_2, T3_3)

1. `clusterCSIDriverClient` (typed `operator.openshift.io/v1` clientset) → `clusterCSIDriverInformers.Operator().V1().ClusterCSIDrivers()` → used only for **List/Watch** (informer cache population) and `Lister().Get()` (in-memory cache reads, no additional API calls).
2. `csiDriverInformer` (`kubeInformersForNamespaces.InformersFor("").Storage().V1().CSIDrivers()`) → same pattern: **List/Watch** for cache population, `Lister().Get()` for reads.
3. No code path anywhere in `T2_1`–`T4_3` makes a live `Get`/`Create`/`Update`/`Delete` call against either resource directly — all reads go through listers; all writes (the `CSIDriver` object mutation) go through the existing `resourceapply.ApplyCSIDriver` path used since before this feature (already covered by existing RBAC).

## RBAC verification (from `config/manifests/stable/secrets-store-csi-driver-operator.clusterserviceversion.yaml`)

| Resource | Granted verbs | Needed by finalized mechanism | Gap? |
|---|---|---|---|
| `operator.openshift.io/clustercsidrivers` (lines 140-150) | get, list, watch, update, patch | get, list, watch (informer) | **No gap** |
| `operator.openshift.io/clustercsidrivers/status` (lines 151-160) | get, list, watch, update, patch | Not used by this feature | **No gap** |
| `storage.k8s.io/csidrivers` (lines 274-284) | create, get, list, watch, update, delete | get, list, watch (informer) + create/update/delete (existing `ApplyCSIDriver` path) | **No gap** |

## Execution approach

This is a verification-only task. No code or RBAC changes are made — the confirmation above closes the task per its own "expected outcome: no change" framing.
