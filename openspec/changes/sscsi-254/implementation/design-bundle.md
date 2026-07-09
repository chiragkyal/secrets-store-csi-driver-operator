# Design Bundle — Task T1_1

**Change:** sscsi-254
**Task:** T1_1 — Track upstream `openshift/api` PR for `SecretsStore`
**Assigned Agent:** N/A (external — API approvers, no in-repo agent)
**Phase:** Phase 1: Vendor the Upstream API Extension

## Constitution excerpts (binding)

> **Principle X — Vendor Mode:** Dependencies are vendored. Never add a dependency without running `go mod tidy && go mod vendor`. Do NOT modify `vendor/` directly.
> **Principle III — No Custom CRD Types:** Spec-driven behavior changes MUST be expressed through existing `ClusterCSIDriver` fields ... or new controller hooks — not new CRD types. (This task's outcome — a new field on the existing CRD — is consistent with this principle; it is not introducing a new CRD.)

## Specs excerpts (relevant FRs)

> FR-005: System MUST reject rotation interval, token audience count, and token validity duration values that fall outside their supported bounds, with a clear validation error, before the configuration is applied.
> FR-007: Once an administrator opts in to operator-managed token audience configuration, System MUST NOT permit reverting to the prior externally-managed state.
> (Both enforced via upstream CEL rules in `openshift/api`, not in this repo's Go code.)

## Plan excerpts (relevant)

> §4 Dependencies & sequencing graph — Explicit blockers: "Hard blocker: the `openshift/api` PR (owner: API approvers, `@JoelSpeed` per the EP frontmatter) — this repo's Phase 1 cannot start (beyond speculative design) until it merges and is tagged."
> §8 Open Question 1: "Upstream `openshift/api` PR status and timeline ... Assumption if unanswered before Task Creation: treat as an unscheduled, long-lead external blocker."

## Repo-assessment excerpts (branch-verified fact)

> §0 / §2: `vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go`'s `CSIDriverType` enum is `"";AWS;Azure;GCP;IBMCloud;vSphere` — **no `SecretsStore` value exists**. `CSIDriverConfigSpec` has no `SecretsStore` field. This is a genuine cross-repository dependency, not implementable in this repo directly.

## Task T1_1 Payload (from tasks.md §4)

- **Objective:** Confirm (or initiate) the `github.com/openshift/api` change adding `SecretsStoreDriverType` and `SecretsStoreCSIDriverConfigSpec` to `CSIDriverConfigSpec`, and obtain a realistic merge/tag timeline.
- **Target file(s):** None in this repo — operates entirely against the external `openshift/api` repository/PR process.
- **Non-goals / forbidden edits:** Do not hand-edit anything under `vendor/github.com/openshift/api/` (Constitution Principle X).
- **Implementation notes:** Confirm with API approvers (`@JoelSpeed`) whether a PR already exists; if not, evaluate filing one following the existing discriminated-union pattern. Evaluate whether a temporary `go.mod` `replace` directive against a fork/branch is acceptable for development-time iteration only.
- **Acceptance criteria:** A known PR reference (or filed PR) exists with a stated target merge/tag; OR an explicit decision to proceed with a `replace`-directive-based development fork is documented and communicated to reviewers.
- **Downstream handoff:** The confirmed upstream commit/tag (or fork SHA) that `T1_2` will vendor.

## Execution approach

This is a **manual, non-code-generation task** (routing: "manual agent → implement task payload directly"). No OAPE command applies — the work is investigation against the external `openshift/api` repository, not code authored in this repo.
