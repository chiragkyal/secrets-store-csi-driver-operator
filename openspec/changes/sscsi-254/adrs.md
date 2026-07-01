# Architectural Decision Records: SSCSI-254

## ADR-001: Pure-Spec Parameter Instead of OperatorClient

**Decision:** `getRotationConfig` and `getTokenRequests` accept `*opv1.ClusterCSIDriverSpec` directly instead of `v1helpers.OperatorClientWithFinalizers`.

**Context:** The tasks.md specified using `operatorClient.GetOperatorState()` but that method returns `*opv1.OperatorSpec` (generic base type). Type-asserting it to `*opv1.ClusterCSIDriverSpec` is not possible — the library-go operator client extracts only the embedded `OperatorSpec` fields, not the CRD-specific fields like `DriverConfig`.

**Consequence:** Callers (`enrichedCSIDriverAssetFunc`, `rotationArgsDaemonSetHook`) must read the full `ClusterCSIDriver` via the dynamic client and pass the spec. This adds a `dynamicClient` call on each reconcile but makes helper functions pure and independently testable.

---

## ADR-002: sigs.k8s.io/yaml + encoding/json Instead of resourceread

**Decision:** `enrichedCSIDriverAssetFunc` uses `sigsyaml.Unmarshal` + `json.Marshal` to roundtrip `csidriver.yaml`.

**Context:** The plan and tasks referenced `resourceread.ReadCSIDriverV1OrDie` from library-go, but that package is not vendored in this repo.

**Consequence:** Functionally equivalent — `sigs.k8s.io/yaml` converts YAML→JSON internally before unmarshaling, and `json.Marshal` produces valid bytes for `resourceapply.ApplyCSIDriver`. TypeMeta is preserved from the unmarshal step.

---

## ADR-003: ClusterCSIDriver Description Update Instead of specDescriptors

**Decision:** T6_1 updates the CSV `spec.description` rather than adding `specDescriptors` for `secretRotation` / `tokenRequests`.

**Context:** `ClusterCSIDriver` is a platform CRD owned by the `openshift/api` bundle, not this operator's OLM bundle. Adding `specDescriptors` for a non-owned CRD in this CSV would be incorrect OLM practice.

**Consequence:** The new fields are documented at the operator description level. Platform-level UI descriptors for `ClusterCSIDriver` would need to be added upstream in `openshift/api` or its associated OLM manifests.

---

## ADR-004: T7_1 E2E Tests Deferred

**Decision:** E2E test authoring deferred to a follow-up — tests require a live OpenShift cluster.

**Context:** Working-folder mode + no cluster access in this session. The 9 required E2E scenarios are fully specified in `tasks.md §4 T7_1` and `task-reports/T7_1.md`.

**Consequence:** The operator logic is correct and unit-tested. E2E validation must happen before merging the PR to `main`.
