# Design Bundle — Task T5_1

**Change:** sscsi-254
**Task:** T5_1 — Upgrade-default-parity regression tests
**Assigned Agent:** Testing_Agent
**Phase:** Phase 5: Unit Test Completion Pass

## Task T5_1 Payload (from tasks.md §4)

- **Objective:** Explicitly table-test the "no `driverConfig`" and "`driverConfig` present but `SecretsStore` nil" paths across **both** consumers (`T3_1`/`T3_2` and `T4_1`) as first-class cases, closing `plan.md` §7's identified regression risk.
- **Target file(s):** Extends the `_test.go` files from `T2_3`/`T3_4`/`T4_3` — no new files anticipated, but a new file is acceptable if a cross-cutting regression suite is clearer.
- **Implementation notes:** Assert that with no `driverConfig` set, the resulting `CSIDriver` object and `DaemonSet` args are byte-for-byte/value-for-value identical to what today's static manifests produce (FR-010) — this is the single highest-value regression check per `plan.md` §7.
- **Acceptance criteria:** `make test-unit` passes; traces to FR-010 and `plan.md` §7 risk mitigation.

## Constitution/Plan context

> `plan.md` §7 highest risk: "Upgrade/migration: ... upgrade with no `driverConfig` set MUST be behaviorally identical to pre-feature defaults — this is a hard regression risk if the shared helper's default-resolution logic has any gap."

## Strongest possible regression test design

Rather than asserting against hand-constructed fixtures (as the incremental per-task tests already do), this task reads the **actual embedded production assets** (`assets.ReadFile("csidriver.yaml")`, `assets.ReadFile("node.yaml")`) via the real `assets` package — the same bytes the operator serves in production — and confirms the dynamic `AssetFunc`/hook produce functionally identical output when no `ClusterCSIDriver` config exists. This directly ties the regression check to the real static manifests, not just to test doubles, closing the loop from `plan.md`'s stated risk.

## Execution approach

New file `pkg/operator/upgrade_parity_test.go` (a new file is explicitly permitted per the task payload for this cross-cutting concern spanning both `csidriverasset.go` and `daemonsethook.go`): two tests, one per consumer, both reading real embedded assets via `github.com/openshift/secrets-store-csi-driver-operator/assets`.
