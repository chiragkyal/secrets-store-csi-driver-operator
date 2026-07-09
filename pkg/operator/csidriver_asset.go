package operator

import (
	opv1 "github.com/openshift/api/operator/v1"
	storagev1 "k8s.io/api/storage/v1"
)

// getRequiresRepublish returns the desired CSIDriver.spec.requiresRepublish
// value. It mirrors secretRotation's effective enable state (FR-011): false
// when rotation is disabled (secretRotation.type == None), true otherwise
// (secretRotation omitted, or type == Custom) -- see specs.md Edge Cases
// ("requiresRepublish mirrors secretRotation.type"). Reuses
// getSecretRotationConfig (pkg/operator/rotation.go) rather than
// duplicating its nil-path logic, so the rotation flag and requiresRepublish
// can never disagree.
func getRequiresRepublish(driverConfig opv1.CSIDriverConfigSpec) *bool {
	enabled, _ := getSecretRotationConfig(driverConfig)
	return &enabled
}

// getTokenRequests computes the desired CSIDriver.spec.tokenRequests from
// driverConfig, given the tokenRequests currently set on the live CSIDriver
// object (existingTokenRequests -- nil or empty if the CSIDriver does not
// exist yet, or has none). Implements the full preservation matrix from
// openspec/inputs/ep.md's Test Plan (FR-004, FR-005, FR-007):
//
//   - driverType != SecretsStore (including the unset/empty value): preserve existing
//   - driverType == SecretsStore, secretsStore is the zero value: preserve existing
//   - tokenRequests is the zero value (type == "", i.e. omitted): preserve existing
//   - type == Unmanaged: preserve existing (the managed field is not used)
//   - type == Managed, managed.audiences == nil: the upstream CRD schema
//     requires managed to be set whenever type is Managed (FR-006's
//     discriminated-union rule), so this path should not occur on an
//     already-admitted object; defensively preserve existing rather than guess
//   - type == Managed, managed.audiences is a non-nil pointer to a slice:
//     return exactly that slice mapped to []storagev1.TokenRequest --
//     INCLUDING the empty-slice case, which explicitly clears all
//     tokenRequests (FR-007). This is why Audiences is a *[]T rather than a
//     plain []T upstream: nil means "omitted" (preserve), a non-nil pointer
//     to an empty slice means "explicitly cleared" (return empty, not nil).
//
// The upstream CEL rule enforcing "tokenRequests.type cannot revert from
// Managed" (FR-006) is intentionally NOT re-implemented here -- this
// function only reads already-validated objects and must not duplicate that
// logic (repo-assessment.md §11 risk #4).
func getTokenRequests(driverConfig opv1.CSIDriverConfigSpec, existingTokenRequests []storagev1.TokenRequest) []storagev1.TokenRequest {
	if driverConfig.DriverType != opv1.SecretsStoreDriverType {
		return existingTokenRequests
	}

	tokenRequests := driverConfig.SecretsStore.TokenRequests
	switch tokenRequests.Type {
	case opv1.TokenRequestsManaged:
		if tokenRequests.Managed.Audiences == nil {
			return existingTokenRequests
		}
		audiences := *tokenRequests.Managed.Audiences
		result := make([]storagev1.TokenRequest, 0, len(audiences))
		for _, audience := range audiences {
			tr := storagev1.TokenRequest{Audience: stringValue(audience.Audience)}
			if audience.ExpirationSeconds != 0 {
				expirationSeconds := int64(audience.ExpirationSeconds)
				tr.ExpirationSeconds = &expirationSeconds
			}
			result = append(result, tr)
		}
		return result
	case opv1.TokenRequestsUnmanaged:
		return existingTokenRequests
	default:
		// Zero value (type == ""): omitted, preserve existing.
		return existingTokenRequests
	}
}

// stringValue safely dereferences s, returning "" if s is nil.
func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
