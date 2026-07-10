package operator

import (
	"fmt"
	"time"

	opv1 "github.com/openshift/api/operator/v1"
	storagev1 "k8s.io/api/storage/v1"
)

const defaultSecretRotationPollInterval = 2 * time.Minute

type secretsStoreDriverConfig struct {
	rotationEnabled      bool
	rotationPollInterval time.Duration
	requiresRepublish    bool
	tokenRequests        []storagev1.TokenRequest
}

func effectiveSecretsStoreDriverConfig(
	clusterDriver *opv1.ClusterCSIDriver,
	existingTokenRequests []storagev1.TokenRequest,
) (secretsStoreDriverConfig, error) {
	rotationEnabled, rotationPollInterval, err := effectiveSecretRotation(clusterDriver)
	if err != nil {
		return secretsStoreDriverConfig{}, err
	}

	tokenRequests, err := effectiveTokenRequests(clusterDriver, existingTokenRequests)
	if err != nil {
		return secretsStoreDriverConfig{}, err
	}

	return secretsStoreDriverConfig{
		rotationEnabled:      rotationEnabled,
		rotationPollInterval: rotationPollInterval,
		requiresRepublish:    rotationEnabled,
		tokenRequests:        tokenRequests,
	}, nil
}

func effectiveSecretRotation(clusterDriver *opv1.ClusterCSIDriver) (bool, time.Duration, error) {
	if !hasSecretsStoreDriverConfig(clusterDriver) {
		return true, defaultSecretRotationPollInterval, nil
	}

	switch clusterDriver.Spec.DriverConfig.SecretsStore.SecretRotation.Type {
	case "":
		return true, defaultSecretRotationPollInterval, nil
	case opv1.SecretRotationNone:
		return false, defaultSecretRotationPollInterval, nil
	case opv1.SecretRotationCustom:
		intervalSeconds := clusterDriver.Spec.DriverConfig.SecretsStore.SecretRotation.Custom.RotationPollIntervalSeconds
		if intervalSeconds == 0 {
			return true, defaultSecretRotationPollInterval, nil
		}

		return true, time.Duration(intervalSeconds) * time.Second, nil
	default:
		return false, 0, fmt.Errorf(
			"unsupported secret rotation type %q",
			clusterDriver.Spec.DriverConfig.SecretsStore.SecretRotation.Type,
		)
	}
}

func effectiveTokenRequests(
	clusterDriver *opv1.ClusterCSIDriver,
	existingTokenRequests []storagev1.TokenRequest,
) ([]storagev1.TokenRequest, error) {
	if !hasSecretsStoreDriverConfig(clusterDriver) {
		return cloneTokenRequests(existingTokenRequests), nil
	}

	switch clusterDriver.Spec.DriverConfig.SecretsStore.TokenRequests.Type {
	case "", opv1.TokenRequestsUnmanaged:
		return cloneTokenRequests(existingTokenRequests), nil
	case opv1.TokenRequestsManaged:
		return managedTokenRequests(
			clusterDriver.Spec.DriverConfig.SecretsStore.TokenRequests.Managed.Audiences,
		)
	default:
		return nil, fmt.Errorf(
			"unsupported token requests type %q",
			clusterDriver.Spec.DriverConfig.SecretsStore.TokenRequests.Type,
		)
	}
}

func managedTokenRequests(
	audiences *[]opv1.SecretsStoreTokenRequest,
) ([]storagev1.TokenRequest, error) {
	if audiences == nil {
		return nil, nil
	}

	tokenRequests := make([]storagev1.TokenRequest, 0, len(*audiences))
	for _, audience := range *audiences {
		if audience.Audience == nil {
			return nil, fmt.Errorf("managed token request audience must not be nil")
		}

		tokenRequest := storagev1.TokenRequest{
			Audience: *audience.Audience,
		}
		if audience.ExpirationSeconds != 0 {
			expirationSeconds := int64(audience.ExpirationSeconds)
			tokenRequest.ExpirationSeconds = &expirationSeconds
		}

		tokenRequests = append(tokenRequests, tokenRequest)
	}

	return tokenRequests, nil
}

func hasSecretsStoreDriverConfig(clusterDriver *opv1.ClusterCSIDriver) bool {
	return clusterDriver != nil && clusterDriver.Spec.DriverConfig.DriverType == opv1.SecretsStoreDriverType
}

func cloneTokenRequests(tokenRequests []storagev1.TokenRequest) []storagev1.TokenRequest {
	if len(tokenRequests) == 0 {
		return nil
	}

	cloned := make([]storagev1.TokenRequest, 0, len(tokenRequests))
	for _, tokenRequest := range tokenRequests {
		clonedTokenRequest := storagev1.TokenRequest{
			Audience: tokenRequest.Audience,
		}
		if tokenRequest.ExpirationSeconds != nil {
			expirationSeconds := *tokenRequest.ExpirationSeconds
			clonedTokenRequest.ExpirationSeconds = &expirationSeconds
		}

		cloned = append(cloned, clonedTokenRequest)
	}

	return cloned
}
