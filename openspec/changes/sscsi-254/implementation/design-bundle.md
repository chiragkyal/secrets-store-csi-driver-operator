# Design Bundle — Task T3_2

**Change:** sscsi-254
**Task:** T3_2 — `tokenRequests` preservation-on-upgrade logic
**Assigned Agent:** ControllerLogic_Agent
**Phase:** Phase 3: Dynamic `CSIDriver` Object Generation (Rotation + WIF Fields)

## Task T3_2 Payload (from tasks.md §4)

- **Objective:** Extend `T3_1`'s `AssetFunc` to read the **live** `CSIDriver` object's existing `spec.tokenRequests` and preserve it whenever the resolved config is `Unmanaged`/omitted.
- **Target file(s):** Same file(s) as `T3_1` (`pkg/operator/csidriverasset.go` and its test file) — **not** `starter.go` (wiring is `T3_3`'s job).
- **Non-goals / forbidden edits:** Do not implement this preservation logic anywhere outside the `AssetFunc`.
- **Implementation notes:** Cascade: `driverType != SecretsStore` → return existing; `tokenRequests` nil/`Unmanaged` → return existing; `Managed` with `managed.audiences` → use audiences (empty clears). This is entirely new code with no precedent elsewhere in this repo.
- **Acceptance criteria:** Traces to FR-006, FR-008, User Story 3. Verified by `T3_4` (not this task, but a smoke test is added here per mandatory verification).
- **Downstream handoff:** A complete, preservation-aware `AssetFunc` ready for registration in `T3_3`.

## New read path needed: live `storage.k8s.io/v1 CSIDriver` lister

Per `repo-assessment.md` §4.2, `WithConditionalStaticResourcesController`'s `AddKubeInformers` call already adds a `storage.k8s.io/v1 CSIDrivers` informer internally (since `csidriver.yaml` is in the applied file list) — but that informer is internal to the static-resource controller, not exposed to our code. This task needs its **own** read access to the live `CSIDriver` object's current `spec.tokenRequests`.

`k8s.io/client-go/listers/storage/v1.CSIDriverLister` is already vendored (`Get(name) (*storagev1.CSIDriver, error)`). Per the same reasoning as `T2_2` (context-free `AssetFunc` signature rules out a live client `Get`), this task threads an **additional constructor parameter** — `liveCSIDriverLister storagev1listers.CSIDriverLister` — into `withSecretsStoreCSIDriverAsset` from `T3_1`. Wiring an actual instance of this lister (e.g. via `kubeInformersForNamespaces.InformersFor("").Storage().V1().CSIDrivers()`, already-established cluster-scoped informer factory from `starter.go`) is deferred to `T3_3`, consistent with the task boundary (this task's target files exclude `starter.go`).

The live object's name is the same singleton name as the operator's own `ClusterCSIDriver` — `providerName` (`"secrets-store.csi.k8s.io"`), already a defined constant in `starter.go`.

## Execution approach

Extend `pkg/operator/csidriverasset.go`'s `withSecretsStoreCSIDriverAsset` with the new lister parameter and a `getLiveTokenRequests` helper; extend the existing test file's fake setup accordingly (all `withSecretsStoreCSIDriverAsset` call sites in `T3_1`'s tests need updating for the new parameter — not a functional deviation, just a signature change).
