# Draft PR Checklist — SSCSI-254

**Jira:** SSCSI-254  
**Upstream target:** `openshift/secrets-store-csi-driver-operator` → `main`  
**Fork head:** `https://github.com/chiragkyal/secrets-store-csi-driver-operator`  
**Feature branch:** `openspec-cursor-agent-sonnet5`

## Pre-push (required)

- [ ] Rebase onto latest `upstream/main` (`git fetch upstream && git rebase upstream/main`)
- [ ] Resolve vendor conflicts — align `go.mod` to upstream pins; run `go mod tidy && go mod vendor`
- [ ] Squash or curate commits — **exclude** `.cursor/`, `openspec/`, `dashboard/`, `eval-generation/` from PR scope
- [ ] Commit pending local changes:
  - [ ] `hack/e2e.sh` (rotation/WIF/upgrade runbook from T4_1–T4_3)
  - [ ] `pkg/operator/csidriver_asset_test.go` (Managed-audiences case from T2_2)
- [ ] Run `make check` on rebased branch (must exit 0)
- [ ] Push feature branch to fork: `git push -u origin openspec-cursor-agent-sonnet5`

## PR creation

- [ ] Open **draft** PR: fork `openspec-cursor-agent-sonnet5` → upstream `main`
- [ ] Title: `SSCSI-254: secrets-store rotation and WIF tokenRequests`
- [ ] Include in PR body:
  - [ ] Summary of rotation + WIF operator changes
  - [ ] Link to Jira SSCSI-254
  - [ ] Known limitations (see `implementation-report.md` §Known Gaps)
  - [ ] E2E status: scripts extended; live cluster blocked locally
  - [ ] RBAC: no change (T3_1 finding)

## PR scope (include only)

| Path | Reason |
|------|--------|
| `pkg/operator/rotation.go` | Rotation DaemonSet hook |
| `pkg/operator/rotation_test.go` | Unit tests |
| `pkg/operator/csidriver_asset.go` | Dynamic CSIDriver asset |
| `pkg/operator/csidriver_asset_test.go` | Unit tests |
| `pkg/operator/starter.go` | Controller wiring |
| `hack/e2e.sh` | E2E rotation/WIF scenarios |
| `go.mod`, `go.sum`, `vendor/` | Dependency bump (post-rebase) |
| `README.md` | Optional — include if T6_1 approved |
| `config/manifests/stable/sscsi-sample-clustercsidriver-secretsstore.yaml` | Optional — include if T6_2 approved |

## PR scope (exclude)

- `.cursor/**`, `openspec/**`, `dashboard/**`, `eval-generation/**`
- OpenSpec implementation artifacts under `openspec/changes/`

## Post-open verification

- [ ] CI Prow: `make verify` + `make test-unit` green
- [ ] FIPS build in CI (local non-FIPS warning acceptable for dev only)
- [ ] Request review from secrets-store CSI maintainers

## Draft PR body template

```markdown
## Summary

- Reconcile `ClusterCSIDriver.spec.driverConfig.secretsStore.secretRotation` to DaemonSet rotation args
- Reconcile WIF `tokenRequests` to `CSIDriver.spec.tokenRequests` via dynamic CSIDriver controller
- Split static vs dynamic CSIDriver reconciliation; preserve upgrade-safe defaults

## Jira

SSCSI-254

## Test plan

- [x] `make check` (unit + verify)
- [ ] `make test-e2e` on OpenShift 5.0 cluster (rotation + WIF propagation)
- [ ] Manual upgrade-preservation runbook (SC-005)

## Known limitations

- Downgrade from `tokenRequests.type: Managed` undefined — document only
- E2E WIF verifies CSIDriver propagation + mount, not full cloud IAM federation
- Live E2E not run in PR author environment (cluster unreachable)
- RBAC `serviceaccounts/token: create` unchanged (legacy vestige; optional follow-up)
```
