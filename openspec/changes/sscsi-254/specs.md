# Feature Specification: Configurable Secret Rotation and Workload Identity Federation for the Secrets Store CSI Driver

**Feature Branch**: `254-secret-rotation-and-wif`

**Created**: 2026-07-09

**Status**: Draft

**Input**: User description: "SSCSI-254: Configurable Secret Rotation and Workload Identity Federation for the Secrets Store CSI Driver (sourced from Enhancement Proposal `openspec/inputs/ep.md`, in lieu of a live Jira ticket fetch)"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Control Automatic Secret Rotation (Priority: P1)

Cluster administrators want to enable, disable, or tune how frequently mounted secrets are automatically refreshed from the external secret provider, so they can balance secret freshness against provider API load and rate limits.

**Why this priority**: Today rotation is always on at a fixed interval. Administrators running static-secret workloads have no way to stop unnecessary provider calls, and administrators with fast-changing secrets have no way to tighten the refresh window. This is a direct, immediate cost/control gap affecting every cluster running the driver today.

**Independent Test**: Can be fully tested by disabling rotation for a workload and confirming no further provider calls occur, and separately by setting a custom interval and confirming secrets refresh at approximately that cadence — without touching workload identity federation configuration at all.

**Acceptance Scenarios**:

1. **Given** a workload using secrets that never change, **When** the administrator disables automatic rotation for the driver, **Then** the driver only fetches secrets from the provider at initial mount time and makes no further periodic provider calls.
2. **Given** automatic rotation is enabled, **When** the administrator configures a custom rotation interval, **Then** the driver refreshes mounted secrets at approximately that interval instead of the previous fixed default.
3. **Given** rotation was previously disabled, **When** the administrator re-enables it, **Then** periodic secret refresh resumes without requiring the workload pods to be recreated.

---

### User Story 2 - Configure Workload Identity Federation Token Audiences (Priority: P1)

Platform engineers and multi-cloud operators want to configure one or more token audiences for the driver, so that workloads can authenticate to external cloud identity providers (e.g. for AWS, Azure, or GCP) using federated identity instead of long-lived static credentials, including scenarios where different workloads on the same cluster federate with different providers simultaneously.

**Why this priority**: Static-credential access to secret providers is a security and operational liability. Federated, per-workload identity is the modern alternative and is the single largest new capability requested — without it, multi-cloud and zero-static-credential deployments are not possible with this driver today.

**Independent Test**: Can be fully tested by configuring a single token audience, confirming the workload receives a token for that audience and successfully authenticates to the identity provider, independent of any secret-rotation configuration.

**Acceptance Scenarios**:

1. **Given** no token audience configuration exists, **When** a platform engineer configures a single token audience, **Then** workloads on the driver receive a token scoped to that audience and can use it to authenticate to the corresponding cloud identity provider.
2. **Given** one token audience is already configured, **When** an operator adds a second, distinct token audience for a different cloud provider, **Then** both audiences are honored simultaneously without either configuration disrupting the other.
3. **Given** operator-managed token audiences are configured, **When** the administrator sets the audience list to empty, **Then** all operator-managed audiences are cleared.
4. **Given** a configured token audience, **When** an optional validity duration is also specified, **Then** issued tokens honor that duration within the platform's supported bounds.

---

### User Story 3 - Preserve Existing Configuration Across Upgrades and Restarts (Priority: P2)

Cluster administrators want their rotation and token audience configuration — including any settings that predate this feature — to persist automatically across operator upgrades and workload/pod restarts, so they never have to manually reapply configuration after routine platform maintenance.

**Why this priority**: This is a trust and safety guarantee rather than a new capability: without it, every upgrade risks silently disrupting already-working workload authentication or rotation behavior, which is a higher-severity concern than either individual capability above but only matters once at least one of them is in use.

**Independent Test**: Can be fully tested by upgrading a cluster that has pre-existing, externally-configured token-audience settings and confirming zero behavior change, independent of whether new configuration is introduced in the same test.

**Acceptance Scenarios**:

1. **Given** a cluster with no rotation or token audience configuration set, **When** the platform is upgraded to a version that supports this feature, **Then** behavior is unchanged from before the upgrade (same default rotation cadence, same absence of token audiences).
2. **Given** a cluster with token audience settings that were configured before this feature existed (outside of the operator's management), **When** the platform is upgraded, **Then** those existing settings are preserved unchanged and workloads continue authenticating without disruption.
3. **Given** an administrator has explicitly opted in to operator-managed token audience configuration, **When** any subsequent configuration change is made, **Then** the operator remains the sole source of truth going forward and does not fall back to externally-managed values.

---

### Edge Cases

- **When** an administrator attempts to set a rotation interval or token validity duration outside the supported range, **then** the configuration is rejected with a clear validation error before it is ever applied.
- **When** an administrator attempts to revert from operator-managed token audience configuration back to externally-managed configuration, **then** the change is rejected — this transition is one-way by design.
- **When** rotation is disabled, **then** the driver does not attempt any periodic provider calls for affected workloads, but still serves secrets fetched at initial mount time.
- **When** a workload's secret provider is temporarily unavailable during a scheduled rotation check, **then** the driver preserves the last successfully fetched secret value and does not disrupt the running workload.
- **When** no token audience configuration and no pre-existing externally-configured audiences exist, **then** no token audience is requested for the workload, preserving current behavior.
- **When** an administrator configures more token audiences than the platform's supported maximum, **then** the configuration is rejected with a clear validation error.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Administrators MUST be able to enable or disable automatic secret rotation for the driver.
- **FR-002**: When rotation is enabled, Administrators MUST be able to configure the interval between rotation checks, within a bounded and validated range.
- **FR-003**: Administrators MUST be able to configure one or more token audiences so that workloads can authenticate to cloud identity providers using federated identity.
- **FR-004**: System MUST support configuring multiple distinct token audiences simultaneously so that different workloads on the same cluster can federate with different cloud providers concurrently.
- **FR-005**: System MUST reject rotation interval, token audience count, and token validity duration values that fall outside their supported bounds, with a clear validation error, before the configuration is applied.
- **FR-006**: System MUST preserve any pre-existing, externally-configured token audience settings unchanged until an administrator explicitly opts in to operator-managed configuration.
- **FR-007**: Once an administrator opts in to operator-managed token audience configuration, System MUST NOT permit reverting to the prior externally-managed state.
- **FR-008**: Administrators MUST be able to clear all operator-managed token audiences by specifying an empty audience list.
- **FR-009**: System MUST apply rotation and token audience configuration changes to running workloads without requiring the administrator to manually restart the driver.
- **FR-010**: When no rotation or token audience configuration is specified, System MUST behave identically to the previously hardcoded default behavior (rotation enabled at the existing fixed interval; any existing token audience settings left unchanged), so that upgrades introduce no behavior change by default.
- **FR-011**: System MUST support at least two simultaneously configured token audiences with independently configurable validity durations.

### Key Entities

- **Secret Rotation Configuration**: Represents whether automatic secret rotation is enabled for the driver and, when enabled, the minimum interval between rotation checks.
- **Token Audience Configuration**: Represents the ownership mode (externally-managed vs. operator-managed) for federated-identity token audiences, and, when operator-managed, the list of token audiences (each with an optional validity duration) issued to workloads for authenticating to cloud identity providers.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An administrator can disable secret rotation and confirm, within one prior rotation cycle, that no further provider calls occur for that workload.
- **SC-002**: An administrator can set a custom rotation interval and observe secrets refreshing at approximately that interval rather than the previous fixed default.
- **SC-003**: A workload can successfully authenticate to a cloud identity provider using a configured token audience with zero manual credential provisioning by the administrator.
- **SC-004**: Administrators can configure at least two distinct token audiences for two different cloud providers on the same cluster, with each functioning correctly and independently.
- **SC-005**: Clusters with pre-existing, externally-configured token audience settings upgrade to a version supporting this feature with zero observable disruption to workloads relying on those settings.
- **SC-006**: Invalid configuration (out-of-range interval, audience count, or validity duration) is rejected at submission time, before any workload is affected, 100% of the time.
- **SC-007**: An administrator can clear all operator-managed token audiences and confirm, within one reconciliation cycle, that no token audience is requested for affected workloads.

## Assumptions

- **A-001**: Cluster administrators (and, for federation-specific configuration, platform engineers) are the personas configuring this feature through the platform's existing operator configuration surface; no new UI or console experience is introduced.
- **A-002**: This feature targets General Availability directly; no separate Tech Preview phase is required.
- **A-003**: Provider-specific credential and trust setup on the cloud identity provider side (e.g., configuring a trust policy for a given audience) is performed by the administrator outside this feature's scope; this feature only configures which token audiences the driver requests.
- **A-004**: Secret rotation control and workload identity federation configuration are delivered together because they are configured through the same configuration surface and reconciled by the same underlying mechanism; each remains independently testable and independently valuable as described in User Stories 1 and 2 (resolves the "Sizing" observation from Stage 0 validation).
- **A-005**: Guidance on choosing a safe, provider-friendly rotation interval will be provided via documentation rather than an additional enforced platform floor beyond the basic bounds validation in FR-005 (resolves the "Ambiguity" observation from Stage 0 validation regarding rotation-interval risk mitigation).
- **A-006**: This feature is scoped entirely to the Secrets Store CSI Driver Operator and the platform configuration API it already exposes; no changes to the underlying driver binary, provider plugins, or other unrelated systems are required (resolves the "Impacted Repositories" observation from Stage 0 validation).
- **A-007**: This feature is supported starting with the target platform release identified in the source ticket; earlier releases are out of scope and unaffected.
- **A-008**: "Persisting configuration across upgrades and restarts" (User Story 3) means configuration survives routine platform lifecycle events without administrator re-intervention — it does not imply any new backup/restore capability beyond the platform's existing configuration storage guarantees.
