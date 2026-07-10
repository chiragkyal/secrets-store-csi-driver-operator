# Upstream Diff Review — T5_1

**Change:** csi-secrets-store-rotation-and-wif (SSCSI-254)  
**Branch:** `openspec-cursor-agent-sonnet5` @ `0b6b5b3a`  
**Upstream:** `openshift/secrets-store-csi-driver-operator` `main` @ `cf9d0f42`  
**Merge base:** `36a83411`  
**Review date:** 2026-07-10

## PR Targets

| Field | Value |
|-------|-------|
| Upstream repo | `https://github.com/openshift/secrets-store-csi-driver-operator` |
| Upstream base branch | `main` |
| Fork (draft PR head) | `https://github.com/chiragkyal/secrets-store-csi-driver-operator` |
| Feature branch | `openspec-cursor-agent-sonnet5` |
| Working mode | `use_working_folder_as_repo: true` (local checkout) |

## Executive Summary

The **operator feature** (secret rotation + WIF tokenRequests) is **not on upstream `main`**. Upstream already vendors `openshift/api` types for `driverConfig.secretsStore` and `SecretsStoreDriverType`, but has **no** `rotation.go`, `csidriver_asset.go`, or related wiring in `starter.go`.

The current branch contains **~34 commits** ahead of upstream, but only **~13 non-vendor files** are merge-relevant. The remaining **~197 non-vendor files** are OpenSpec workflow artifacts, Cursor tooling, dashboard, and eval scaffolding — **must be excluded** from the upstream PR.

**Recommendation for T5_2:** Rebase onto latest `upstream/main`, cherry-pick or squash to a clean commit series containing only feature + test + doc files listed below, reconcile `go.mod`/`vendor/` against upstream pins, then open draft PR.

## Diff Statistics

| Scope | Files | Insertions | Deletions |
|-------|-------|------------|-----------|
| Full branch vs upstream | 820 | +82,219 | −80,028 |
| `vendor/` only | 612 | +53,011 | −79,967 |
| Feature core (`pkg/operator`, `hack/e2e.sh`, `go.mod`, `go.sum`) | 8 | +1,586 | −61 |
| Merge-relevant (excl. vendor, `.cursor`, `openspec`, `dashboard`, `eval-generation`) | 13 | — | — |

## Merge-Relevant Files (Include in PR)

| File | Role | Notes |
|------|------|-------|
| `pkg/operator/rotation.go` | **New** | DaemonSet hook: rotation args from `driverConfig.secretsStore.secretRotation` |
| `pkg/operator/rotation_test.go` | **New** | Unit tests for rotation config matrix |
| `pkg/operator/csidriver_asset.go` | **New** | Dynamic CSIDriver asset: `requiresRepublish`, `tokenRequests` |
| `pkg/operator/csidriver_asset_test.go` | **New** | Unit tests including Managed-audiences case (T2_2) |
| `pkg/operator/starter.go` | **Modified** | Split CSIDriver controller, rotation hook, clusterCSIDriver informer |
| `hack/e2e.sh` | **Modified** | +340 lines: rotation, WIF, upgrade-preservation runbook comments |
| `go.mod` / `go.sum` | **Modified** | Dependency bumps — see §Dependency Pin Validation |
| `config/manifests/stable/sscsi-sample-clustercsidriver-secretsstore.yaml` | **New** | Optional sample (T6_2 scope; include if docs task approved) |
| `README.md` | **Modified** | +96 lines rotation/WIF config docs (T6_1 scope; include if docs task approved) |

### `starter.go` Key Changes vs Upstream

- Removes `csidriver.yaml` from static resources controller.
- Adds `SecretsStoreDynamicCSIDriverController` with `NewDynamicCSIDriverAssetFunc`.
- Registers `WithSecretRotationDaemonSetHook` alongside existing `WithCABundleDaemonSetHook`.
- Passes `clusterCSIDriverInformer` into `WithCSIDriverNodeService` optional informers.

### Files Unchanged (Expected)

- `assets/node.yaml`, `assets/csidriver.yaml` template, RBAC assets — no diff vs upstream.
- `assets/rbac/secretproviderclasses_role.yaml` — unchanged (T3_1: no RBAC change needed).

## Exclude from Upstream PR (Unintended Scope)

| Category | File count | Action |
|----------|------------|--------|
| `.cursor/` (commands, skills, plans, e2e-generator) | 29 | **Drop** — local AI workflow tooling |
| `openspec/` (changes, schema, telemetry, inputs) | 88 | **Drop** — spec workflow artifacts |
| `dashboard/` (Python telemetry UI) | 78 | **Drop** — not part of operator |
| `eval-generation/` | 2 | **Drop** — eval scaffolding |
| `.gitignore` | 1 | **Review** — may include openspec ignores; reconcile with upstream |

These paths account for the bulk of the 820-file diff. They must not appear in the PR to `openshift/secrets-store-csi-driver-operator`.

## Dependency Pin Validation

| Dependency | Branch pin | Upstream `main` pin | Assessment |
|------------|------------|---------------------|------------|
| `github.com/openshift/api` | `580f1c1ba691` (2026-07-09) | `b1cc68a860b3` (2026-07-08) | Branch is **1 day newer**; both include `SecretsStoreDriverType` and CEL immutability rules. Rebase should take **upstream pin** unless newer commit is required for a specific fix. |
| `github.com/openshift/client-go` | `24d059aea27a` (2026-07-03) | `24d059aea27a` | **Identical** |
| `github.com/openshift/library-go` | `5d9eb6295ff6` (2026-03-03) | `aa59c3fbacc1` (2026-07-08) | Branch is **behind upstream** — rebase will likely require vendor refresh to upstream library-go. |

**Constitution Principle X:** Vendor changes are expected for `openshift/api` bump; after rebase, run `go mod tidy && go mod vendor` against upstream baseline rather than carrying the full 612-file vendor churn from the feature branch history.

## Commit Hygiene

The branch has **34 commits** vs upstream, including:

- OpenSpec workflow commits (`opsx-new`, validation, spec generation, planning, task generation)
- Per-task implementation commits (`T1_1` … `T6_2`, `T5_3`, `T4_4`, etc.)
- Archive commit `0b6b5b3a`

**Not suitable for upstream review as-is.** Recommended PR structure:

1. **Single squashed commit** (preferred for OpenShift operators), or
2. **2–3 logical commits:** (a) operator logic + tests, (b) e2e extensions, (c) optional docs/sample

Commit messages should follow upstream convention (Jira reference `SSCSI-254`, imperative subject).

## Local Uncommitted Changes (Not in Branch Diff)

At review time, the working tree had uncommitted changes **not yet** in `0b6b5b3a`:

| Path | Status |
|------|--------|
| `hack/e2e.sh` | Modified (T4_1/T4_2/T4_3 session work) |
| `pkg/operator/csidriver_asset_test.go` | Modified (T2_2 Managed-audiences case) |
| `openspec/changes/csi-secrets-store-rotation-and-wif/` | Untracked (implementation artifacts) |

T5_2 must ensure e2e and test fixes are committed before PR push.

## Plan §8 #2 Resolution

| Question | Answer |
|----------|--------|
| Is feature already merged upstream? | **No** — operator reconciliation logic is branch-only. API types are already in upstream vendor. |
| What remains for upstream merge? | Operator code (4 new + 1 modified Go files), e2e tests, dependency/vendor sync, optional README + sample YAML. |
| Surprise unrelated changes? | **Yes** — ~197 non-vendor tooling files must be stripped before PR. |
| PR target branch | `main` on `openshift/secrets-store-csi-driver-operator` |

## Known Gaps (Handoff to T5_2)

- **Downgrade behavior** after `tokenRequests.type: Managed` — documented open question (Plan §8 #1); not a PR blocker.
- **Live E2E** — cluster blocked; script structure verified offline (T4_1/T4_2).
- **RBAC** — no change; optional follow-up to remove vestigial `serviceaccounts/token: create` (T3_1).
- **Vendor rebase** — expect conflicts; align to upstream `library-go` after rebase.
- **Commit/squash** — required before draft PR.

## Acceptance Criteria Checklist

- [x] Written diff summary (this document)
- [x] PR target branch identified (`openshift/secrets-store-csi-driver-operator` → `main`)
- [x] Unintended files flagged (`.cursor`, `openspec`, `dashboard`, `eval-generation`)
- [x] Feature scope confirmed vs upstream (operator logic not merged; API types present)
- [x] `go.mod`/`vendor/` pin validation documented
