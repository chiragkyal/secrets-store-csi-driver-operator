package azure

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	opv1 "github.com/openshift/api/operator/v1"
	"k8s.io/utils/ptr"
)

// Describe("Azure Workload Identity Federation") is the real, end-to-end
// verification that driverConfig.secretsStore.tokenRequests (this
// operator's new feature) produces working WIF: kubelet issues a
// ServiceAccount token for the configured audience, the real Azure provider
// exchanges it via a federated identity credential, and the actual Key
// Vault secret is fetched and mounted -- with no manual `oc patch csidriver`
// step required.
//
// Marked Ordered with BeforeAll/AfterAll: the Key Vault, managed identity,
// federated credential, provider install, and pod are expensive, shared
// setup for every spec in this container (the initial-mount assertion and,
// in a nested Context, the real-rotation assertion), not per-spec fixtures.
var _ = Describe("Azure Workload Identity Federation", Ordered, func() {
	var (
		namespace    string
		keyVaultName string
		identityName string
		clientID     string
		podName      string
		secretName   string
		secretValue  string

		spcName = "azure"
	)

	BeforeAll(func() {
		namespace = "sscsi-e2e-wif-" + runSuffix
		keyVaultName = "sscsi-e2e-vault-" + runSuffix
		identityName = "sscsi-e2e-uami-" + runSuffix
		podName = "sscsi-e2e-wif-pod"
		secretName = "sscsi-e2e-secret"
		secretValue = "initial-value-" + runSuffix

		By("creating a privileged test namespace")
		Expect(createPrivilegedNamespace(namespace)).To(Succeed())

		By("creating a Key Vault and secret")
		Expect(azKeyVaultCreate(keyVaultName, resourceGroup, location)).To(Succeed())
		Expect(azKeyVaultSecretSet(keyVaultName, secretName, secretValue)).To(Succeed())

		By("creating a user-assigned managed identity")
		Expect(azIdentityCreate(identityName, resourceGroup)).To(Succeed())
		var err error
		clientID, err = azIdentityClientID(identityName, resourceGroup)
		Expect(err).NotTo(HaveOccurred())

		By("creating a federated identity credential for the test namespace's default ServiceAccount")
		subject := fmt.Sprintf("system:serviceaccount:%s:default", namespace)
		Expect(azFederatedCredentialCreate("sscsi-e2e-fed-cred-"+runSuffix, identityName, resourceGroup, oidcIssuer, subject, azureWIFAudience)).To(Succeed())

		By("granting the identity access to read the Key Vault secret")
		principalID, err := azIdentityPrincipalID(identityName, resourceGroup)
		Expect(err).NotTo(HaveOccurred())
		Expect(azKeyVaultSetPolicy(keyVaultName, principalID)).To(Succeed())

		By("installing the real Azure provider")
		Expect(installAzureProvider()).To(Succeed())
		waitAzureProviderReady()

		By("configuring driverConfig.secretsStore.tokenRequests to Managed with the Azure WIF audience (replaces the manual CSIDriver patch)")
		setSecretsStoreConfig(opv1.SecretsStoreCSIDriverConfigSpec{
			TokenRequests: opv1.SecretsStoreTokenRequests{
				Type: opv1.TokenRequestsManaged,
				Managed: opv1.ManagedTokenRequests{
					Audiences: &[]opv1.SecretsStoreTokenRequest{
						{Audience: ptr.To(azureWIFAudience)},
					},
				},
			},
		})
		waitForTokenRequestAudiences(azureWIFAudience)
		waitForDaemonSetRollout()

		By("deploying the SecretProviderClass and a pod mounting it via a CSI inline volume")
		Expect(deploySecretProviderClass(namespace, spcName, clientID, keyVaultName, tenantID, secretName)).To(Succeed())
		Expect(createInlineVolumePod(namespace, podName, spcName)).To(Succeed())
		waitPodReady(namespace, podName)
	})

	AfterAll(func() {
		By("tearing down the Azure WIF test fixtures")
		if err := deleteNamespace(namespace); err != nil {
			GinkgoWriter.Printf("unable to delete namespace %q: %v\n", namespace, err)
		}
		if err := uninstallAzureProvider(); err != nil {
			GinkgoWriter.Printf("unable to uninstall Azure provider: %v\n", err)
		}
		if err := azKeyVaultDelete(keyVaultName, resourceGroup); err != nil {
			GinkgoWriter.Printf("unable to delete Key Vault %q: %v\n", keyVaultName, err)
		}
		if err := azIdentityDelete(identityName, resourceGroup); err != nil {
			GinkgoWriter.Printf("unable to delete identity %q: %v\n", identityName, err)
		}
	})

	It("mounts the real Key Vault secret via WIF configured through driverConfig.secretsStore.tokenRequests", func() {
		got, err := readMountedSecret(namespace, podName, secretName)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(secretValue))
	})

	Context("secret rotation against the real Key Vault", func() {
		It("reflects an updated Key Vault secret value in the mounted file within minimumRefreshAge", func() {
			rotateAndAssert(namespace, podName, keyVaultName, secretName, &secretValue)
		})
	})
})
