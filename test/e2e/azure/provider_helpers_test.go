package azure

import (
	"context"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// helmReleaseName is the Helm release name for the Azure provider install,
// matching azure.bats's `helm upgrade --install csi ...`.
const helmReleaseName = "csi"

// installAzureProvider installs the real Azure Key Vault provider via Helm,
// matching azure.bats's "install azure provider" step exactly (with the
// upstream secrets-store-csi-driver install disabled, since this operator
// already manages the driver DaemonSet).
func installAzureProvider() error {
	if _, err := runCmd("helm", "repo", "add", "csi-provider-azure", "https://azure.github.io/secrets-store-csi-driver-provider-azure/charts"); err != nil {
		return err
	}
	if _, err := runCmd("helm", "repo", "update"); err != nil {
		return err
	}
	_, err := runCmd("helm", "upgrade", "--install", helmReleaseName, "csi-provider-azure/csi-secrets-store-provider-azure",
		"--namespace", providerNamespace,
		"--set", "secrets-store-csi-driver.install=false",
		"--set", "windows.enabled=false",
		"--set", "logVerbosity=5",
		"--set", "logFormatJSON=true")
	return err
}

// uninstallAzureProvider removes the Helm release installed by
// installAzureProvider. Best-effort cleanup: callers should log, not fail,
// on error.
func uninstallAzureProvider() error {
	_, err := runCmd("helm", "uninstall", helmReleaseName, "--namespace", providerNamespace)
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
