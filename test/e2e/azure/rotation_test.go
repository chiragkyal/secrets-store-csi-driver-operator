package azure

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	opv1 "github.com/openshift/api/operator/v1"
	"k8s.io/utils/ptr"
)

const (
	// rotationMinimumRefreshAge is a short interval (seconds) so this suite
	// doesn't have to wait out the 2-minute pre-feature default poll
	// interval to observe a rotation.
	rotationMinimumRefreshAge = 30
	// rotationPollIntervalArgPrefix mirrors the csi-driver container's flag
	// prefix from pkg/operator/rotation_daemonset_hook.go, duplicated here
	// for a debug log line only (test/e2e/azure cannot import
	// pkg/operator's _test.go-adjacent unexported constants).
	rotationPollIntervalArgPrefix = "--rotation-poll-interval="
)

// rotateAndAssert configures a short secretRotation.minimumRefreshAge
// (while re-specifying the already-Managed tokenRequests configuration,
// since driverConfig.secretsStore is replaced wholesale on every update and
// tokenRequests.type: Managed cannot be implicitly dropped once set),
// updates the real Key Vault secret's value, and polls the pod's mounted
// file until it reflects the new value within an expected window.
// currentValue is updated in place so the caller and AfterAll cleanup
// always have the latest value on record.
func rotateAndAssert(namespace, podName, keyVaultName, secretName string, currentValue *string) {
	By("configuring a short secretRotation.minimumRefreshAge, preserving the Managed tokenRequests audience")
	setSecretsStoreConfig(opv1.SecretsStoreCSIDriverConfigSpec{
		SecretRotation: opv1.SecretsStoreSecretRotation{
			Type: opv1.SecretRotationCustom,
			Custom: opv1.CustomSecretRotation{
				MinimumRefreshAge: rotationMinimumRefreshAge,
			},
		},
		TokenRequests: opv1.SecretsStoreTokenRequests{
			Type: opv1.TokenRequestsManaged,
			Managed: opv1.ManagedTokenRequests{
				Audiences: &[]opv1.SecretsStoreTokenRequest{
					{Audience: ptr.To(azureWIFAudience)},
				},
			},
		},
	})

	pollIntervalArg, err := daemonSetArgValue(rotationPollIntervalArgPrefix)
	Expect(err).NotTo(HaveOccurred())
	GinkgoWriter.Printf("DaemonSet %s=%s\n", rotationPollIntervalArgPrefix, pollIntervalArg)
	waitForDaemonSetRollout()

	By("updating the real Key Vault secret's value")
	newValue := *currentValue + "-rotated"
	Expect(azKeyVaultSecretSet(keyVaultName, secretName, newValue)).To(Succeed())
	*currentValue = newValue

	By("waiting for the mounted file to reflect the new Key Vault secret value")
	Eventually(func() (string, error) {
		return readMountedSecret(namespace, podName, secretName)
	}, 5*time.Minute, 5*time.Second).Should(Equal(newValue), "mounted secret did not rotate to the new Key Vault value within the expected window")
}
