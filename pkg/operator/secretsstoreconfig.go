package operator

import (
	opv1 "github.com/openshift/api/operator/v1"
)

// Defaults mirror the hardcoded values in assets/node.yaml so that clusters with no
// driverConfig.secretsStore configuration see no behavior change (specs.md FR-010).
const (
	defaultRotationEnabled             = true
	defaultRotationPollIntervalSeconds = 120 // matches "--rotation-poll-interval=2m" in assets/node.yaml
)

// ResolvedRotationConfig is the fully-resolved secret rotation behavior for the driver.
type ResolvedRotationConfig struct {
	// Enabled mirrors what the "--enable-secret-rotation" DaemonSet arg and the
	// CSIDriver's "requiresRepublish" field should be set to.
	Enabled bool
	// RotationPollIntervalSeconds is the interval the driver should use between
	// rotation attempts, in seconds.
	RotationPollIntervalSeconds int32
}

// ResolvedTokenRequestsConfig is the fully-resolved workload identity federation
// token-audience configuration.
type ResolvedTokenRequestsConfig struct {
	// Managed is true when the operator should be the sole source of truth for
	// CSIDriver.spec.tokenRequests. When false, the caller MUST preserve whatever
	// tokenRequests already exist on the live CSIDriver object instead of overwriting
	// them (specs.md FR-006, User Story 3).
	Managed bool
	// Audiences is the desired audience list when Managed is true. An empty
	// (non-nil) slice means "clear all managed audiences" (specs.md FR-008).
	// This field has no meaning when Managed is false.
	Audiences []opv1.SecretsStoreTokenRequest
}

// ResolveSecretsStoreConfig extracts the SecretsStore-specific rotation and
// token-request configuration from a ClusterCSIDriver spec, applying built-in
// defaults whenever the administrator has not opted in to a driver-specific
// setting. This is the single shared read path for the CSIDriver AssetFunc and the
// DaemonSet rotation hook — both MUST call this instead of re-implementing the
// nil-safety/default-resolution logic themselves.
//
// Because every level of CSIDriverConfigSpec.SecretsStore is a value type (not a
// pointer) in the vendored API, there is no nil-pointer risk when traversing it;
// resolution instead branches on the string-typed discriminator fields
// (DriverType, SecretRotation.Type, TokenRequests.Type) against their zero value.
func ResolveSecretsStoreConfig(spec *opv1.ClusterCSIDriverSpec) (ResolvedRotationConfig, ResolvedTokenRequestsConfig) {
	rotation := ResolvedRotationConfig{
		Enabled:                     defaultRotationEnabled,
		RotationPollIntervalSeconds: defaultRotationPollIntervalSeconds,
	}
	tokenRequests := ResolvedTokenRequestsConfig{
		Managed:   false,
		Audiences: nil,
	}

	if spec == nil {
		return rotation, tokenRequests
	}
	if spec.DriverConfig.DriverType != opv1.SecretsStoreDriverType {
		return rotation, tokenRequests
	}

	secretsStore := spec.DriverConfig.SecretsStore

	switch secretsStore.SecretRotation.Type {
	case opv1.SecretRotationNone:
		rotation.Enabled = false
	case opv1.SecretRotationCustom:
		rotation.Enabled = true
		if interval := secretsStore.SecretRotation.Custom.RotationPollIntervalSeconds; interval > 0 {
			rotation.RotationPollIntervalSeconds = interval
		}
	}
	// When Type is omitted (""), the defaults set above are left untouched.

	if secretsStore.TokenRequests.Type == opv1.TokenRequestsManaged {
		tokenRequests.Managed = true
		if audiences := secretsStore.TokenRequests.Managed.Audiences; audiences != nil {
			tokenRequests.Audiences = *audiences
		} else {
			tokenRequests.Audiences = []opv1.SecretsStoreTokenRequest{}
		}
	}
	// When Type is "Unmanaged" or omitted, Managed stays false — the caller MUST
	// preserve any tokenRequests already present on the live CSIDriver object.

	return rotation, tokenRequests
}
