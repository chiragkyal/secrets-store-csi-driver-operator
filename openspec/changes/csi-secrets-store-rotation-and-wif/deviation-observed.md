# Deviations Observed — csi-secrets-store-rotation-and-wif

**Change:** SSCSI-254  
**Date:** 2026-07-10

## Summary

All 13 tasks completed. The following deviations from ideal workflow outcomes were observed and accepted.

| # | Deviation | Tasks | Impact | Resolution |
|---|-----------|-------|--------|------------|
| 1 | Live cluster E2E blocked (kubeconfig DNS failure) | T4_1, T4_2, T4_3 | No live rotation/WIF/upgrade verification | Offline script validation + manual runbook documented |
| 2 | Draft PR not opened in-session | T5_2 | PR creation deferred | `implementation/pr-draft-checklist.md` provides handoff |
| 3 | Branch contains non-operator tooling (~197 files) | T5_1 | Upstream PR scope bloated if pushed as-is | Rebase + squash; exclude `.cursor/`, `openspec/`, `dashboard/` |
| 4 | Verification-only doc tasks (pre-existing content) | T6_1, T6_2 | Minor additive edits only | README disable subsection added; sample YAML verified unchanged |
| 5 | Downgrade after `Managed` tokenRequests undefined | T5_2 | Known product gap | Documented in `implementation-report.md` §Known Gaps |
| 6 | RBAC `serviceaccounts/token: create` unchanged | T3_1 | Legacy vestige remains | Optional follow-up PR |
| 7 | Working-folder mode active | T5_2 | No automated fork push/PR from orchestrator | User follows `pr-draft-checklist.md` manually |
| 8 | Uncommitted local changes at completion | T2_2, T4_x, T6_1 | `hack/e2e.sh`, `csidriver_asset_test.go`, `README.md`, openspec artifacts | Commit before upstream PR push |

## Blocking Issues

None — all tasks marked complete with documented workarounds.
