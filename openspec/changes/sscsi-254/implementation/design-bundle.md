# Design Bundle — Task T2_3

**Change:** sscsi-254
**Task:** T2_3 — Unit tests: read-path nil-safety branches
**Assigned Agent:** Testing_Agent
**Phase:** Phase 2: Shared Read Path

## Testing guidelines excerpts (binding)

> Use table-driven tests with named test cases as the default pattern... Run subtests with `t.Run(tc.name, ...)`... Use `t.Errorf` when subsequent assertions may still provide useful diagnostic information... No third-party assertion libraries.

## Task T2_3 Payload (from tasks.md §4)

- **Objective:** Cover every nil-safety branch in `T2_1`'s helper with table-driven tests.
- **Target file(s):** New `_test.go` file co-located with `T2_1`'s new file, following the `pkg/operator/starter_test.go` pattern.
- **Implementation notes:** Cases: `driverConfig` absent; `driverType != SecretsStore`; `secretsStore` nil; `secretRotation` nil; `tokenRequests` nil; fully-populated happy path.
- **Acceptance criteria:** `make test-unit` passes; every branch in `T2_1` has at least one covering case.

## Existing coverage (from T2_1's smoke tests, `pkg/operator/secretsstoreconfig_test.go`)

Already covered: nil spec; `DriverType` = AWS (not SecretsStore); `SecretsStore` fully zero-value (all sub-fields omitted); `secretRotation.Type: None`; `secretRotation.Type: Custom` (interval > 0 set); `tokenRequests.Type: Managed`; `tokenRequests.Type: Unmanaged`; managed-audiences nil/empty/populated.

## Gap identified for this task to close

Reviewing `ResolveSecretsStoreConfig`'s `switch secretsStore.SecretRotation.Type` branch: the `case opv1.SecretRotationCustom` path only sets `rotation.RotationPollIntervalSeconds` `if interval > 0`. **No existing test exercises `Type: Custom` with `RotationPollIntervalSeconds` omitted (zero value)** — this must fall back to the default 120s per FR-010, but it's untested. Also adding: an explicit `DriverType: ""` (true zero-value, "driverConfig absent" framing) case, and a combined "fully-populated happy path" case (custom rotation + managed multi-audience tokenRequests together) per this task's own payload language.

## Execution approach

Extend the existing `pkg/operator/secretsstoreconfig_test.go` (rather than creating a second, redundant test file for the same production file — cleaner and avoids Go test-file fragmentation for one small package) with a new test function covering the identified gap plus the explicitly-named cases from the task payload.
