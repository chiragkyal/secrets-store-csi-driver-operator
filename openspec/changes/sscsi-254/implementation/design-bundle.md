# Design Bundle — Task T4_3

**Change:** sscsi-254
**Task:** T4_3 — Unit tests: hook arg replacement + error path
**Assigned Agent:** Testing_Agent
**Phase:** Phase 4: DaemonSet Rotation Hook

## Task T4_3 Payload (from tasks.md §4)

- **Objective:** Table-driven coverage of `T4_1`'s arg-replacement logic and its "csi-driver container not found" error path.
- **Target file(s):** New `_test.go` co-located with `T4_1`'s file.
- **Implementation notes:** Cases: default (no config) preserves `true`/`2m`; custom interval sets `--rotation-poll-interval=<value>`; `secretRotation.type: "None"` sets `--enable-secret-rotation=false`; container-not-found returns a non-nil error.
- **Acceptance criteria:** `make test-unit` passes; traces to `specs.md` FR-001/FR-002 and Edge Cases.

## Existing coverage (from T4_1's mandatory verification tests)

`T4_1` already wrote and this task inherits: `Defaults`, `CustomInterval`, `RotationDisabled`, `ContainerNotFound`, `ListerError` — covering every case explicitly listed in this task's own payload.

## Gap identified for this task to close

Reviewing `setArgPrefix`: the existing test fixture (`buildTestDaemonSet`) always pre-populates both `--enable-secret-rotation=` and `--rotation-poll-interval=` args, so **the "append when the arg is missing entirely" branch of `setArgPrefix` is never exercised**. Also, no existing test explicitly asserts that unrelated args (`--endpoint=`, `--provider-health-check=`) and the arg count are left untouched by the hook — a regression-safety gap for a find/replace function operating on a shared slice.

## Execution approach

Extend `pkg/operator/daemonsethook_test.go` (from `T4_1`) with two new test functions: one exercising the append-when-missing path with a DaemonSet fixture that omits the rotation args entirely, and one asserting unrelated args are preserved byte-for-byte after the hook runs.
