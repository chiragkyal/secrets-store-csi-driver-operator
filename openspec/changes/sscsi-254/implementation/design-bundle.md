# Design Bundle — Task T7_1

**Change:** sscsi-254
**Task:** T7_1 — Discovery: enumerate `hack/e2e.sh` structure
**Assigned Agent:** Testing_Agent
**Phase:** Phase 7: E2E Scenarios

## Task T7_1 Payload (from tasks.md §4)

- **Objective:** Read `hack/e2e.sh` in full and enumerate the actual e2e test file organization, closing the `repo-assessment.md` §11.1 UNVERIFIED item before scoping `T7_2`–`T7_4`.
- **Target file(s):** `hack/e2e.sh` (read-only for this task).
- **Acceptance criteria:** A short discovery note (where new e2e scenarios will be added) is available to unblock `T7_2`–`T7_4`.

## DISCOVERY FINDINGS (closes `repo-assessment.md` §11.1 UNVERIFIED item)

**Confirmed: this repo has NO Go/Ginkgo e2e test package.** `hack/e2e.sh` is the **only** e2e test artifact — a single monolithic bash script (183 lines) that drives tests entirely via the `oc` CLI. No `test/e2e/` directory exists anywhere in the source tree (confirmed via full-repo search).

### Structure of `hack/e2e.sh`

- **Config (env vars, lines 9-23)**: `KUBECONFIG`, `E2E_PROVIDER_NAMESPACE`, `E2E_PROVIDER_APP_LABEL`, `PROVISIONER_NAME` (`secrets-store.csi.k8s.io`), a randomly-suffixed `E2E_TEST_NAMESPACE`, `E2E_TEST_SERVICEACCOUNT`, `E2E_TEST_PROVIDER` (`e2e-provider`), `E2E_TEST_IMAGE`, `E2E_TEST_POD_TIMEOUT` (120s).
- **Bash functions** (each returns 0/1, following a "check-and-return" convention, no framework):
  - `test_prechecks()` — confirms the `CSIDriver` object and e2e-provider pod exist/are ready.
  - `test_setup()` — creates the test namespace, grants `privileged` SCC, applies pod-security labels, creates a `SecretProviderClass` (`secrets-store.csi.x-k8s.io/v1`).
  - `test_teardown()` — deletes the test namespace.
  - `test_pods_dump()` — diagnostic dump (`oc describe pods`, `oc get pods -o yaml`) on failure.
  - `test_pod_create/wait/log_check/delete()` — granular pod lifecycle helpers, parameterized by pod name.
  - `test_pod_with_secret()` — the **only existing test scenario today**: creates a pod mounting a CSI volume via `SecretProviderClass`, waits for Ready, checks the log contains the expected secret value, deletes the pod.
- **Execution (lines 154-182)**: strictly sequential — `test_prechecks` → `test_setup` → `test_pod_with_secret` → `test_teardown`, each gated by an explicit `if [ $? -ne 0 ]` check that dumps diagnostics and exits 1 on failure.

### Implication for `T7_2`/`T7_3`/`T7_4`

New rotation/WIF/upgrade scenarios must be added as **new bash functions following this exact `test_xxx() { ...; return $?; }` convention**, wired into the sequential execution block at the bottom (each new scenario gated the same way as `test_pod_with_secret`) — **not** as Go test files. This repo's `.cursor/e2e-test-generator` skill fixture (`fixtures/e2e-sample-library-go_test.sh.example`) confirms this is the expected style for library-go-based operators like this one (setup/log/fail helpers, namespace-per-run, `oc` CLI driven).

### Manual verification commands to incorporate (from `repo-assessment.md` §12)

- `oc get csidriver secrets-store.csi.k8s.io -o yaml` — verify `spec.requiresRepublish`/`spec.tokenRequests`.
- `oc get ds -n openshift-cluster-csi-drivers secrets-store-csi-driver-node -o jsonpath='{.spec.template.spec.containers[?(@.name=="csi-driver")].args}'` — verify rotation args.

## Execution approach

This task produces no code changes — it is a discovery/documentation step. The findings above (especially "bash functions, not Go tests" and the exact execution-block pattern) directly scope `T7_2`'s, `T7_3`'s, and `T7_4`'s implementation approach.
