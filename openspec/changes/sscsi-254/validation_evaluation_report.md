# Evaluation Report: validation

**Change:** sscsi-254  
**Artifact:** validation.json  
**EP:** [SSCSI-254 — Configurable Secret Rotation and WIF](https://github.com/openshift/enhancements/pull/2012)  
**Evaluated at:** 2026-06-30T13:05:00Z  
**Gate type:** rubric_only (no stage eval YAML)

---

## Eval Summary

| Metric | Value |
|--------|-------|
| Overall score | **88%** |
| Overall status | **PASS** |
| Completeness score | 86% |
| Quality score | 90% |
| Refinement applied | No |

---

## Rubric Assessment

### A) Completeness (86/100)

| Pillar | Present | Notes |
|--------|---------|-------|
| Context & Motivation | ✓ | Two clear problems (no rotation control, no WIF). Historical context (GA 4.17, upstream v1.6.0). |
| User Personas / Actors | ✓ | 5 explicit user stories: cluster admin (×3), platform engineer, multi-cloud operator. |
| Acceptance Criteria & Edge Cases | ✓ | Exhaustive test plan: unit, API integration (CEL immutability), E2E (rotation, WIF, upgrade scenarios). |
| Scope Boundaries & Non-Goals | ✓ | 3 explicit non-goals: no cloud auto-detection, no upstream changes, no provider config. |
| Impacted Repositories | ✗ | openshift/api referenced via PR #2846 link only; no formal "Impacted Repos" list. |

**CSI ecosystem pillars (from agents.md Validation Stage Hints):**

| Pillar | Addressed | Notes |
|--------|-----------|-------|
| CSI driver registration | ✓ | CSIDriver.spec.requiresRepublish + tokenRequests management — central to design |
| Secret rotation | ✓ | Full coverage: minimumRefreshAge, enable/disable, requiresRepublish mapping |
| Provider integration | ✓ | tokenRequests enable WIF with AWS STS, Azure AD, GCP IAM |
| Management state / upgrade | ✓ | Detailed Unmanaged-default semantics on upgrade; nil-handling at every level |
| Upgrade / downgrade skew | ✓ | Upgrade table with exact before/after values; 6 upgrade E2E scenarios |
| RBAC blast radius | ✗ | SA token projection via tokenRequests not analyzed vs. existing RBAC |
| OLM / CSV upgrade | ✗ | New spec fields not reflected in CSV alm-status-descriptors discussion |
| Network policy | N/A | No new ports introduced |
| CA bundle | N/A | No new containers |

### B) Quality (90/100)

| Dimension | Score | Notes |
|-----------|-------|-------|
| Ambiguity | −5 | `minimumRefreshAge` "subject to change" vs. "currently 120 seconds" in implementation notes — inconsistent for API readers |
| Testability | −3 | Low-interval risk mitigation is docs-only; no status condition proposed for misconfiguration |
| Sizing | ✓ | Two tightly coupled features (rotation + WIF) sharing one API extension — appropriate bundling |
| Consistency | ✓ | Mermaid diagrams, example YAMLs, upgrade table, and prose are internally consistent |

---

## Gap Analysis

| Gap | Source | Severity |
|-----|--------|----------|
| openshift/api not listed as impacted repo | EP §Impacted Repositories absent | MINOR |
| RBAC: SA token projection not analyzed | agents.md — RBAC blast radius pillar | MINOR |
| OLM/CSV: alm-status-descriptors not mentioned | agents.md — OLM upgrade pillar | MINOR |
| minimumRefreshAge default value inconsistent in API doc vs. implementation notes | EP §API + Implementation notes | MINOR |

---

## Quality Assessment

- **Completeness:** Strong — all core pillars present except formal repo listing. EP goes well beyond minimum with upgrade nil-handling at every API level and 3 test categories.
- **Consistency:** Excellent — the Mermaid flowcharts, example YAMLs, CEL rules, test cases, and upgrade table all agree.
- **Grounding:** All claims are repo-evidenced (references to library-go ApplyCSIDriver, CSIControllerSet hooks, upstream v1.6.0 release notes).
- **Testability:** Very high — unit test cases are scenario-by-scenario, not just "add unit tests". E2E upgrade scenarios cover the exact Unmanaged preservation path.

---

## Recommendations

1. **Before tasks.md**: Add `openshift/api` as an explicit dependency task (the API PR #2846 may need to land first or in parallel; coordinate versioning).
2. **Add task for OLM/CSV update**: Verify whether new `secretsStore` spec fields need entries in `alm-status-descriptors` in `config/manifests/stable/*.clusterserviceversion.yaml`.
3. **RBAC confirmation task**: Verify existing `secretproviderclasses_role.yaml` `serviceaccounts/token` verbs cover the new SA token projection path — document the conclusion.
4. **Consider status condition**: For the "minimumRefreshAge below kubelet syncFrequency" case, evaluate a warning status condition (low priority, non-blocking).
