package azure

import (
	"context"
	"fmt"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// azureProviderVersion pins the upstream Azure Key Vault provider release
	// installed via provider-azure-installer.yaml. Update deliberately.
	azureProviderVersion = "v1.8.2"
	// azureProviderServiceAccount is the ServiceAccount created by
	// provider-azure-installer.yaml.
	azureProviderServiceAccount = "csi-secrets-store-provider-azure"
)

// azureProviderInstallerURL is the pinned upstream deployment manifest for a
// provider-only install (no bundled CSI driver). See:
// https://azure.github.io/secrets-store-csi-driver-provider-azure/docs/getting-started/installation/#using-deployment-yamls
const azureProviderInstallerURL = "https://github.com/Azure/secrets-store-csi-driver-provider-azure/releases/download/" + azureProviderVersion + "/provider-azure-installer.yaml"

// installAzureProvider installs the real Azure Key Vault provider via the
// upstream provider-azure-installer.yaml manifest (provider only -- this
// operator already manages the CSI driver DaemonSet). On OpenShift, the
// provider's hostPath socket mount requires the privileged SCC.
func installAzureProvider() error {
	if _, err := runCmd("oc", "apply", "-n", providerNamespace, "-f", azureProviderInstallerURL); err != nil {
		return err
	}
	_, err := runCmd("oc", "adm", "policy", "add-scc-to-user", "privileged",
		fmt.Sprintf("system:serviceaccount:%s:%s", providerNamespace, azureProviderServiceAccount))
	return err
}

// uninstallAzureProvider removes the manifest installed by
// installAzureProvider. Best-effort cleanup: callers should log, not fail,
// on error.
func uninstallAzureProvider() error {
	_, err := runCmd("oc", "delete", "-n", providerNamespace, "-f", azureProviderInstallerURL, "--ignore-not-found")
	return err
}

// waitAzureProviderReady polls until at least one Azure provider pod is
// Ready, matching azure.bats's `kubectl wait --for=condition=Ready` step.
func waitAzureProviderReady() {
	Eventually(func() (bool, error) {
		pods, err := kubeClient.CoreV1().Pods(providerNamespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: "app=" + providerAppLabel,
		})
		if err != nil {
			return false, err
		}
		if len(pods.Items) == 0 {
			return false, nil
		}
		for _, pod := range pods.Items {
			if !isPodReady(&pod) {
				return false, nil
			}
		}
		return true, nil
	}, pollTimeout, pollInterval).Should(BeTrue(), "Azure provider pods in namespace %q did not become Ready", providerNamespace)
}

// isPodReady reports whether pod's Ready condition is True.
func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}
