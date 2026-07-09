# Design Bundle — Task T3_3

**Change:** sscsi-254
**Task:** T3_3 — Register the new `AssetFunc` in `WithConditionalStaticResourcesController`
**Assigned Agent:** ControllerLogic_Agent
**Phase:** Phase 3: Dynamic `CSIDriver` Object Generation (Rotation + WIF Fields)

## Task T3_3 Payload (from tasks.md §4)

- **Objective:** Swap the current generic `AssetFunc` reference for `csidriver.yaml` with the new dynamic one from `T3_1`/`T3_2`, in the existing controller-set wiring.
- **Target file(s):** `pkg/operator/starter.go` (`WithConditionalStaticResourcesController` call).
- **Non-goals / forbidden edits:** Do not alter the other 7 files in that call's file list — they remain on the existing generic `replaceNamespaceFunc`.
- **Implementation notes:** Per-file `AssetFunc` override may require restructuring — verify the vendored signature and adapt with a dispatcher `AssetFunc` that special-cases `csidriver.yaml`.
- **Acceptance criteria:** `make verify && make test-unit` passes; manual verification: `oc get csidriver secrets-store.csi.k8s.io -o yaml`.
- **Downstream handoff:** Feature-complete `CSIDriver` reconciliation ready for `T7_3`/`T7_4` e2e coverage.

## Confirmed mechanism (from repo-assessment.md §4.2 / vendored source)

`csicontrollerset.WithConditionalStaticResourcesController` takes a single `manifests resourceapply.AssetFunc` parameter and passes it to **both** the base `staticresourcecontroller.NewStaticResourceController(...)` call and the subsequent `.WithConditionalResources(manifests, files, ...)` call — meaning our call site only needs to swap **one** argument. `withSecretsStoreCSIDriverAsset` (from `T3_1`/`T3_2`) is already exactly the "dispatcher AssetFunc that special-cases csidriver.yaml, passes everything else through" the task anticipated — no additional restructuring needed.

## New read path for T3_2's live-CSIDriver preservation lister

`T3_2` added a `liveCSIDriverLister storagev1listers.CSIDriverLister` parameter but did not wire an actual instance (target files excluded `starter.go`). This task supplies it via `kubeInformersForNamespaces.InformersFor("").Storage().V1().CSIDrivers()` — the same cluster-scoped (`""`) factory access pattern library-go's own `AddKubeInformers` uses internally for cluster-scoped kinds like `CSIDriver` (confirmed in `repo-assessment.md` §4.2: `informer = kubeInformersByNamespace.InformersFor(metadata.GetNamespace())`, which resolves to `""` for cluster-scoped objects). No new informer factory is needed — `kubeInformersForNamespaces` already exists and is already started.

## Execution approach

In `starter.go`: obtain `kubeInformersForNamespaces.InformersFor("").Storage().V1().CSIDrivers()`, then change the `WithConditionalStaticResourcesController(...)` call's manifests argument from `replaceNamespaceFunc(operatorNamespace)` to `withSecretsStoreCSIDriverAsset(replaceNamespaceFunc(operatorNamespace), clusterCSIDriverInformer.Lister(), csiDriverInformer.Lister())`. No other files in the conditional-resources list are affected — `withSecretsStoreCSIDriverAsset` passes them through unchanged.
