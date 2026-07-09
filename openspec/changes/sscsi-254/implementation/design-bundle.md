# Design Bundle — Task T8_1

**Change:** sscsi-254
**Task:** T8_1 — README quick-start update, if warranted
**Assigned Agent:** Docs_Agent
**Phase:** Phase 8: Documentation (terminal task)

## Task T8_1 Payload (from tasks.md §4)

- **Objective:** Update `README.md`'s quick-start `ClusterCSIDriver` example if the new `driverConfig.secretsStore` fields warrant a documentation mention.
- **Target file(s):** `README.md`.
- **Non-goals / forbidden edits:** Do not touch `docs/*-guidelines.md` (contributor conventions, not user docs).
- **Implementation notes:** Optional/judgment call — if the maintainers decide the quick-start example doesn't need the new fields shown, this task can close as "no change needed" with a one-line rationale.
- **Acceptance criteria:** Either an updated `README.md` example, or a documented decision that no update is needed.
- **Downstream handoff:** N/A — terminal task.

## Judgment call

Since `driverConfig.secretsStore` is entirely optional and its absence preserves the exact pre-feature default behavior (FR-010, verified in `T5_1`), the existing quick-start `ClusterCSIDriver` example does **not need to change** to remain correct. However, this feature (configurable secret rotation + WIF token audiences) is a headline, user-facing capability — leaving the README with zero mention of it would make it effectively undiscoverable to operators/admins who don't read `specs.md` or the API godoc directly.

**Decision: update the README** — add a new, clearly-optional documentation section immediately after the existing quick-start block, showing the `driverConfig.secretsStore` shape with both rotation and WIF examples. The existing minimal quick-start example is left untouched (no `driverConfig` shown there), since that already correctly represents the operator's default/no-op behavior.
