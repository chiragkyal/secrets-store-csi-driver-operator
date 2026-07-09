# Feature Specification: Configurable Secret Rotation and Workload Identity Federation for the Secrets Store CSI Driver

**Feature Branch**: `csi-secrets-store-rotation-and-wif`

**Created**: 2026-07-09

**Status**: Draft

**Input**: User description: "SSCSI-254 — extend the Secrets Store CSI driver so cluster administrators can configure secret rotation behavior (enable/disable, polling interval) and workload identity federation (WIF) token audiences, instead of relying on hardcoded defaults."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Disable or enable automatic secret rotation (Priority: P1)

As a cluster administrator, I want to turn automatic secret rotation on or off for the driver, so that workloads using static secrets don't generate unnecessary provider API calls that could count against rate limits.

**Why this priority**: This directly addresses a stated pain point (unnecessary provider calls) and is the simplest, highest-value control an administrator can exercise. It is also the safest to deliver independently — it only affects an existing, already-enabled behavior.

**Independent Test**: Can be fully tested by disabling rotation on a cluster with an active secret mount and confirming that no further periodic secret refresh occurs, then re-enabling it and confirming refresh resumes.

**Acceptance Scenarios**:

1. **Given** a running driver with rotation enabled by default, **When** an administrator disables rotation, **Then** the driver stops periodically refreshing already-mounted secrets and only fetches them again on the next pod mount.
2. **Given** rotation has been disabled, **When** an administrator re-enables rotation, **Then** periodic secret refresh resumes without requiring workloads to be restarted.

---

### User Story 2 - Configure workload identity federation token audiences (Priority: P1)

As a platform engineer, I want to configure one or more service account token audiences for the driver, so that pods can authenticate to external cloud secret providers (AWS, Azure, GCP) using workload identity federation instead of long-lived credentials.

**Why this priority**: This unlocks an entirely new capability (WIF) that many multi-cloud and zero-long-lived-credential environments require, and is the second core problem this feature exists to solve.

**Independent Test**: Can be fully tested by configuring a token audience for a cloud provider, mounting a secret through the driver in a pod, and confirming the driver receives a token for that audience and successfully authenticates to the provider.

**Acceptance Scenarios**:

1. **Given** no token audience configuration has been set, **When** a platform engineer adds a token audience, **Then** the driver begins requesting and receiving a token for that audience for subsequent pod mounts.
2. **Given** a token audience has already been configured, **When** a platform engineer removes it by setting an empty audience list, **Then** the driver stops requesting a token for that audience.

---

### User Story 3 - Tune the secret rotation refresh interval (Priority: P2)

As a cluster administrator, I want to configure how frequently secrets are checked for rotation, so that I can balance secret freshness against load on the external secret provider.

**Why this priority**: Builds on User Story 1; valuable but secondary to the basic enable/disable control, since a reasonable default already exists.

**Independent Test**: Can be fully tested by setting a custom refresh interval and confirming, through observed refresh timing, that the driver checks for updated secrets at approximately that cadence rather than the default.

**Acceptance Scenarios**:

1. **Given** rotation is enabled with the default interval, **When** an administrator sets a custom refresh interval, **Then** the driver checks for secret updates at approximately the new interval going forward.
2. **Given** a custom refresh interval has been set, **When** an administrator removes the custom value, **Then** the driver falls back to the default refresh interval.

---

### User Story 4 - Federate identity with multiple cloud providers simultaneously (Priority: P2)

As a multi-cloud operator, I want to configure multiple token audiences at once, so that different workloads on the same cluster can federate identity with different cloud providers (e.g., AWS and Azure) at the same time.

**Why this priority**: Extends User Story 2 to multi-cloud environments; important for a meaningful subset of clusters but not required for the baseline single-provider WIF capability to deliver value.

**Independent Test**: Can be fully tested by configuring two or more audiences for different providers and confirming pods can independently obtain tokens for each configured audience.

**Acceptance Scenarios**:

1. **Given** no token audiences are configured, **When** a multi-cloud operator configures audiences for two different providers, **Then** the driver requests and receives a distinct token for each configured audience.

---

### Edge Cases

- **When** an administrator upgrades a cluster that has pre-existing, manually configured token audiences and does not set any new rotation or token configuration, **then** the existing token audience configuration and the previous rotation cadence remain unchanged, with no disruption to running workloads.
- **When** an administrator submits a rotation interval below the allowed minimum (1 second) or above the allowed maximum (approximately 1 year), **then** the configuration is rejected with a validation error and no change is applied.
- **When** a platform engineer submits a token expiration value below the allowed minimum (10 minutes) or above the allowed maximum (approximately 10 years), **then** the configuration is rejected with a validation error and no change is applied.
- **When** an administrator attempts to revert token audience configuration from operator-managed back to an unmanaged state, **then** the request is rejected with a clear error and the existing managed configuration remains in effect.
- **When** an administrator sets the managed audience list to an explicit empty list, **then** all previously configured managed token audiences are cleared from the driver.
- **When** rotation is disabled, **then** the driver stops periodic refresh attempts entirely and only fetches secret values at initial pod mount time.
- **When** the operator is downgraded to a version that predates this feature after token audience configuration has already been set to operator-managed, **then** [NEEDS CLARIFICATION: behavior is not defined by the source specification — does the prior operator version preserve the existing managed audience configuration untouched, ignore it, or risk disrupting workload identity federation?]

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST allow administrators to enable or disable automatic secret rotation through the driver's existing cluster-wide configuration mechanism.
- **FR-002**: System MUST allow administrators to configure a custom minimum interval between rotation attempts, bounded between 1 second and approximately 1 year, when rotation is explicitly enabled with a custom configuration.
- **FR-003**: System MUST apply a default rotation behavior (enabled, with a default minimum interval of 2 minutes) whenever no explicit rotation configuration is provided, so that upgrading clusters see no change in behavior.
- **FR-004**: System MUST allow platform engineers to configure one or more service account token audiences, each with an optional expiration duration, for use in workload identity federation with external cloud secret providers.
- **FR-005**: System MUST preserve any token audience configuration already present on the driver's registration whenever no explicit token audience configuration is provided by the administrator, avoiding disruption to already-configured identity federation.
- **FR-006**: Once an administrator has explicitly designated token audience configuration as managed by the operator, System MUST reject any attempt to revert that designation back to an unmanaged state.
- **FR-007**: System MUST allow an administrator to clear all operator-managed token audiences by explicitly submitting an empty audience list, distinct from omitting the configuration entirely.
- **FR-008**: System MUST validate rotation interval and token audience/expiration values at submission time and reject values outside the allowed bounds before they are applied to any running component.
- **FR-009**: System MUST propagate rotation and token audience configuration changes to the running driver components without requiring administrators to manually restart or recreate workloads.
- **FR-010**: System MUST retain administrator-configured rotation and token audience settings across operator upgrades and driver component restarts.
- **FR-011**: When rotation is disabled, System MUST stop periodic secret refresh so that secret values are fetched only at initial pod mount time.
- **FR-012**: System MUST NOT change rotation or token audience behavior for existing clusters that do not opt in to this feature's configuration.

### Key Entities

- **Secret Rotation Configuration**: Represents whether automatic secret rotation is enabled for the driver and, when enabled with a custom configuration, the minimum interval between rotation attempts.
- **Token Audience**: Represents a single workload-identity-federation audience value and an optional token expiration duration, used by the driver when requesting a service account token for that audience.
- **Driver Configuration (cluster-scoped)**: The administrator-facing configuration surface where rotation and token audience settings are set for a given Secrets Store CSI driver instance.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An administrator can disable secret rotation and confirm, within a few minutes (one reconciliation cycle), that the driver has stopped issuing periodic refresh calls to the secret provider.
- **SC-002**: An administrator can set a custom rotation interval and observe secrets refreshing at approximately that cadence without any manual restart of workloads.
- **SC-003**: A platform engineer can configure a token audience and confirm that a pod using the driver successfully authenticates to the corresponding cloud provider using workload identity federation.
- **SC-004**: A multi-cloud operator can configure audiences for two or more providers simultaneously and confirm each is independently usable by pods on the cluster.
- **SC-005**: Clusters upgrading from a version without this feature retain their existing rotation cadence and any manually configured token audiences with zero observable disruption to running workloads.
- **SC-006**: 100% of rotation interval or token audience/expiration submissions outside the allowed bounds are rejected at submission time, with zero invalid values reaching running driver components.
- **SC-007**: 100% of attempts to revert operator-managed token audience configuration back to unmanaged are rejected with a clear error, and the previously managed configuration remains intact.

## Assumptions

- **A-001**: Administrators and platform engineers interact with this configuration through the driver's existing cluster-scoped configuration mechanism already used for other driver settings; no new administrator-facing configuration surface is introduced.
- **A-002**: This feature does not include automatic detection of the underlying cloud provider; administrators and platform engineers must explicitly provide the correct token audience values for their environment (addresses Stage 0 non-goal).
- **A-003**: Provider-specific integration details (e.g., a specific cloud vault's authentication flow) are out of scope; this feature only concerns the rotation and token-audience configuration surfaced by the driver itself.
- **A-004**: Detailed guidance on choosing a rotation interval that avoids overwhelming the secret provider is a documentation concern, not an enforceable system requirement beyond the numeric bounds already stated in FR-002 (addresses Stage 0 quality issue re: vague mitigation language).
- **A-005**: The rotation and token-audience capabilities described here are delivered together as a single feature; partial delivery of only one capability is not in scope for this specification (addresses Stage 0 sizing observation).
- **A-006**: The exact target platform release version is a documentation/release detail outside the scope of this specification; this spec assumes delivery on a currently supported platform release line (addresses Stage 0 quality issue re: an inconsistent version reference in the source material).
- **A-007**: Hosted/hypershift-specific behavior, single-node deployments, and non-standard topologies are out of scope unless explicitly required; this feature targets standard multi-node cluster topologies.
