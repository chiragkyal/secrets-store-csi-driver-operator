# E2E Rotation Test Status (T4_1)

**Change:** csi-secrets-store-rotation-and-wif  
**Task:** T4_1  
**Date:** 2026-07-10

## Cluster execution

| Check | Result |
|-------|--------|
| Live cluster run | **BLOCKED** |
| Blocker | `KUBECONFIG` points to unreachable API (`api.cluster-validation.gcp.devcluster.openshift.com` — DNS resolution failure) |
| Operator deployed | Not verified (requires cluster) |

### Reproduction steps (when cluster available)

```bash
export KUBECONFIG=/path/to/working/kubeconfig
# Ensure operator, driver, and e2e-provider are deployed (see hack/e2e.sh header)
make test-e2e
# or: ./hack/e2e.sh
```

Expected rotation functions exercised (in order):

1. `test_rotation_default_baseline` — default DS args + `requiresRepublish: true` (E2)
2. `test_rotation_toggle` — disable/enable + `requiresRepublish` false/true (E1, SC-001)
3. `test_rotation_custom_interval` — custom interval + fallback to 2m default (E3, SC-002)

## Local verification (this session)

| Check | Command | Result |
|-------|---------|--------|
| Bash syntax | `bash -n hack/e2e.sh` | PASS |
| Unit suite (operator logic) | `make test-unit` | PASS |

## T2_1 gap closure in script

| Gap | Addressed |
|-----|-----------|
| E1 — CSIDriver `requiresRepublish` in rotation e2e | `test_wait_csidriver_requires_republish` + assertions in `test_rotation_toggle` |
| E2 — Default-path e2e | `test_rotation_default_baseline` |
| E3 — Custom interval removal fallback | Step in `test_rotation_custom_interval` |

## SC mapping

| Criterion | E2E coverage |
|-----------|--------------|
| SC-001 | `test_rotation_toggle` (disable path + requiresRepublish false) |
| SC-002 | `test_rotation_custom_interval` (custom + fallback) |
