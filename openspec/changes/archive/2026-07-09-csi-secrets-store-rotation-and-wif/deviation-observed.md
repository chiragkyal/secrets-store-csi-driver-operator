# Deviations Observed

**Change**: csi-secrets-store-rotation-and-wif
**Jira**: SSCSI-254

---

## Phase 1: Vendor API Extension

- **Task T1_2**: Bumped `github.com/openshift/client-go` (to `v0.0.0-20260703082747-24d059aea27a`) in addition to the planned `openshift/api` bump. `go build` surfaced a pre-existing version-skew incompatibility between the newly-bumped `openshift/api` (July 2026) and the previously-pinned `client-go` (March 2026), which vendored `config/v1alpha1` listers/apply-configurations referencing types that no longer exist in the same form upstream — entirely unrelated to this feature's `SecretsStore` types. Resolved by bumping `client-go` to its own current `master` HEAD, restoring compatibility. Still within the task's Target files (`go.mod`/`go.sum`/`vendor/`); a deviation from the task's narrower initial hypothesis about *why* client-go might need touching, not from its intent.

---

## Phase 2: Secret Rotation DaemonSet Hook

- **Task T2_4**: A test-authoring bug was self-caught during development and corrected before presenting for approval (not a residual defect): `TestWithSecretRotationDaemonSetHookMissingContainer` initially passed a `nil` `ClusterCSIDriver`, which triggers the hook's early `NotFound` return path before ever reaching the container-lookup code being tested. Fixed by constructing a present-but-unconfigured `ClusterCSIDriver` so the test genuinely exercises the "container not found" error path.

---

## Phase 4: Wire-up & Regression Guard

- **Task T4_3** (real production defect, found and fixed): While adding the default-path regression test (byte-for-byte comparison against the pre-feature baseline documented in `repo-assessment.md` §3.2), discovered that `time.Duration.String()` renders exactly-2-minutes as `"2m0s"`, not the historical literal `"2m"` hardcoded in `assets/node.yaml`. Left unfixed, this would have caused an **unintended DaemonSet rollout for every existing cluster** the first time this feature's code ran against a `ClusterCSIDriver` with no `driverConfig.secretsStore` configured — a real regression against FR-003/FR-012 and `specs.md` SC-005 ("zero behavior change for unconfigured clusters"). Fixed via a new `formatRotationInterval` helper in `pkg/operator/rotation.go`, rendering whole-minute durations as `"Nm"`. This task's own Non-goals explicitly permit production-code changes when the regression test reveals an actual defect (rather than silently adjusting the test to match wrong behavior), which is exactly what happened here.

---

## Phase 5: E2E Coverage

- **Tasks T5_1, T5_2**: Both E2E tasks verify the operator's own reconciliation surface (`ClusterCSIDriver` → DaemonSet args / `CSIDriver.spec.tokenRequests`) rather than the driver's actual runtime rotation/WIF behavior end-to-end, because this repo's `e2e-provider` test fixture has no existing mechanism to mutate the secret value it returns mid-test (confirmed via investigation — no such scaffolding exists anywhere in this repo today). This matches the "Manual/Cluster" verification row `plan.md` §6 documents for this feature and is the correct scope for what a single-script E2E run can verify without new external test infrastructure, which was out of scope for both tasks' payloads.

- **Task T5_2** (additional, related): Identified that once `CSIDriver.spec.tokenRequests.type` is set to `Managed` (FR-006), it can never be reverted to `Unmanaged` — only the audience list can be emptied (FR-007). A plain `driverConfig` restore (as used for rotation cleanup in T5_1) cannot fully undo a WIF test's side effects on its own. Added a dedicated `test_wif_clear_audiences` cleanup step (Managed + empty audience list) called before every driverConfig restore in both WIF tests, so the test suite ends in the correct, spec-compliant clean state.

- **Task T5_3**: Acceptance criterion narrowed from "an automated `hack/e2e.sh` scenario" to "an explicitly-documented manual verification runbook step" — both explicitly listed as acceptable outcomes in this task's own Acceptance criteria, and the narrowing itself was pre-authorized by the task's Implementation notes for exactly this circumstance (no upgrade-testing mechanism found anywhere in this repo's CI setup, confirmed via investigation). The underlying preservation guarantee this runbook exists to confirm on a real cluster remains fully covered at the unit level by T3_3 (`tokenRequests` preservation matrix) and T4_3 (default-path baseline regression).

---

## Cross-cutting note

None of the above deviations required re-scoping any Functional Requirement or Success Criterion in `specs.md`. All were either (a) self-corrected before approval, (b) explicitly pre-authorized by the relevant task's own Non-goals/Implementation notes for exactly the circumstance encountered, or (c) a genuine defect the task's regression-testing purpose existed to catch (T4_3) — which is the deviation type most worth flagging to reviewers.
