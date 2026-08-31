package azure

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	opv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	// batsSecretName and batsSecretValue mirror azure.bats defaults.
	batsSecretName  = "secret1"
	batsSecretValue = "test"
	syncLabelValue  = "test"

	inlinePodName   = "secrets-store-inline-crd"
	multiplePodName = "secrets-store-inline-multiple-crd"
	spcAzure        = "azure"
	spcAzureSync    = "azure-sync"
	deploymentOne   = "busybox-deployment"
	deploymentTwo   = "busybox-deployment-two"
	rotationPodName = "sscsi-e2e-rotation-pod"
)

// Describe("Azure provider e2e") ports the upstream azure.bats scenarios to
// this operator repo, exercising the real Azure provider against a live
// OpenShift-on-Azure cluster while configuring tokenRequests through
// driverConfig.secretsStore (instead of a manual oc patch csidriver).
//
// Specs are Ordered to preserve azure.bats's sequential assumptions.
var _ = Describe("Azure provider e2e", Ordered, func() {
	var (
		mainNamespace     string
		testNS            string
		negativeTestNS    string
		keyVaultName      string
		identityName      string
		clientID          string
		rotationSecretVal string
	)

	BeforeAll(func() {
		mainNamespace = "sscsi-e2e-main-" + runSuffix
		testNS = "sscsi-e2e-test-ns-" + runSuffix
		negativeTestNS = "sscsi-e2e-negative-" + runSuffix
		keyVaultName = "sscsi-e2e-vault-" + runSuffix
		identityName = "sscsi-e2e-uami-" + runSuffix
		rotationSecretVal = batsSecretValue

		By("creating privileged test namespaces")
		for _, ns := range []string{mainNamespace, testNS, negativeTestNS} {
			Expect(createPrivilegedNamespace(ns)).To(Succeed())
		}

		By("creating a Key Vault and secret")
		Expect(azKeyVaultCreate(keyVaultName, resourceGroup, location)).To(Succeed())
		Expect(azKeyVaultSecretSet(keyVaultName, batsSecretName, batsSecretValue)).To(Succeed())

		By("creating a user-assigned managed identity")
		Expect(azIdentityCreate(identityName, resourceGroup)).To(Succeed())
		var err error
		clientID, err = azIdentityClientID(identityName, resourceGroup)
		Expect(err).NotTo(HaveOccurred())

		By("creating federated identity credentials for each test namespace")
		for i, ns := range []string{mainNamespace, testNS, negativeTestNS} {
			subject := fmt.Sprintf("system:serviceaccount:%s:default", ns)
			credName := fmt.Sprintf("sscsi-e2e-fed-cred-%s-%d", runSuffix, i)
			Expect(azFederatedCredentialCreate(credName, identityName, resourceGroup, oidcIssuer, subject, azureWIFAudience)).To(Succeed())
		}

		By("granting the identity access to read the Key Vault secret")
		principalID, err := azIdentityPrincipalID(identityName, resourceGroup)
		Expect(err).NotTo(HaveOccurred())
		Expect(azKeyVaultSetPolicy(keyVaultName, principalID)).To(Succeed())

		By("installing the real Azure provider")
		Expect(installAzureProvider()).To(Succeed())
		waitAzureProviderReady()

		By("configuring driverConfig.secretsStore.tokenRequests to Managed with the Azure WIF audience")
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
	})

	AfterAll(func() {
		By("tearing down Azure e2e fixtures")
		for _, ns := range []string{mainNamespace, testNS, negativeTestNS} {
			if err := deleteNamespace(ns); err != nil {
				GinkgoWriter.Printf("unable to delete namespace %q: %v\n", ns, err)
			}
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

	It("deploys an azure SecretProviderClass", func() {
		Expect(deploySecretProviderClass(mainNamespace, spcAzure, clientID, keyVaultName, tenantID, batsSecretName)).To(Succeed())
		out, err := runCmd("oc", "get", "secretproviderclass", spcAzure, "-n", mainNamespace, "-o", "yaml")
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("azure"))
	})

	It("creates an inline CSI volume pod", func() {
		Expect(createBatsInlinePod(mainNamespace, inlinePodName, spcAzure)).To(Succeed())
		Eventually(func() (bool, error) {
			pod, err := kubeClient.CoreV1().Pods(mainNamespace).Get(context.Background(), inlinePodName, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			return isPodReady(pod), nil
		}, 5*time.Minute, pollInterval).Should(BeTrue(), "pod %s/%s did not become Ready", mainNamespace, inlinePodName)
	})

	It("reads the Azure Key Vault secret from the inline volume pod", func() {
		Eventually(func() (string, error) {
			return readMountedSecret(mainNamespace, inlinePodName, batsSecretName)
		}, pollTimeout, pollInterval).Should(Equal(batsSecretValue))
	})

	It("deletes the inline volume pod cleanly", func() {
		Expect(deletePod(mainNamespace, inlinePodName)).To(Succeed())
		waitForPodDeleted(mainNamespace, inlinePodName)
	})

	Context("sync with Kubernetes secrets", func() {
		It("creates the sync SecretProviderClass and busybox deployments", func() {
			Expect(deploySyncSecretProviderClass(mainNamespace, spcAzureSync, clientID, keyVaultName, tenantID, batsSecretName, syncLabelValue)).To(Succeed())
			Expect(deploySyncDeployment(mainNamespace, deploymentOne, spcAzureSync)).To(Succeed())
			Expect(deploySyncDeployment(mainNamespace, deploymentTwo, spcAzureSync)).To(Succeed())
			waitForLabeledPodsReady(mainNamespace, "busybox", 90*time.Second)
		})

		It("reads mounted content, synced secrets, env vars, and owner references", func() {
			podName, err := podNameByLabel(mainNamespace, "busybox")
			Expect(err).NotTo(HaveOccurred())

			got, err := readMountedFile(mainNamespace, podName, "/mnt/secrets-store/secretalias")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(batsSecretValue))

			username, err := getSecretKey(mainNamespace, "foosecret", "username")
			Expect(err).NotTo(HaveOccurred())
			Expect(username).To(Equal(batsSecretValue))

			envVal, err := podEnvValue(mainNamespace, podName, "SECRET_USERNAME")
			Expect(err).NotTo(HaveOccurred())
			Expect(envVal).To(Equal(batsSecretValue))

			label, err := getSecretLabel(mainNamespace, "foosecret", "environment")
			Expect(err).NotTo(HaveOccurred())
			Expect(label).To(Equal(syncLabelValue))

			managed, err := getSecretLabel(mainNamespace, "foosecret", "secrets-store.csi.k8s.io/managed")
			Expect(err).NotTo(HaveOccurred())
			Expect(managed).To(Equal("true"))

			waitForSecretOwnerCount(mainNamespace, "foosecret", 2)
		})

		It("deletes one deployment, then both, and cleans up the synced secret", func() {
			Expect(deleteDeployment(mainNamespace, deploymentOne)).To(Succeed())
			waitForSecretOwnerCount(mainNamespace, "foosecret", 1)

			Expect(deleteDeployment(mainNamespace, deploymentTwo)).To(Succeed())
			waitForSecretDeleted(mainNamespace, "foosecret")
		})
	})

	Context("namespaced SecretProviderClass", func() {
		It("deploys cluster- and namespace-scoped SPCs and a busybox deployment in test-ns", func() {
			Expect(deployNamespacedSecretProviderClasses(mainNamespace, testNS, clientID, keyVaultName, tenantID, batsSecretName)).To(Succeed())
			Expect(deploySyncDeployment(testNS, deploymentOne, spcAzureSync)).To(Succeed())
			waitForLabeledPodsReady(testNS, "busybox", 60*time.Second)
		})

		It("reads mounted content, synced secrets, env vars, and owner references in test-ns", func() {
			podName, err := podNameByLabel(testNS, "busybox")
			Expect(err).NotTo(HaveOccurred())

			got, err := readMountedFile(testNS, podName, "/mnt/secrets-store/secretalias")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(batsSecretValue))

			username, err := getSecretKey(testNS, "foosecret", "username")
			Expect(err).NotTo(HaveOccurred())
			Expect(username).To(Equal(batsSecretValue))

			envVal, err := podEnvValue(testNS, podName, "SECRET_USERNAME")
			Expect(err).NotTo(HaveOccurred())
			Expect(envVal).To(Equal(batsSecretValue))

			waitForSecretOwnerCount(testNS, "foosecret", 1)
		})

		It("deletes the deployment and synced secret in test-ns", func() {
			Expect(deleteDeployment(testNS, deploymentOne)).To(Succeed())
			waitForSecretDeleted(testNS, "foosecret")
		})
	})

	Context("namespaced SecretProviderClass negative test", func() {
		It("fails to mount when the SecretProviderClass is absent in the pod namespace", func() {
			Expect(deploySyncDeployment(negativeTestNS, deploymentOne, spcAzureSync)).To(Succeed())
			var podName string
			Eventually(func() error {
				var err error
				podName, err = podNameByLabel(negativeTestNS, "busybox")
				return err
			}, pollTimeout, pollInterval).Should(Succeed())
			waitForPodMountFailure(negativeTestNS, podName, fmt.Sprintf("failed to get secretproviderclass %s/%s", negativeTestNS, spcAzureSync))
			Expect(deleteDeployment(negativeTestNS, deploymentOne)).To(Succeed())
		})
	})

	Context("multiple SecretProviderClass", func() {
		It("deploys multiple SecretProviderClasses", func() {
			Expect(deployMultipleSecretProviderClasses(mainNamespace, clientID, keyVaultName, tenantID, batsSecretName)).To(Succeed())
			for _, name := range []string{"azure-spc-0", "azure-spc-1"} {
				out, err := runCmd("oc", "get", "secretproviderclass", name, "-n", mainNamespace, "-o", "yaml")
				Expect(err).NotTo(HaveOccurred())
				Expect(out).To(ContainSubstring(name))
			}
		})

		It("creates a pod mounting multiple SecretProviderClasses", func() {
			Expect(createMultipleSPCPod(mainNamespace, multiplePodName)).To(Succeed())
			waitPodReady(mainNamespace, multiplePodName)
		})

		It("reads mounted content, synced secrets, and env vars from both volumes", func() {
			for _, mount := range []string{"/mnt/secrets-store-0/secretalias", "/mnt/secrets-store-1/secretalias"} {
				got, err := readMountedFile(mainNamespace, multiplePodName, mount)
				Expect(err).NotTo(HaveOccurred())
				Expect(got).To(Equal(batsSecretValue))
			}

			for _, secretName := range []string{"foosecret-0", "foosecret-1"} {
				username, err := getSecretKey(mainNamespace, secretName, "username")
				Expect(err).NotTo(HaveOccurred())
				Expect(username).To(Equal(batsSecretValue))
				waitForSecretOwnerCount(mainNamespace, secretName, 1)
			}

			for _, envName := range []string{"SECRET_USERNAME_0", "SECRET_USERNAME_1"} {
				envVal, err := podEnvValue(mainNamespace, multiplePodName, envName)
				Expect(err).NotTo(HaveOccurred())
				Expect(envVal).To(Equal(batsSecretValue))
			}
		})
	})

	Context("operator secret rotation against the real Key Vault", func() {
		BeforeAll(func() {
			By("deploying a pod for the rotation assertion")
			Expect(deploySecretProviderClass(mainNamespace, spcAzure, clientID, keyVaultName, tenantID, batsSecretName)).To(Succeed())
			Expect(createInlineVolumePod(mainNamespace, rotationPodName, spcAzure)).To(Succeed())
			waitPodReady(mainNamespace, rotationPodName)
			got, err := readMountedSecret(mainNamespace, rotationPodName, batsSecretName)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(rotationSecretVal))
		})

		It("reflects an updated Key Vault secret value in the mounted file within minimumRefreshAge", func() {
			rotateAndAssert(mainNamespace, rotationPodName, keyVaultName, batsSecretName, &rotationSecretVal)
		})
	})
})
