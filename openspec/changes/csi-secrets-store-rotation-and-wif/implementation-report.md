# Implementation Report — csi-secrets-store-rotation-and-wif

**Jira:** SSCSI-254  
**Change:** csi-secrets-store-rotation-and-wif  
**Branch:** `openspec-cursor-agent-sonnet5`  
**Posture:** IMPLEMENTED (DELTA) — verification and PR readiness  
**Report date:** 2026-07-10

## Executive Summary

The Secrets Store CSI Driver Operator on this branch implements reconciliation of `ClusterCSIDriver.spec.driverConfig.secretsStore` for **secret rotation** (DaemonSet args) and **workload identity federation** (`CSIDriver.spec.tokenRequests`). Core operator logic, unit tests, and extended E2E scripts are in place. Final verification gate (`make check`) passes. Live cluster E2E is blocked in the author environment. A clean upstream PR requires rebase, scope trimming, and commit of pending local changes (see `implementation/pr-draft-checklist.md`).

## Task Completion Summary

| Task | Title | Result | Key deliverable |
|------|-------|--------|-----------------|
| T1_1 | FR/SC traceability audit | PASS | `implementation/fr-sc-traceability.md` |
| T1_2 | `make check` baseline | PASS | Green baseline confirmed |
| T1_3 | Constitution compliance | PASS | `implementation/constitution-compliance-checklist.md` |
| T2_1 | Coverage audit | PASS | `implementation/coverage-audit-report.md` |
| T2_2 | Fix unit test gaps | PASS | Managed-audiences case in `csidriver_asset_test.go` |
| T3_1 | RBAC relevance | PASS | No RBAC change — `implementation/rbac-relevance-finding.md` |
| T4_1 | E2E rotation | PASS (offline) | `implementation/e2e-rotation-status.md` — cluster BLOCKED |
| T4_2 | E2E WIF | PASS (offline) | `implementation/e2e-wif-status.md` — cluster BLOCKED |
| T4_3 | Upgrade preservation | PASS (runbook) | `implementation/upgrade-preservation-runbook.md` |
| T5_1 | Upstream diff review | PASS | `implementation/upstream-diff-review.md` |
| T5_2 | Final make check + PR prep | PASS | This report + `implementation/pr-draft-checklist.md` |
| T6_1 | README update | PASS | `README.md` rotation/WIF docs + disable guidance |
| T6_2 | Sample ClusterCSIDriver YAML | PASS | `sscsi-sample-clustercsidriver-secretsstore.yaml` verified |

## Feature Implementation

### Operator code

| Component | File | Behavior |
|-----------|------|----------|
| Rotation hook | `pkg/operator/rotation.go` | Maps `secretRotation` to DaemonSet `--enable-secret-rotation` / `--rotation-poll-interval` |
| Dynamic CSIDriver | `pkg/operator/csidriver_asset.go` | Sets `requiresRepublish`, manages `tokenRequests` matrix |
| Wiring | `pkg/operator/starter.go` | Split CSIDriver controller, informer for ClusterCSIDriver, hooks registered |

### Tests

- Unit: `rotation_test.go`, `csidriver_asset_test.go` — table-driven coverage per FR/SC matrix
- E2E: `hack/e2e.sh` — rotation toggle/custom interval, WIF single/multi audience, unmanaged preservation, upgrade runbook comments

### Verification (T5_2)

```
make check → PASS (2026-07-10)
  gofmt: clean
  go vet: pass
  go test -race ./pkg/... ./cmd/...: pass
```

Local FIPS warning expected (dev toolchain without `GOEXPERIMENT=strictfipsruntime`); CI enforces FIPS.

## Known Gaps and Limitations

### 1. Downgrade behavior (Plan §8 #1)

**Status:** Open — documented, not implemented.

After `tokenRequests.type` is set to `Managed`, CEL immutability prevents reverting to `Unmanaged`. Cleanup path is `managed.audiences: []`. Downgrade of operator or CR to pre-feature state with Managed type is undefined — requires product/SME decision.

### 2. Live E2E (T4_1, T4_2)

**Status:** BLOCKED locally.

Kubeconfig API server DNS unreachable (`api.cluster-validation.gcp.devcluster.openshift.com`). E2E script structure and `bash -n` validated offline. Cluster verification required before merge confidence.

### 3. E2E WIF scope

**Status:** By design — propagation only.

Tests verify `CSIDriver.spec.tokenRequests` propagation and secret mount continuity. Full AWS STS / Azure AD federation round-trip is out of repo scope.

### 4. RBAC (T3_1, Plan §8 #3)

**Status:** No change for this feature.

`assets/rbac/secretproviderclasses_role.yaml` `serviceaccounts/token: create` is a legacy vestige unrelated to kubelet-driven tokenRequests. Optional follow-up PR to remove.

### 5. Upstream PR scope (T5_1)

**Status:** Action required before merge.

Branch contains ~197 non-vendor tooling files (`.cursor/`, `openspec/`, `dashboard/`) that must be excluded. Rebase onto upstream `main` and reconcile vendor (branch `library-go` behind upstream).

### 6. Uncommitted local changes

**Status:** Pending commit before push.

- `hack/e2e.sh` — T4 session extensions
- `pkg/operator/csidriver_asset_test.go` — T2_2 Managed-audiences case

## Constitution Compliance

Principles I, III, IV, VIII, X verified in T1_3. No custom CRD. Management state gating preserved. CA bundle hook retained alongside rotation hook. Vendor consumed via `go mod vendor` only.

## PR Readiness

Draft PR checklist: `implementation/pr-draft-checklist.md`

| Item | Status |
|------|--------|
| `make check` green | Done |
| Known gaps documented | Done |
| RBAC finding included | Done |
| Upstream diff scope defined | Done (T5_1) |
| Draft PR opened | **Pending** — requires rebase, commit, push (network/gh unavailable during T5_2) |

## Optional Follow-on Tasks

- **T6_1:** README rotation/WIF section — **complete**
- **T6_2:** Sample ClusterCSIDriver YAML — **complete** (`sscsi-sample-clustercsidriver-secretsstore.yaml`)

## References

- Task reports: `implementation/task-reports/T1_1.md` … `T5_2.md`
- Upstream diff: `implementation/upstream-diff-review.md`
- Upgrade runbook: `implementation/upgrade-preservation-runbook.md`
