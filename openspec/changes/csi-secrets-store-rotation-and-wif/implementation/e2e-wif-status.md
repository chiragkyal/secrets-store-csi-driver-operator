# E2E WIF Test Status (T4_2)

**Change:** csi-secrets-store-rotation-and-wif  
**Task:** T4_2  
**Date:** 2026-07-10

## Cluster execution

| Check | Result |
|-------|--------|
| Live cluster run | **BLOCKED** (same kubeconfig DNS failure as T4_1) |
| Reproduction | `export KUBECONFIG=/path/to/working/kubeconfig && make test-e2e` |

## WIF functions verified locally (syntax)

| Function | SC / FR | Purpose |
|----------|---------|---------|
| `test_wif_unmanaged_preservation` | FR-005, E4 | Unmanaged type preserves manual CSIDriver audiences |
| `test_wif_single_audience` | US2, SC-003 | Single managed audience + mount continuity |
| `test_wif_multi_audience` | US4, SC-004 | Multi audience + expirationSeconds + mount |

## Local verification

| Check | Command | Result |
|-------|---------|--------|
| Bash syntax | `bash -n hack/e2e.sh` | PASS |
| Unit suite | `make test-unit` | PASS |

## Scope note

Propagation-only verification per `repo-assessment.md` §8.4 — no AWS STS / Azure AD round-trip.
