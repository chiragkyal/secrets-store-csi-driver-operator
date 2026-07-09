# Design Bundle — Task T3_1

**Change:** sscsi-254
**Task:** T3_1 — Dynamic `AssetFunc` for `csidriver.yaml`
**Assigned Agent:** ControllerLogic_Agent
**Phase:** Phase 3: Dynamic `CSIDriver` Object Generation (Rotation + WIF Fields)

## Constitution excerpts (binding)

> **Principle II:** New assets MUST be plain YAML files added to `assets/` with `${NAMESPACE}` as the only runtime token... Never add a bindata code-generation step.
> **Principle X:** Never modify `vendor/` directly.

## Specs excerpts

> FR-003/FR-004/FR-011: configure one or more token audiences; support multiple simultaneous audiences with independent validity durations.
> Edge case (resolved Open Question 1 from the source EP, carried via specs.md): `requiresRepublish` mirrors `secretRotation.type` — false only when explicitly `"None"`.

## Task T3_1 Payload (from tasks.md §4)

- **Objective:** Replace the byte-level-only application of `assets/csidriver.yaml` with a dynamic `AssetFunc` that additionally sets `spec.requiresRepublish` and `spec.tokenRequests` based on `T2_1`'s resolved config.
- **Target file(s):** `assets/csidriver.yaml` (base template, content otherwise unchanged); new/extended file in `pkg/operator/`.
- **Non-goals / forbidden edits:** Do not touch `podInfoOnMount`/`attachRequired`/`fsGroupPolicy`/`volumeLifecycleModes`. **Do not implement live-tokenRequests preservation here — that is T3_2's job** (Unmanaged/omitted case just leaves `tokenRequests` unset from this AssetFunc; T3_2 layers the preservation read on top).
- **Implementation notes:** Follow `replaceNamespaceFunc`'s closure shape but decode via `resourceread.ReadCSIDriverV1OrDie`, mutate, re-serialize via `sigs.k8s.io/yaml.Marshal` (already vendored). `requiresRepublish` = `rotation.Enabled` directly (T2_1's resolver already encodes the "false only when explicitly None" rule).
- **Acceptance criteria:** Traces to FR-003, FR-004, FR-011. Verified by `T3_4` (not this task).
- **Downstream handoff:** A working `AssetFunc` that `T3_2` extends with preservation logic and `T3_3` registers into `starter.go`.

## Reusable assets confirmed

- `resourceread.ReadCSIDriverV1OrDie(objBytes []byte) *storagev1.CSIDriver` — vendored decoder, exact library-go precedent for this pattern.
- `sigs.k8s.io/yaml` — vendored, used for `Marshal` to re-serialize the mutated typed object back to bytes for the `AssetFunc` return value.
- `T2_2`'s `operatorv1listers.ClusterCSIDriverLister` (via the informer wired in `starter.go`) — the read path this `AssetFunc` will use to get the live `ClusterCSIDriver` spec (passed as a constructor parameter, matching `WithCABundleDaemonSetHook`'s pattern of accepting the lister/informer directly rather than a closure).

## storagev1.CSIDriverSpec field types confirmed

```go
type CSIDriverSpec struct {
    // ...
    TokenRequests     []TokenRequest `json:"tokenRequests,omitempty"`
    RequiresRepublish *bool          `json:"requiresRepublish,omitempty"`
}
type TokenRequest struct {
    Audience          string `json:"audience"`
    ExpirationSeconds *int64 `json:"expirationSeconds,omitempty"`
}
```

## Execution approach

New file `pkg/operator/csidriverasset.go`: `withSecretsStoreCSIDriverAsset(base resourceapply.AssetFunc, clusterCSIDriverLister operatorv1listers.ClusterCSIDriverLister) resourceapply.AssetFunc` — a wrapping `AssetFunc` that special-cases `csidriver.yaml` only, passing every other asset name through unchanged. Not yet registered in `starter.go` (that's T3_3).
