# Design Bundle — Task T2_1

**Change:** sscsi-254
**Task:** T2_1 — Implement shared `ClusterCSIDriver.Spec.DriverConfig` read-path helper
**Assigned Agent:** ControllerLogic_Agent
**Phase:** Phase 2: Shared Read Path

## Constitution excerpts (binding)

> **Principle I — Single Controller Pattern:** Any new operator capability MUST be expressed as either a new CSIControllerSet hook, a new static asset in assets/, or a new informer — never a separate reconciler loop.
> **Principle III — No Custom CRD Types:** Spec-driven behavior changes MUST be expressed through existing ClusterCSIDriver fields... or new controller hooks.

## Specs excerpts (relevant)

> FR-010: When no rotation or token audience configuration is specified, System MUST behave identically to the previously hardcoded default behavior (rotation enabled at the existing fixed interval; any existing token audience settings left unchanged).

## Repo-assessment / plan excerpts

> No code in this repo today reads `ClusterCSIDriver.Spec.DriverConfig` — this is genuinely new plumbing (`repo-assessment.md` §1.3/§4.2, `plan.md` §1).
> This helper is used by BOTH the CSIDriver AssetFunc (T3_1/T3_2) and the DaemonSet hook (T4_1) — do not duplicate logic (`plan.md` §3.2/§11).

## CONFIRMED ACTUAL API SHAPE (from T1_2's vendored bump — supersedes EP assumptions)

Per direct inspection of `vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go` (post T1_2):

```go
type ClusterCSIDriverSpec struct {
    OperatorSpec `json:",inline"`
    StorageClassState StorageClassStateName
    DriverConfig CSIDriverConfigSpec  // VALUE type, not pointer
}

type CSIDriverConfigSpec struct {
    DriverType CSIDriverType  // "" | AWS | Azure | GCP | IBMCloud | vSphere | SecretsStore
    // ... aws/azure/gcp/ibmcloud/vSphere pointer fields ...
    SecretsStore SecretsStoreCSIDriverConfigSpec `json:"secretsStore,omitzero"`  // VALUE type
}

type SecretsStoreCSIDriverConfigSpec struct {
    SecretRotation SecretsStoreSecretRotation `json:"secretRotation,omitzero"`  // VALUE type
    TokenRequests  SecretsStoreTokenRequests  `json:"tokenRequests,omitzero"`   // VALUE type
}

type SecretsStoreSecretRotation struct {
    Type   SecretRotationType `json:"type,omitempty"`  // "" | None | Custom
    Custom CustomSecretRotation `json:"custom,omitzero"`  // VALUE type
}

type CustomSecretRotation struct {
    RotationPollIntervalSeconds int32 `json:"rotationPollIntervalSeconds,omitempty"`
}

type SecretsStoreTokenRequests struct {
    Type    TokenRequestsType `json:"type,omitempty"`  // "" | Managed | Unmanaged
    Managed ManagedTokenRequests `json:"managed,omitzero"`  // VALUE type
}

type ManagedTokenRequests struct {
    Audiences *[]SecretsStoreTokenRequest `json:"audiences,omitempty"`  // ONLY pointer field — nil vs empty-slice distinguishes "omitted" vs "explicitly cleared"
}

type SecretsStoreTokenRequest struct {
    Audience          *string `json:"audience,omitempty"`
    ExpirationSeconds int32   `json:"expirationSeconds,omitempty"`
}
```

**IMPORTANT DEVIATION FROM PLAN ASSUMPTIONS**: `plan.md`/`tasks.md` assumed (following the EP) a "5-level nil-check cascade" through pointer fields. The actual merged API uses **value types with `omitzero`** at every level except `ManagedTokenRequests.Audiences`. This means there is **no nil-pointer risk** when traversing `spec.DriverConfig.SecretsStore.SecretRotation.Custom` etc. — the cascade collapses to checking string-typed discriminator fields (`DriverType`, `SecretRotation.Type`, `TokenRequests.Type`) against their zero value (`""`) or expected enum constants. This makes the implementation meaningfully simpler than planned.

## Task T2_1 Payload (from tasks.md §4)

- **Objective:** Build the single shared component that reads `ClusterCSIDriver.Spec.DriverConfig.SecretsStore` with full nil-safety and resolves rotation/token-audience values with defaults matching today's hardcoded behavior.
- **Target file(s):** New file under `pkg/operator/` — suggested `pkg/operator/secretsstoreconfig.go`.
- **Non-goals / forbidden edits:** Do not implement CSIDriver mutation or DaemonSet mutation logic here — read/resolve layer only.
- **Implementation notes:** Handle: `driverConfig` (DriverType) not SecretsStore; `secretRotation`/`tokenRequests` omitted — each falling back to built-in defaults (rotation enabled, 2-minute interval [120s, matching `assets/node.yaml:46` `--rotation-poll-interval=2m`], no managed audiences) exactly matching today's hardcoded values (FR-010).
- **Acceptance criteria:** Traces to FR-001–FR-004, FR-006, FR-008, FR-010, FR-011 and Edge Cases. Full branch coverage deferred to T2_3; this task includes a smoke-test to satisfy mandatory verification.
- **Downstream handoff:** A stable Go function signature that T2_2 wires up and T3_1/T3_2/T4_1 call.

## Execution approach

Route: `OperatorController_Agent`-equivalent (`ControllerLogic_Agent` per the documented substitution) → implement directly in `pkg/operator/` following the existing package's conventions (error wrapping with `%w`, lowercase-verb error messages per `docs/error-handling-guidelines.md`).
