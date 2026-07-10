# Feature Specification: Configurable Secret Rotation and Workload Identity Federation

**Feature Branch**: `sscsi-254-rotation-and-wif`

**Created**: 2026-07-10

**Status**: Draft

**Input**: SSCSI-254 — Extend the Secrets Store CSI Driver Operator to allow cluster administrators to configure secret rotation behavior and workload identity federation token audiences through the operator's configuration resource.

## User Scenarios & Testing

### User Story 1 - Disable Secret Rotation for Static Workloads (Priority: P1)

As a cluster administrator, I want to disable automatic secret rotation for workloads that use static secrets, so that the driver does not make unnecessary provider API calls that count against rate limits.

**Why this priority**: Disabling rotation for static-secret workloads eliminates unnecessary provider API load and avoids rate-limiting. This is the most basic knob administrators need to control cost and reliability.

**Independent Test**: Can be fully tested by setting rotation to disabled on the operator configuration and verifying that no periodic secret re-fetching occurs after initial mount.

**Acceptance Scenarios**:

1. **Given** a cluster with the operator running and rotation previously enabled, **When** the administrator sets rotation to disabled in the operator configuration, **Then** the driver stops periodic re-fetching of secrets — secrets are only fetched at initial pod mount time.
2. **Given** rotation is disabled, **When** a new pod mounts a secret volume, **Then** the secret is fetched once at mount time and never refreshed automatically.
3. **Given** rotation is disabled, **When** the administrator later re-enables rotation, **Then** periodic re-fetching resumes with the configured or default interval.

---

### User Story 2 - Configure Rotation Polling Interval (Priority: P1)

As a cluster administrator, I want to configure the rotation polling interval so that I can tune the trade-off between secret freshness and provider API load.

**Why this priority**: Different environments have different freshness requirements. A security-sensitive cluster may want faster rotation (e.g., 1 minute), while a cost-sensitive cluster may prefer slower rotation (e.g., 30 minutes). Interval tuning is essential for production readiness.

**Independent Test**: Can be fully tested by setting a custom interval, mounting a secret, and verifying that the driver re-fetches from the provider at the configured cadence rather than the default.

**Acceptance Scenarios**:

1. **Given** the operator is configured with a custom rotation interval of 5 minutes, **When** a pod has a mounted secret, **Then** the driver re-fetches the secret from the provider no more frequently than every 5 minutes.
2. **Given** the operator configuration specifies a custom rotation interval, **When** the interval is changed to a different value, **Then** the driver begins using the new interval on the next reconciliation cycle without manual pod restarts.
3. **Given** the administrator sets a custom rotation interval, **When** the interval value is omitted (reset to default), **Then** the system falls back to the built-in default interval of 2 minutes.

---

### User Story 3 - Configure Token Audiences for Workload Identity Federation (Priority: P1)

As a platform engineer, I want to configure service account token audiences on the driver so that pods can use workload identity federation to authenticate with cloud provider secret stores (e.g., AWS STS, Azure AD, GCP IAM).

**Why this priority**: WIF is the recommended authentication pattern for cloud-native workloads. Without operator-managed token audiences, administrators must manually patch the driver's cluster-level configuration — a fragile approach that does not survive operator reconciliation or upgrades.

**Independent Test**: Can be fully tested by configuring a token audience in the operator configuration and verifying that the driver receives service account tokens with the specified audience during volume mount.

**Acceptance Scenarios**:

1. **Given** the operator configuration specifies a token audience (e.g., "sts.amazonaws.com"), **When** a pod mounts a secret volume, **Then** the driver receives a service account token scoped to the configured audience.
2. **Given** the operator configuration specifies a token audience with a custom expiration (e.g., 3600 seconds), **When** a pod mounts a secret volume, **Then** the token provided to the driver has the requested validity duration.
3. **Given** token management is set to operator-managed mode, **When** the administrator updates the audiences list, **Then** the driver configuration is updated to reflect the new audiences without manual intervention.

---

### User Story 4 - Multi-Cloud WIF with Multiple Audiences (Priority: P2)

As a multi-cloud operator, I want to configure multiple token audiences on a single driver instance so that different workloads on the same cluster can federate identity with different cloud providers simultaneously.

**Why this priority**: Multi-cloud deployments are increasingly common. Supporting multiple audiences on a single cluster avoids the need for separate driver instances per cloud provider and simplifies cluster management.

**Independent Test**: Can be fully tested by configuring two audiences (e.g., one for AWS, one for Azure), mounting two different secret volumes from different providers, and verifying each receives the correct audience-scoped token.

**Acceptance Scenarios**:

1. **Given** the operator configuration specifies two token audiences (e.g., "sts.amazonaws.com" and "api://AzureADTokenExchange"), **When** pods mount secrets from both providers, **Then** each pod receives tokens scoped to the appropriate audience.
2. **Given** multiple audiences are configured, **When** the administrator adds a third audience, **Then** the existing two audiences continue to function and the third is added without disruption.
3. **Given** multiple audiences are configured, **When** the administrator removes one audience from the list, **Then** only the removed audience stops being provided; the remaining audiences are unaffected.

---

### User Story 5 - Preserve Existing Token Configuration on Upgrade (Priority: P1)

As a cluster administrator, I want my existing manually-configured token audiences (e.g., for Azure WIF) to be preserved when the operator is upgraded, so that workload identity federation is not disrupted.

**Why this priority**: Clusters already running WIF with manually patched token configuration must not experience disruption on operator upgrade. Breaking existing WIF would cause secret mount failures and workload outages.

**Independent Test**: Can be fully tested by manually configuring token audiences on the driver, upgrading the operator, and verifying the existing token configuration remains intact.

**Acceptance Scenarios**:

1. **Given** a cluster has manually configured token audiences on the driver (pre-upgrade), **When** the operator is upgraded to the version supporting this feature, **Then** the existing token audiences are preserved without modification.
2. **Given** the operator has been upgraded and token management is not explicitly configured, **When** the operator reconciles, **Then** the driver's token configuration matches the pre-upgrade state exactly — no additions, no removals.
3. **Given** existing token configuration is preserved after upgrade, **When** the administrator explicitly opts in to operator-managed tokens and provides an audiences list, **Then** the operator replaces the previous token configuration with the administrator-specified list.

---

### User Story 6 - Configuration Persistence Across Restarts (Priority: P2)

As a cluster administrator, I want my rotation and token configuration to persist across operator upgrades and pod restarts without manual re-intervention.

**Why this priority**: Configuration durability is a basic operational expectation. Administrators should not need to re-apply settings after operator restarts, pod rescheduling, or cluster maintenance windows.

**Independent Test**: Can be fully tested by configuring rotation and token settings, restarting the operator, and verifying the settings remain in effect.

**Acceptance Scenarios**:

1. **Given** the administrator has configured custom rotation and token settings, **When** the operator pod restarts, **Then** the same settings are applied to the driver without manual re-intervention.
2. **Given** the administrator has configured custom settings, **When** the operator is upgraded to a new version, **Then** the settings are preserved and continue to be enforced.

---

### Edge Cases

- **When** the administrator sets the rotation interval to the minimum allowed value (1 second), **then** the system accepts the configuration but the effective rotation cadence is bounded by the platform's internal sync frequency (typically 1 minute), resulting in no faster rotation than that floor.
- **When** the administrator sets the rotation interval to the maximum allowed value (~1 year), **then** the system accepts the configuration and secrets are re-fetched no more often than once per year.
- **When** the administrator configures an invalid rotation interval (e.g., 0 or negative), **then** the system rejects the configuration at admission time with a clear validation error.
- **When** the administrator configures a token expiration below the platform minimum (10 minutes), **then** the system rejects the configuration at admission time.
- **When** the administrator attempts to revert token management from operator-managed back to unmanaged mode, **then** the system rejects the change — this is a one-way transition to prevent accidental loss of operator control.
- **When** the administrator provides an empty audiences list in operator-managed mode, **then** the system clears all token audiences from the driver — this is the supported way to remove WIF configuration.
- **When** the administrator provides duplicate audience values, **then** the system rejects the configuration — each audience must be unique.
- **When** the operator configuration resource has no driver-specific configuration set (upgrade scenario), **then** the system behaves identically to the pre-upgrade behavior: rotation enabled at the default interval, existing token configuration preserved.
- **When** the driver's cluster-level configuration is deleted and recreated during a spec update, **then** the window of absence is negligible and does not affect already-running pods.
- **When** the maximum number of token audiences (10) is reached and the administrator attempts to add another, **then** the system rejects the configuration with a clear validation error.

## Requirements

### Functional Requirements

- **FR-001**: System MUST allow administrators to disable automatic secret rotation through the operator configuration, causing secrets to be fetched only at initial pod mount time.
- **FR-002**: System MUST allow administrators to enable automatic secret rotation with a custom polling interval, specified in seconds, controlling how frequently the driver re-fetches secrets from the provider.
- **FR-003**: When rotation configuration is omitted from the operator configuration, the system MUST default to rotation enabled with a 2-minute polling interval, matching the pre-existing behavior.
- **FR-004**: When custom rotation is enabled and the polling interval is omitted, the system MUST use a reasonable default interval (currently 2 minutes).
- **FR-005**: System MUST validate that the rotation polling interval is between 1 second and 31,560,000 seconds (~1 year) and reject values outside this range at admission.
- **FR-006**: System MUST allow administrators to configure service account token audiences for workload identity federation, specifying the audience string and optional token expiration for each entry.
- **FR-007**: System MUST support at least 10 simultaneous token audience configurations on a single driver instance.
- **FR-008**: System MUST enforce unique audience values — duplicate audiences in the configuration MUST be rejected.
- **FR-009**: System MUST validate that token expiration values are between 600 seconds (10 minutes) and 315,360,000 seconds (~10 years) and reject values outside this range at admission.
- **FR-010**: System MUST support two token management modes: "operator-managed" where the operator is the sole source of truth for token configuration, and "unmanaged" where the operator preserves existing token configuration without modification.
- **FR-011**: When token management mode is not configured (or set to unmanaged), the system MUST preserve any existing token configuration on the driver without modification — including token audiences that were manually configured before this feature existed.
- **FR-012**: Transitioning from unmanaged to operator-managed token mode MUST be allowed, but transitioning back from operator-managed to unmanaged MUST be rejected — this is a one-way transition.
- **FR-013**: When the administrator provides an empty audiences list in operator-managed mode, the system MUST clear all token audiences from the driver.
- **FR-014**: System MUST propagate rotation configuration changes to the driver's runtime arguments dynamically, triggering a rolling update of driver pods without requiring operator restart.
- **FR-015**: System MUST propagate token audience changes to the driver's cluster-level configuration dynamically, recreating the configuration object as needed (since the spec is immutable).
- **FR-016**: System MUST NOT change any driver behavior on operator upgrade when no driver-specific configuration is set on the operator configuration resource — the default behavior MUST exactly match pre-upgrade behavior.
- **FR-017**: System MUST dynamically enable the platform's periodic volume republish mechanism when rotation is enabled, and disable it when rotation is disabled.
- **FR-018**: The operator configuration MUST use discriminated union semantics for both rotation and token settings — the type discriminator determines which fields are valid, and the system MUST reject configurations where the discriminator and associated fields are inconsistent.

### Key Entities

- **Secret Rotation Configuration**: Controls whether the driver periodically re-fetches secrets from the provider. Has a mode (disabled or custom) and, when custom, a polling interval in seconds.
- **Token Management Configuration**: Controls how the operator manages service account tokens for workload identity federation. Has a mode (operator-managed or unmanaged) and, when operator-managed, a list of audience entries.
- **Token Audience Entry**: Represents a single service account token audience for WIF. Has an audience string (the cloud provider's expected audience value) and an optional expiration in seconds.
- **Driver Cluster-Level Configuration**: The platform object describing driver capabilities including whether periodic volume republish is enabled and which token audiences are requested. Effectively immutable — changes require delete and recreate.

## Success Criteria

### Measurable Outcomes

- **SC-001**: Administrator can disable rotation via operator configuration and observe that mounted secrets are not re-fetched after initial mount (verified by checking that the driver does not contact the provider after the initial mount).
- **SC-002**: Administrator can set a custom rotation interval (e.g., 5 minutes) and observe that the driver re-fetches secrets at the configured cadence rather than the default 2-minute interval.
- **SC-003**: Administrator can configure a token audience and observe that pods mounting secret volumes receive service account tokens scoped to the specified audience (verified by inspecting the volume context at mount time).
- **SC-004**: An existing cluster that upgrades to the operator version with this feature, without setting any new configuration, experiences zero behavior change — rotation remains enabled at 2 minutes, existing token audiences are preserved, and no driver pod rolling update is triggered.
- **SC-005**: A cluster with pre-existing manually-configured token audiences (e.g., Azure WIF) preserves those audiences through operator upgrade and ongoing reconciliation when token management is not explicitly configured.
- **SC-006**: Invalid configuration (out-of-range intervals, invalid expiration, duplicate audiences, inconsistent discriminator/field combinations, reverting managed-to-unmanaged) is rejected at admission time with a clear error message — not at runtime.

## Assumptions

- **A-001**: The target user persona is a cluster administrator or platform engineer with permission to edit the operator's cluster-scoped configuration resource.
- **A-002**: The feature targets GA directly — there is no Tech Preview gate. All configuration fields are stable from their initial release.
- **A-003**: The upstream driver (v1.6.0+) already supports the periodic republish mechanism and token request capabilities. This feature exposes those capabilities through the operator — it does not implement them in the driver itself.
- **A-004**: The platform's internal sync frequency (kubelet syncFrequency, default 1 minute) acts as a natural floor on effective rotation cadence, regardless of the configured interval. This is expected behavior, not a bug.
- **A-005**: Hypershift / Hosted Control Planes, Single-node / MicroShift, and OpenShift Kubernetes Engine topologies are not in scope for this enhancement.
- **A-006**: Provider-specific configuration (e.g., Azure Key Vault, AWS Secrets Manager, HashiCorp Vault) is out of scope — providers are installed and configured separately.
- **A-007**: The system does not auto-detect which cloud provider a cluster runs on. Administrators must explicitly configure the appropriate token audiences for their environment.
- **A-008**: Changes to the driver's cluster-level configuration object require delete and recreate (the spec field is immutable). The brief absence window during recreation does not affect running pods.
- **A-009**: The two impacted codebases are: (1) the API types repository (for the configuration schema extension) and (2) the operator repository (for the controller logic that propagates configuration to the driver). This addresses the validation finding about missing repository inventory.
