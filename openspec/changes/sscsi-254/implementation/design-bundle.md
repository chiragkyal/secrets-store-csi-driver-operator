# Design Bundle — Task T2_1
**Change:** sscsi-254 | **Task:** T2_1 | **Agent:** OperatorController_Agent

---

## Task Payload

**Objective:** Add `getRotationConfig` to `pkg/operator/starter.go` — a private function that reads `ClusterCSIDriver.spec.driverConfig.secretsStore.secretRotation` and returns `(requiresRepublish bool, enableRotation bool, pollInterval string)` with built-in defaults at every nil level.

**Target file:** `pkg/operator/starter.go`

**Non-goals:** Do not modify `RunOperator`, `replaceNamespaceFunc`, `getOperatorSyncState`, or `extractOperatorSpec`. Do not create new files.

**Implementation notes:**
- Nil-handling chain (return defaults `true, true, "2m0s"` for all nil paths):
  - `DriverType != SecretsStore` (or empty/absent driverConfig) → built-in defaults
  - `SecretsStore` is zero-value → built-in defaults  
  - `SecretRotation` is zero-value (omitzero) → built-in defaults
  - `SecretRotation.Type == None` → `false, false, "2m0s"`
  - `SecretRotation.Type == Custom`, `RotationPollIntervalSeconds == 0` → `true, true, "2m0s"`
  - `SecretRotation.Type == Custom`, `RotationPollIntervalSeconds > 0` → `true, true, formatted duration`
- `requiresRepublish`: `false` ONLY when `type == None`; `true` for all other states
- Duration formatting: convert seconds to Go duration string e.g. 300 → `"5m0s"`, 3600 → `"1h0m0s"`
- Use `operatorClient.GetOperatorState()` to read spec (already available in package scope)
- Actual API field: `RotationPollIntervalSeconds int32` (not MinimumRefreshAge — PR #2906 pending)
- Actual types: `opv1.SecretsStoreDriverType`, `opv1.SecretRotationNone`, `opv1.SecretRotationCustom`

**Acceptance criteria:**
- Function signature: `func getRotationConfig(operatorClient v1helpers.OperatorClientWithFinalizers) (requiresRepublish bool, enableRotation bool, pollInterval string)`
- `go build ./... && go vet ./...` passes
- All nil-path permutations return correct defaults (unit tests in T5_1)

---

## Constitution Guardrails
- All logic in `pkg/operator/starter.go` — no new files
- No controller-runtime, no custom types
- Library-go fakes in tests; standard `t.Fatalf` assertions

---

## Relevant API Types (from vendor)
```go
// opv1.CSIDriverType
SecretsStoreDriverType CSIDriverType = "SecretsStore"

// opv1.SecretRotationType
SecretRotationNone    SecretRotationType = "None"
SecretRotationCustom  SecretRotationType = "Custom"

// opv1.SecretsStoreSecretRotation
type SecretsStoreSecretRotation struct {
    Type   SecretRotationType   `json:"type,omitempty"`
    Custom CustomSecretRotation `json:"custom,omitzero"`
}

// opv1.CustomSecretRotation
type CustomSecretRotation struct {
    RotationPollIntervalSeconds int32 `json:"rotationPollIntervalSeconds,omitempty"`
}

// opv1.CSIDriverConfigSpec
type CSIDriverConfigSpec struct {
    DriverType   CSIDriverType                  `json:"driverType,omitempty"`
    SecretsStore SecretsStoreCSIDriverConfigSpec `json:"secretsStore,omitzero"`
    // ... other types ...
}

// opv1.ClusterCSIDriverSpec
type ClusterCSIDriverSpec struct {
    OperatorSpec `json:",inline"`
    DriverConfig CSIDriverConfigSpec `json:"driverConfig,omitzero"`
}
```
