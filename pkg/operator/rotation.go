package operator

import (
	"time"

	opv1 "github.com/openshift/api/operator/v1"
)

const (
	// defaultRotationEnabled and defaultRotationPollInterval match the
	// values hardcoded in assets/node.yaml prior to this feature
	// (--enable-secret-rotation=true, --rotation-poll-interval=2m) and are
	// returned whenever the administrator has expressed no opinion, so
	// existing clusters see no change in behavior (FR-003, FR-012).
	defaultRotationEnabled      = true
	defaultRotationPollInterval = 2 * time.Minute

	// defaultCustomRefreshInterval is the operator-chosen default applied
	// when secretRotation.type is Custom but custom.minimumRefreshAge is
	// omitted, matching the field's documented "reasonable default...
	// currently 120s" in the upstream API.
	defaultCustomRefreshInterval = 120 * time.Second
)

// getSecretRotationConfig computes the effective secret-rotation enable flag
// and poll interval for the Secrets Store CSI driver from a
// ClusterCSIDriver's driverConfig (FR-001, FR-002, FR-003, FR-011).
//
// Bounds validation (1s-~1yr on minimumRefreshAge) is intentionally NOT
// performed here: it is enforced by the upstream CRD schema, so any value
// reaching this function has already been admitted by the API server.
//
// Nil-safety matrix (driverType, secretsStore, and secretRotation are all Go
// value types with `omitzero`/`omitempty` JSON semantics, not pointers --
// "omitted" means the zero value):
//   - driverType != SecretsStore (including the unset/empty value): defaults
//   - driverType == SecretsStore but secretsStore is the zero value: defaults
//   - secretRotation is the zero value (type == ""): defaults
//   - secretRotation.type == None: rotation disabled
//   - secretRotation.type == Custom, custom.minimumRefreshAge > 0: that interval
//   - secretRotation.type == Custom, custom.minimumRefreshAge == 0 (omitted): default interval
func getSecretRotationConfig(driverConfig opv1.CSIDriverConfigSpec) (enabled bool, interval time.Duration) {
	if driverConfig.DriverType != opv1.SecretsStoreDriverType {
		return defaultRotationEnabled, defaultRotationPollInterval
	}

	rotation := driverConfig.SecretsStore.SecretRotation
	switch rotation.Type {
	case opv1.SecretRotationNone:
		return false, defaultRotationPollInterval
	case opv1.SecretRotationCustom:
		if rotation.Custom.MinimumRefreshAge > 0 {
			return true, time.Duration(rotation.Custom.MinimumRefreshAge) * time.Second
		}
		return true, defaultCustomRefreshInterval
	default:
		// Zero value (type == "") or any unrecognized future value: no
		// opinion expressed, keep the defaults.
		return defaultRotationEnabled, defaultRotationPollInterval
	}
}
