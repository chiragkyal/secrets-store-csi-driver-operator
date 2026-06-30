# Feature Specification: Configurable Secret Rotation and Workload Identity Federation for the Secrets Store CSI Driver

**Feature Branch**: `sscsi-254-configurable-rotation-and-wif`

**Created**: 2026-06-30

**Status**: Draft

**Jira**: SSCSI-254 | **EP**: [openshift/enhancements#2012](https://github.com/openshift/enhancements/pull/2012)

---

## User Scenarios & Testing

### User Story 1 — Disable Automatic Secret Rotation (Priority: P1)

As a cluster administrator managing workloads that use static, long-lived secrets, I want to disable automatic secret rotation so that the driver does not make unnecessary periodic calls to external secret providers, reducing API costs and rate-limit pressure.

**Why this priority**: Rotation is enabled by default and cannot be turned off today. Customers with high secret counts (e.g., 200+ secrets) incur unnecessary provider API costs and transaction overhead.

**Independent Test**: Deploy a workload that mounts a secret. Disable rotation via the operator API. Observe that the provider is contacted only at initial pod mount time and not again during the pod's lifetime.

**Acceptance Scenarios**:

1. **Given** a cluster with the Secrets Store CSI Driver installed and rotation enabled, **When** an administrator sets rotation type to "disabled" via the operator API, **Then** the CSI driver stops making periodic calls to the secret provider and secrets are only fetched at initial pod mount time.
2. **Given** rotation is disabled, **When** an administrator re-enables rotation with a custom interval, **Then** the CSI driver resumes periodic secret refresh at the configured interval.
3. **Given** rotation is disabled, **When** a pod mounts a new volume, **Then** the secret is still fetched from the provider successfully on first mount.

---

### User Story 2 — Configure Workload Identity Federation Token Audiences (Priority: P1)

As a platform engineer, I want to configure service account token audiences on the CSI driver registration object through the operator API, so that pods can use workload identity federation (WIF) to authenticate with cloud secret providers (AWS STS, Azure AD, GCP IAM) without long-lived credentials.

**Why this priority**: WIF is the cloud-native credential model for all major providers. The operator currently provides no way to configure this; users are forced to manually patch a managed object, which is unsupported and fragile across upgrades.

**Independent Test**: Configure one token audience via the operator API. Deploy a pod that mounts a secret from the configured cloud provider using WIF. Confirm the pod successfully authenticates using a projected service account token (no static credentials).

**Acceptance Scenarios**:

1. **Given** no token audiences are configured, **When** an administrator sets a token audience (e.g., AWS STS) via the operator API, **Then** the CSI driver registration object is updated to include that audience and kubelet provides a projected service account token to the driver on each mount call.
2. **Given** token audiences are operator-managed, **When** an administrator updates the audience list, **Then** the CSI driver registration object is updated to reflect the new list.
3. **Given** token audiences are operator-managed with an empty list, **When** reconciliation occurs, **Then** all token audiences are removed from the CSI driver registration object.
4. **Given** an administrator has set the management policy to "operator-managed", **When** they attempt to revert to "unmanaged" mode, **Then** the API rejects the change with a clear validation error (one-way transition).

---

### User Story 3 — Configure Rotation Polling Interval (Priority: P2)

As a cluster administrator, I want to tune the minimum time between secret rotation attempts so that I can balance secret freshness against external provider API load for my cluster's specific workload profile.

**Why this priority**: The 2-minute hardcoded default is unsuitable for clusters with many secrets or rate-limited providers. Administrators need control over this trade-off.

**Independent Test**: Set a custom interval (e.g., 5 minutes) via the operator API. Observe via the driver's behavior that it contacts the provider no more frequently than the configured interval, even when kubelet calls the driver more often.

**Acceptance Scenarios**:

1. **Given** a cluster with rotation enabled at the default interval, **When** an administrator sets a custom minimum refresh age via the operator API, **Then** the CSI driver respects the new interval and does not contact the secret provider more frequently than specified.
2. **Given** a custom interval is set, **When** the interval has not elapsed since the last provider call, **Then** the driver returns success immediately without contacting the provider.
3. **Given** an administrator sets the interval to a value outside the valid range, **Then** the API rejects the configuration with a clear validation error before it is persisted.

---

### User Story 4 — Multi-Cloud Workload Identity Federation (Priority: P2)

As a multi-cloud operator, I want to configure multiple token audiences on a single Secrets Store CSI Driver instance so that different workloads on the same cluster can federate identity with different cloud providers simultaneously.

**Why this priority**: Enterprise clusters commonly span multiple clouds (e.g., AWS for compute + Azure Key Vault for secrets). Today this is impossible without manual and unsupported workarounds.

**Independent Test**: Configure two token audiences (e.g., AWS + Azure) via the operator API. Deploy two workloads, each fetching secrets from a different cloud provider using WIF. Both must succeed simultaneously.

**Acceptance Scenarios**:

1. **Given** multiple token audiences are configured, **When** a pod mounts a volume, **Then** kubelet provides tokens for all configured audiences to the driver in each mount call.
2. **Given** an audience is added to an existing list, **When** reconciliation occurs, **Then** the new audience is appended without disrupting existing audiences.

---

### User Story 5 — Safe Migration of Existing Manual Token Configurations (Priority: P2)

As a cluster administrator who has manually configured token audiences directly on the CSI driver registration object (e.g., for Azure WIF before this feature existed), I want my existing configuration to be preserved when I upgrade the operator, so that my workloads are not disrupted.

**Why this priority**: Upgrading operators must never silently break existing workloads. At least one production cluster is known to have manually patched token audiences.

**Independent Test**: Manually configure a token audience on the CSI driver registration object. Upgrade the operator to a version that includes this feature. Confirm the existing audience is still present and WIF continues to work without any administrator action.

**Acceptance Scenarios**:

1. **Given** a cluster with manually-patched token audiences on the CSI driver registration object, **When** the operator is upgraded, **Then** the existing token audiences are preserved and WIF-dependent workloads continue to function without interruption.
2. **Given** a cluster upgraded with no token configuration in the operator API, **When** the administrator is ready to adopt operator-managed token audiences, **Then** they can set the management policy to "operator-managed" and provide the desired audiences, at which point the operator takes full ownership.

---

### User Story 6 — Persistent Configuration Across Upgrades and Restarts (Priority: P3)

As a cluster administrator, I want my rotation and token configuration to persist across operator upgrades and pod restarts so that I do not need to re-apply settings after each maintenance event.

**Why this priority**: Configuration persistence is a basic contract for operator-managed settings. This is implicitly guaranteed by the operator API design but must be explicitly validated.

**Independent Test**: Configure rotation and token settings. Restart the operator pod. Confirm settings are still active and the driver behavior is unchanged.

**Acceptance Scenarios**:

1. **Given** rotation and token audiences are configured via the operator API, **When** the operator pod restarts, **Then** the configuration is re-applied from the stored operator resource and driver behavior is unchanged.

---

### Edge Cases

- **When** the rotation interval is set below the kubelet's volume sync frequency (approximately 1 minute), **then** the effective rotation cadence is bounded by the kubelet sync frequency; no additional provider calls occur beyond the sync rate.
- **When** the CSI driver registration object is deleted externally while rotation or token configuration is active, **then** the operator recreates it with the current desired configuration within the next reconcile cycle.
- **When** an administrator attempts to set token audience management policy to "operator-managed" without providing an audience list, **then** the API rejects the configuration with a clear validation error.
- **When** no rotation or token configuration is specified in the operator API (upgrade scenario), **then** the operator applies built-in defaults that exactly match pre-upgrade behavior: rotation enabled at 2-minute interval, existing token audiences on the CSI driver registration object preserved.
- **When** rotation is disabled and then re-enabled with a custom interval, **then** the driver resumes periodic rotation at the new interval without requiring a pod restart.
- **When** a token audience expiration is set outside the valid range, **then** the API rejects the configuration with a clear error before it is persisted.

---

## Requirements

### Functional Requirements

- **FR-001**: The operator MUST allow cluster administrators to disable automatic secret rotation via the operator API without affecting any other operator functionality.
- **FR-002**: The operator MUST allow cluster administrators to set a minimum refresh age (rotation interval) for secret rotation attempts via the operator API.
- **FR-003**: The operator MUST apply rotation settings to the CSI driver process configuration during the next reconcile after the operator API is changed.
- **FR-004**: The operator MUST apply rotation settings to the CSI driver registration object (enabling or disabling kubelet's periodic remount behavior) during the next reconcile.
- **FR-005**: The operator MUST allow cluster administrators to configure service account token audiences for workload identity federation via the operator API.
- **FR-006**: The operator MUST allow configuration of multiple token audiences simultaneously on a single driver instance, with optional per-audience expiration duration.
- **FR-007**: When token audience management is not explicitly enabled by the administrator, the operator MUST preserve any existing token audiences already present on the CSI driver registration object.
- **FR-008**: Once an administrator enables operator-managed token audiences, that management policy MUST be irreversible — the operator rejects any attempt to revert to unmanaged mode.
- **FR-009**: When no rotation or token configuration is specified in the operator API, the operator MUST apply built-in defaults that preserve the exact behavior present before this feature was introduced.
- **FR-010**: The operator MUST validate rotation interval values against a defined range and reject out-of-range values at the API admission layer.
- **FR-011**: The operator MUST validate token audience expiration values against a defined range and reject out-of-range values at the API admission layer.
- **FR-012**: The operator MUST report a degraded status condition when it fails to apply rotation or token configuration to the CSI driver registration object or process configuration.

### Key Entities

- **Secret Rotation Configuration**: Represents the desired rotation behavior — type (disabled or custom), and when custom, the minimum time in seconds between rotation attempts.
- **Token Request Configuration**: Represents the desired token audience management — management policy (operator-managed or unmanaged), and when managed, the list of token audiences with optional expiration.
- **Token Audience**: A single service account token audience entry consisting of an audience identifier string and an optional token validity duration.

---

## Success Criteria

### Measurable Outcomes

- **SC-001**: After disabling rotation via the operator API, the CSI driver stops contacting external secret providers periodically; secrets are only fetched at initial pod mount time. Observable via provider-side API call logs and driver metrics.
- **SC-002**: After configuring a token audience via the operator API, pods using workload identity federation successfully authenticate with the configured cloud provider without static credentials. Observable via successful secret mount and provider audit logs.
- **SC-003**: After configuring a custom rotation interval, the CSI driver contacts the secret provider no more frequently than the specified interval during a pod's lifetime. Observable via driver metrics and provider API call timing.
- **SC-004**: After configuring multiple token audiences, workloads targeting different cloud providers on the same cluster all succeed in fetching secrets via workload identity federation simultaneously.
- **SC-005**: When an invalid configuration is submitted (out-of-range interval, conflicting union fields, attempt to revert managed policy), the API rejects it with a clear, actionable error message before any change is persisted.
- **SC-006**: Clusters upgrading from a previous operator version with no rotation or token configuration in the operator API observe no change in behavior — existing token audiences on the CSI driver registration object are preserved and rotation continues at the previous default interval.
- **SC-007**: When an administrator explicitly adopts operator-managed token audiences after upgrade, the CSI driver registration object is updated to match within one reconcile cycle and WIF-dependent workloads continue to function without interruption.

---

## Assumptions

- **A-001**: This feature targets General Availability (GA) directly; no Tech Preview phase is required.
- **A-002**: The operator API extension adds new optional fields to the existing cluster-scoped operator resource for the CSI driver; no new resource kinds are introduced.
- **A-003**: Once an administrator sets token audience management to operator-managed, that policy cannot be reverted — this is by design to prevent accidental disruption to WIF-dependent workloads.
- **A-004**: The minimum effective rotation cadence is bounded by kubelet's volume sync frequency (approximately 1 minute by default); setting a rotation interval below this value has no additional effect on actual provider call frequency.
- **A-005**: Provider plugins (AWS Secrets Manager, Azure Key Vault, GCP Secret Manager, HashiCorp Vault, etc.) are installed and configured separately; this feature does not cover provider-specific setup.
- **A-006**: This feature does not support MicroShift; the Secrets Store CSI Driver is not available on that platform.
- **A-007**: The operator API extension depends on an upstream API change (in the platform API repository) that must land before or concurrently with the operator implementation; this is a cross-repository dependency that must be tracked.
- **A-008**: Clusters with manually-patched token audiences on the CSI driver registration object are assumed to have made those changes intentionally; the upgrade path preserves them by default (Unmanaged policy).
- **A-009**: The operator does not auto-detect the cluster's cloud provider; administrators must explicitly configure the audience strings appropriate for their environment (e.g., `sts.amazonaws.com` for AWS, `api://AzureADTokenExchange` for Azure).
- **A-010**: Existing operator permissions are sufficient to manage the new fields on the CSI driver registration object; no additional RBAC is required for the operator service account. *(To be confirmed during implementation.)*
- **A-011**: The operator distribution metadata (OLM bundle) may require minor updates to describe the new configurable fields; this is a packaging concern tracked separately from the functional implementation.
