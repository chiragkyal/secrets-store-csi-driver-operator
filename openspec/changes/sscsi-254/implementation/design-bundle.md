# Design Bundle — Task T1_2

**Change:** sscsi-254
**Task:** T1_2 — Bump `go.mod`/`go.sum`/`vendor/` once merged
**Assigned Agent:** ControllerLogic_Agent
**Phase:** Phase 1: Vendor the Upstream API Extension

## Constitution excerpts (binding)

> **Principle X — Vendor Mode:** Dependencies are vendored. Never add a dependency without running `go mod tidy && go mod vendor`. Do NOT modify `vendor/` directly. The `.snyk` file tracks security policy — do not remove it.
> **Additional Constraints — Go version**: Match `go.mod` directive — currently `go 1.25.0`.

## Prior task finding (T1_1, now complete)

> `openshift/api` PR #2846 ("SSCSI-245: Add Secrets Store CSI driver configuration to ClusterCSIDriver API") is MERGED at commit `50c3975e874ff67ee526e9ba68fc4f4edd5137ee` (2026-06-24) on `master`. Confirmed API shape: `secretsStore *SecretsStoreCSIDriverConfigSpec` (pointer field) on `CSIDriverConfigSpec`; `SecretsStoreCSIDriverConfigSpec{secretRotation, tokenRequests}`; `SecretsStoreSecretRotation{type, custom *CustomSecretRotation}`; `CustomSecretRotation{rotationPollIntervalSeconds}`; `SecretsStoreTokenRequests{type, managed *ManagedTokenRequests}`; `ManagedTokenRequests{audiences []SecretsStoreTokenRequest}`; `SecretsStoreTokenRequest{audience, expirationSeconds}`.

## Repo-assessment excerpts (current state, branch-verified)

> Current `go.mod` pin: `github.com/openshift/api v0.0.0-20260302174620-dcac36b908db` (March 2, 2026) — predates the June 24 merge, so the vendored `CSIDriverType` enum still lacks `SecretsStore` and `CSIDriverConfigSpec` still lacks the `secretsStore` field.

## Task T1_2 Payload (from tasks.md §4)

- **Objective:** Consume the new `SecretsStore` driver-type API surface once it lands in `github.com/openshift/api`.
- **Target file(s):** `go.mod`, `go.sum`, `vendor/modules.txt`, `vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go` (regenerated via `go mod vendor`, never hand-edited — Constitution Principle X).
- **Non-goals / forbidden edits:** Do not hand-edit any file under `vendor/`. Do not add a permanent `replace` directive (not needed — the real dependency is already merged, per T1_1).
- **Implementation notes:** `go get github.com/openshift/api@<new-sha-or-tag>` then `go mod vendor`. Confirm the vendored `CSIDriverType` enum now includes `SecretsStore` and `CSIDriverConfigSpec` has the new field.
- **Acceptance criteria:** `go mod tidy && go mod vendor && make verify` passes cleanly; the new types are visible and compile in `vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go`.
- **Downstream handoff:** A vendored tree with the new Go types available for `T2_1` to import.

## Execution approach

Manual agent task — no OAPE command applies (this is a dependency/vendor bump, not controller logic or API-type authoring in *this* repo). Execute `go get`/`go mod vendor` directly, then verify.
