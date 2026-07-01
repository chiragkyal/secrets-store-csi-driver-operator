# Implementation Checklist: SSCSI-254

## Operator Code
- [x] `go.mod` bumped — all three openshift packages at June/July 2026 versions
- [x] `SecretsStore` API types available in vendor (`types_csi_cluster_driver.go`)
- [x] `getRotationConfig` — nil-safe, all 7 test cases pass
- [x] `getTokenRequests` — Managed + Unmanaged paths, live CSIDriver read, all 7 test cases pass
- [x] `enrichedCSIDriverAssetFunc` — enriches `csidriver.yaml` at reconcile, delegates all other assets unchanged
- [x] `rotationArgsDaemonSetHook` — replaces `--enable-secret-rotation` and `--rotation-poll-interval` on `csi-driver` container
- [x] `RunOperator` wired — `enrichedCSIDriverAssetFunc` replaces static `replaceNamespaceFunc`; `dynamicInformers` CCD informer wired; both hooks registered

## Tests
- [x] `make check` passes (gofmt + vet + test-race)
- [x] 26/26 unit tests pass across 5 suites
- [ ] E2E tests on live cluster (`hack/e2e.sh`) — 9 scenarios per T7_1.md

## OLM
- [x] CSV description updated to document new configuration surface
- [ ] Formal `specDescriptors` — N/A (`ClusterCSIDriver` is platform CRD)

## Before PR Merge
- [ ] E2E tests run and pass on live OpenShift cluster
- [ ] openshift/api version coordinated with release stream
- [ ] PR description links to EP ([#2012](https://github.com/openshift/enhancements/pull/2012)) and SSCSI-254
- [ ] Jira ticket SSCSI-254 updated to Implementation Review state
