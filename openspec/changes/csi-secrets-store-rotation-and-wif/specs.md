# Feature Specification: Configurable Secret Rotation and Workload Identity Federation

**Feature Branch**: `[ssCSI-254-csi-secrets-store-rotation-and-wif]`

**Created**: 2026-07-10

**Status**: Draft

**Input**: User description: "Allow administrators to configure secret rotation behavior and workload identity federation token audiences for the secrets store CSI capability without manual manifest edits."

<!--
  QUALITY TARGET: ≥95% against the Stage 1 rubric before output is final.
  Self-check (all must pass):
  - Every FR maps to ≥1 Given/When/Then scenario; every P1 story has ≥2 scenarios.
  - Zero implementation leakage (no languages, frameworks, file paths, API groups, version pins).
  - Success criteria are user-observable outcomes — NOT CI gates, release processes, or internal milestones.
  - Edge cases state concrete outcomes (not open questions); resolve singleton/scope ambiguities in FR text.
  - At most 3 [NEEDS CLARIFICATION] markers total; all other gaps become numbered Assumptions (A-001…).
  - Assumptions section is complete — one bullet per unresolved ticket gap or Stage 0 missing_element.
-->

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Control Secret Rotation Behavior (Priority: P1)

A cluster administrator wants to choose whether secret content is refreshed automatically and, when enabled, how aggressively refresh attempts should occur so the platform can balance secret freshness against provider load.

**Why this priority**: Rotation behavior affects core day-2 operations for every workload that relies on mounted secrets, and administrators need direct control over platform-wide refresh cost and freshness.

**Independent Test**: Can be fully tested by applying each supported rotation mode to the operator-managed secret delivery capability and observing whether refresh behavior is enabled, disabled, or throttled to the configured minimum interval.

**Acceptance Scenarios**:

1. **Given** automatic secret refresh is using the platform default behavior, **When** an administrator disables automatic rotation, **Then** the platform stops performing periodic refresh attempts for already-mounted secrets.
2. **Given** automatic secret refresh is enabled, **When** an administrator sets a custom minimum refresh age, **Then** the platform continues automatic refreshes but does not attempt them more frequently than the configured minimum interval.
3. **Given** an administrator leaves rotation configuration unset, **When** the platform reconciles the managed secret delivery capability, **Then** the platform retains its built-in default rotation behavior instead of requiring explicit configuration.

---

### User Story 2 - Configure Federated Identity Token Audiences (Priority: P1)

A platform engineer wants to declare one or more service account token audiences, with optional token lifetimes, so workloads can federate identity with external secret providers through the platform-managed secret delivery capability.

**Why this priority**: Federated identity support is the new user-facing capability in scope and is required for multi-cloud secret retrieval without manual operand patching.

**Independent Test**: Can be fully tested by configuring one or more token audiences, then confirming that workloads using the managed secret delivery capability receive tokens for all configured audiences and optional lifetimes.

**Acceptance Scenarios**:

1. **Given** federated identity management is enabled through the operator configuration, **When** an administrator supplies one or more token audiences, **Then** workloads using the managed secret delivery capability receive tokens that target every configured audience.
2. **Given** federated identity management is enabled, **When** an administrator supplies an optional token lifetime for an audience, **Then** the requested lifetime is included in the platform-managed token request for that audience.
3. **Given** federated identity management is enabled, **When** an administrator supplies an empty managed audience list, **Then** the platform clears all operator-managed token audience requests.

---

### User Story 3 - Preserve Existing Behavior During Upgrade and Ownership Changes (Priority: P2)

A cluster administrator wants upgrades and ownership transitions to preserve current workload behavior until the administrator explicitly opts into new management modes so existing clusters do not lose secret access or identity federation unexpectedly.

**Why this priority**: Safe adoption is critical, but it builds on the primary rotation and federated identity capabilities rather than replacing them.

**Independent Test**: Can be tested independently by upgrading a cluster with existing secret delivery behavior and previously configured token audiences, then verifying that current behavior is preserved until the administrator explicitly chooses operator-managed ownership.

**Acceptance Scenarios**:

1. **Given** a cluster already has externally configured token audiences, **When** the platform is upgraded without new federated identity settings, **Then** the existing token audience behavior is preserved without manual intervention.
2. **Given** an administrator has not yet opted into operator-managed token audience ownership, **When** the administrator enables operator-managed ownership with a defined audience set, **Then** the platform adopts the declared audiences as the sole managed source of truth.
3. **Given** operator-managed token audience ownership is already enabled, **When** an administrator attempts to switch back to preserve-existing behavior mode, **Then** the platform rejects that change and keeps operator-managed ownership in effect.

---

### User Story 4 - Support Multi-Cloud Identity Federation Choices (Priority: P3)

A multi-cloud operator wants to configure distinct token audiences for different cloud identity providers on the same cluster so workloads can authenticate to multiple external secret systems without separate manual customization.

**Why this priority**: This expands the value of federated identity support for more advanced operating models, but it depends on the base identity-management capability already being available.

**Independent Test**: Can be tested independently by configuring multiple distinct token audiences and verifying that the managed secret delivery capability exposes all of them to workloads that need federated identity.

**Acceptance Scenarios**:

1. **Given** an administrator manages workloads across multiple cloud environments, **When** the administrator configures more than one token audience, **Then** the platform preserves all distinct configured audiences in the effective federated identity configuration.

---

### Edge Cases

- **When** an administrator provides a rotation interval or token lifetime outside the supported range, **then** the platform rejects the configuration before changing existing secret delivery behavior. 
- **When** an administrator enables operator-managed federated identity on a cluster that already has externally maintained token audiences, **then** the platform replaces the preserved behavior only after the administrator explicitly supplies the managed audience set.
- **When** an administrator disables automatic rotation after it was previously enabled, **then** periodic secret refresh attempts stop and secrets are refreshed only on initial mount or future explicit remount events.
- **When** a cluster upgrades without any new secret-delivery configuration, **then** the platform preserves current refresh behavior and retains any previously configured token audiences.
- **When** the platform cannot apply a valid administrator-requested configuration, **then** the administrator receives an observable degraded or error state instead of silent partial adoption.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST allow cluster administrators to leave automatic secret rotation at the platform default, disable it entirely, or enable it with a custom minimum refresh age.
- **FR-002**: System MUST apply administrator-selected rotation behavior to all workloads that rely on the operator-managed secret delivery capability.
- **FR-003**: System MUST allow administrators to define zero or more federated identity token audiences, each with an optional requested token lifetime.
- **FR-004**: System MUST preserve previously existing token audience behavior until an administrator explicitly opts into operator-managed federated identity ownership.
- **FR-005**: System MUST treat operator-managed federated identity ownership as a one-way transition; once enabled, administrators MUST NOT be able to revert to preserve-existing behavior mode.
- **FR-006**: System MUST persist administrator-selected rotation and federated identity settings across operator restarts, workload restarts, and platform upgrades.
- **FR-007**: System MUST reject invalid or internally inconsistent administrator configuration before replacing the currently effective behavior.
- **FR-008**: System MUST surface an operator-visible failure state when requested configuration cannot be applied successfully.
- **FR-009**: System MUST support multiple distinct federated identity token audiences at the same time for the same managed secret delivery capability.
- **FR-010**: System MUST allow administrators to clear all operator-managed federated identity audiences without removing the broader secret delivery capability.

### Key Entities *(include if feature involves data)*

- **Secret Rotation Policy**: Administrator-selected refresh behavior for mounted secrets, including default behavior, disabled behavior, or a custom minimum refresh age.
- **Federated Identity Audience Entry**: A requested token audience and optional token lifetime used by workloads to authenticate to an external secret provider.
- **Federated Identity Ownership Mode**: The control mode that determines whether existing token audience behavior is preserved or replaced by operator-managed configuration.
- **Managed Secret Delivery Configuration**: The full administrator-managed settings that govern rotation behavior, federated identity behavior, and upgrade-safe defaults for the platform's secret delivery capability.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An administrator can disable automatic secret rotation and observe that already-mounted secrets stop refreshing automatically after the configuration change becomes effective.
- **SC-002**: An administrator can set a custom minimum refresh age and observe that automatic refresh attempts do not occur more frequently than the configured interval.
- **SC-003**: When an administrator configures one or more federated identity audiences, workloads using the managed secret delivery capability can obtain tokens for all configured audiences.
- **SC-004**: Clusters upgraded without new administrator configuration continue the same effective refresh behavior and retain any pre-existing token audience behavior without manual intervention.
- **SC-005**: When an administrator opts into operator-managed federated identity ownership, the effective audience set matches the administrator-provided list and can be reduced to zero audiences intentionally.
- **SC-006**: Invalid administrator configuration is rejected before the platform replaces the previously effective secret delivery behavior.

## Assumptions

- **A-001**: Cluster administrators and platform engineers are the primary users responsible for managing this feature.
- **A-002**: Provider-specific secret system configuration remains out of scope; this feature only governs the platform-managed secret delivery behavior and token requests needed by those providers.
- **A-003**: Existing clusters may already rely on externally maintained federated identity audiences, and preserving that behavior until explicit opt-in is a required compatibility expectation.
- **A-004**: Leaving the custom rotation interval unspecified means the platform will continue to use a platform-selected default rather than a user-declared value.
- **A-005**: Guidance about choosing a safe refresh interval may be documented separately, but documentation deliverables are not part of this specification unless explicitly added later.
- **A-006**: Unsupported or not-yet-supported platform footprints remain outside scope unless a future ticket explicitly broadens support requirements.
