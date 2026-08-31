package azure

import (
	"context"
	"fmt"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	// testImage matches hack/e2e.sh's E2E_TEST_IMAGE.
	testImage = "quay.io/openshifttest/busybox:multiarch"
	// secretProviderClassAPIVersion is the stable SecretProviderClass CRD
	// version. SecretProviderClass is a CRD owned by the driver, not part
	// of client-go's typed API, so it is applied via `oc apply` below.
	secretProviderClassAPIVersion = "secrets-store.csi.x-k8s.io/v1"
)

// createPrivilegedNamespace creates a namespace labeled for privileged pod
// security (matching hack/e2e.sh's test_setup), then grants the default
// ServiceAccount the privileged SCC (the CSI driver and this test's pod
// both need privileged access to mount/read the secrets-store volume).
func createPrivilegedNamespace(name string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"security.openshift.io/scc.podSecurityLabelSync": "false",
				"pod-security.kubernetes.io/enforce":             "privileged",
				"pod-security.kubernetes.io/audit":               "privileged",
				"pod-security.kubernetes.io/warn":                "privileged",
			},
		},
	}
	if _, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("unable to create namespace %q: %w", name, err)
	}

	_, err := runCmd("oc", "adm", "policy", "add-scc-to-user", "privileged", fmt.Sprintf("system:serviceaccount:%s:default", name))
	return err
}

// deleteNamespace deletes namespace, best-effort.
func deleteNamespace(name string) error {
	return kubeClient.CoreV1().Namespaces().Delete(context.Background(), name, metav1.DeleteOptions{})
}

// deploySecretProviderClass applies an azure-provider SecretProviderClass
// named spcName in namespace, referencing keyvaultName/clientID/tenantID and
// a single secret object named secretName -- matching
// test/bats/tests/azure/azure_v1_secretproviderclass.yaml.
func deploySecretProviderClass(namespace, spcName, clientID, keyvaultName, tenantID, secretName string) error {
	manifest := fmt.Sprintf(`apiVersion: %s
kind: SecretProviderClass
metadata:
  name: %s
  namespace: %s
spec:
  provider: azure
  parameters:
    clientID: "%s"
    keyvaultName: "%s"
    objects: |
      array:
        - |
          objectName: %s
          objectType: secret
    tenantId: "%s"
`, secretProviderClassAPIVersion, spcName, namespace, clientID, keyvaultName, secretName, tenantID)

	_, err := runCmdStdin(manifest, "oc", "apply", "-f", "-")
	return err
}

// createInlineVolumePod creates a pod in namespace that mounts secretName
// via a CSI inline volume referencing the given SecretProviderClass.
func createInlineVolumePod(namespace, podName, spcName string) error {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels:    map[string]string{"name": podName},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "default",
			Containers: []corev1.Container{
				{
					Name:    "test-container",
					Image:   testImage,
					Command: []string{"sh", "-c", "sleep 3600"},
					SecurityContext: &corev1.SecurityContext{
						Privileged: ptr.To(true),
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "secrets-store-inline",
							MountPath: "/mnt/secrets-store",
							ReadOnly:  true,
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "secrets-store-inline",
					VolumeSource: corev1.VolumeSource{
						CSI: &corev1.CSIVolumeSource{
							Driver:           driverName,
							ReadOnly:         ptr.To(true),
							VolumeAttributes: map[string]string{"secretProviderClass": spcName},
						},
					},
				},
			},
		},
	}
	_, err := kubeClient.CoreV1().Pods(namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	return err
}

// waitPodReady polls until the named pod's Ready condition is True.
func waitPodReady(namespace, podName string) {
	Eventually(func() (bool, error) {
		pod, err := kubeClient.CoreV1().Pods(namespace).Get(context.Background(), podName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return isPodReady(pod), nil
	}, pollTimeout, pollInterval).Should(BeTrue(), "pod %s/%s did not become Ready", namespace, podName)
}

// readMountedSecret execs into the pod to read the mounted secret file at
// /mnt/secrets-store/<secretName>, matching azure.bats's assertions.
func readMountedSecret(namespace, podName, secretName string) (string, error) {
	return runCmd("oc", "exec", "-n", namespace, podName, "--", "cat", "/mnt/secrets-store/"+secretName)
}
