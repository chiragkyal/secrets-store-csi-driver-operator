package azure

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	return readMountedFile(namespace, podName, "/mnt/secrets-store/"+secretName)
}

// readMountedFile execs into the pod and cats mountPath.
func readMountedFile(namespace, podName, mountPath string) (string, error) {
	return runCmd("oc", "exec", "-n", namespace, podName, "--", "cat", mountPath)
}

// applyManifest applies a multi-document manifest via oc apply.
func applyManifest(manifest string) error {
	_, err := runCmdStdin(manifest, "oc", "apply", "-f", "-")
	return err
}

// deleteManifest deletes resources described by manifest via oc delete.
func deleteManifest(manifest string) error {
	_, err := runCmdStdin(manifest, "oc", "delete", "-f", "-", "--ignore-not-found")
	return err
}

// deploySyncSecretProviderClass applies azure_synck8s_v1_secretproviderclass.yaml
// in namespace with the given sync secret label value.
func deploySyncSecretProviderClass(namespace, spcName, clientID, keyvaultName, tenantID, secretName, labelValue string) error {
	return applyManifest(fmt.Sprintf(`apiVersion: %s
kind: SecretProviderClass
metadata:
  name: %s
  namespace: %s
spec:
  provider: azure
  secretObjects:
  - secretName: foosecret
    type: Opaque
    labels:
      environment: "%s"
    data:
    - objectName: secretalias
      key: username
  parameters:
    clientID: "%s"
    keyvaultName: "%s"
    objects: |
      array:
        - |
          objectName: %s
          objectType: secret
          objectAlias: secretalias
    tenantId: "%s"
`, secretProviderClassAPIVersion, spcName, namespace, labelValue, clientID, keyvaultName, secretName, tenantID))
}

// deployNamespacedSecretProviderClasses applies the cluster-scoped and
// test-ns-scoped SPC pair from azure_v1_secretproviderclass_ns.yaml.
func deployNamespacedSecretProviderClasses(mainNamespace, testNS, clientID, keyvaultName, tenantID, secretName string) error {
	return applyManifest(fmt.Sprintf(`apiVersion: %s
kind: SecretProviderClass
metadata:
  name: azure-sync
  namespace: %s
spec:
  provider: invalidprovider
  secretObjects:
  - secretName: foosecret
    type: Opaque
    data:
    - objectName: secretalias
      key: username
  parameters:
    clientID: "%s"
    keyvaultName: "%s"
    objects: |
      array:
        - |
          objectName: %s
          objectType: secret
          objectAlias: secretalias
    tenantId: "%s"
---
apiVersion: %s
kind: SecretProviderClass
metadata:
  name: azure-sync
  namespace: %s
spec:
  provider: azure
  secretObjects:
  - secretName: foosecret
    type: Opaque
    data:
    - objectName: secretalias
      key: username
  parameters:
    clientID: "%s"
    keyvaultName: "%s"
    objects: |
      array:
        - |
          objectName: %s
          objectType: secret
          objectAlias: secretalias
    tenantId: "%s"
`, secretProviderClassAPIVersion, mainNamespace, clientID, keyvaultName, secretName, tenantID,
		secretProviderClassAPIVersion, testNS, clientID, keyvaultName, secretName, tenantID))
}

// deployMultipleSecretProviderClasses applies azure_v1_multiple_secretproviderclass.yaml.
func deployMultipleSecretProviderClasses(namespace, clientID, keyvaultName, tenantID, secretName string) error {
	spc := func(name string) string {
		return fmt.Sprintf(`apiVersion: %s
kind: SecretProviderClass
metadata:
  name: %s
  namespace: %s
spec:
  provider: azure
  secretObjects:
  - secretName: foosecret-%s
    type: Opaque
    data:
    - objectName: secretalias
      key: username
  parameters:
    clientID: "%s"
    keyvaultName: "%s"
    objects: |
      array:
        - |
          objectName: %s
          objectType: secret
          objectAlias: secretalias
    tenantId: "%s"
`, secretProviderClassAPIVersion, name, namespace, strings.TrimPrefix(name, "azure-spc-"), clientID, keyvaultName, secretName, tenantID)
	}
	return applyManifest(spc("azure-spc-0") + "---\n" + spc("azure-spc-1"))
}

// createBatsInlinePod creates pod-secrets-store-inline-volume-crd.yaml in namespace.
func createBatsInlinePod(namespace, podName, spcName string) error {
	return applyManifest(fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  terminationGracePeriodSeconds: 0
  containers:
  - image: %s
    name: busybox
    imagePullPolicy: IfNotPresent
    command: ["/bin/sleep", "3600"]
    volumeMounts:
    - name: secrets-store-inline
      mountPath: /mnt/secrets-store
      readOnly: true
  volumes:
  - name: secrets-store-inline
    csi:
      driver: %s
      readOnly: true
      volumeAttributes:
        secretProviderClass: "%s"
  nodeSelector:
    kubernetes.io/os: linux
`, podName, namespace, testImage, driverName, spcName))
}

// createMultipleSPCPod creates pod-azure-inline-volume-multiple-spc.yaml.
func createMultipleSPCPod(namespace, podName string) error {
	return applyManifest(fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  terminationGracePeriodSeconds: 0
  containers:
  - image: %s
    name: busybox
    imagePullPolicy: IfNotPresent
    command: ["/bin/sleep", "3600"]
    volumeMounts:
    - name: secrets-store-inline-0
      mountPath: /mnt/secrets-store-0
      readOnly: true
    - name: secrets-store-inline-1
      mountPath: /mnt/secrets-store-1
      readOnly: true
    env:
    - name: SECRET_USERNAME_0
      valueFrom:
        secretKeyRef:
          name: foosecret-0
          key: username
    - name: SECRET_USERNAME_1
      valueFrom:
        secretKeyRef:
          name: foosecret-1
          key: username
  volumes:
  - name: secrets-store-inline-0
    csi:
      driver: %s
      readOnly: true
      volumeAttributes:
        secretProviderClass: azure-spc-0
  - name: secrets-store-inline-1
    csi:
      driver: %s
      readOnly: true
      volumeAttributes:
        secretProviderClass: azure-spc-1
  nodeSelector:
    kubernetes.io/os: linux
`, podName, namespace, testImage, driverName, driverName))
}

// deploySyncDeployment applies deployment-synck8s-azure.yaml in namespace.
func deploySyncDeployment(namespace, deploymentName, spcName string) error {
	return applyManifest(fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
  labels:
    app: busybox
spec:
  replicas: 2
  selector:
    matchLabels:
      app: busybox
  template:
    metadata:
      labels:
        app: busybox
    spec:
      terminationGracePeriodSeconds: 0
      containers:
      - image: %s
        name: busybox
        imagePullPolicy: IfNotPresent
        command: ["/bin/sleep", "3600"]
        env:
        - name: SECRET_USERNAME
          valueFrom:
            secretKeyRef:
              name: foosecret
              key: username
        volumeMounts:
        - name: secrets-store-inline
          mountPath: /mnt/secrets-store
          readOnly: true
      volumes:
      - name: secrets-store-inline
        csi:
          driver: %s
          readOnly: true
          volumeAttributes:
            secretProviderClass: "%s"
      nodeSelector:
        kubernetes.io/os: linux
`, deploymentName, namespace, testImage, driverName, spcName))
}

// deleteDeployment deletes a deployment by name in namespace.
func deleteDeployment(namespace, name string) error {
	return kubeClient.AppsV1().Deployments(namespace).Delete(context.Background(), name, metav1.DeleteOptions{})
}

// waitForLabeledPodsReady waits until all pods with app=<label> are Ready.
func waitForLabeledPodsReady(namespace, label string, timeout time.Duration) {
	Eventually(func() (bool, error) {
		pods, err := kubeClient.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "app=" + label})
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
	}, timeout, pollInterval).Should(BeTrue(), "pods with label app=%s in %s did not become Ready", label, namespace)
}

// podNameByLabel returns the first pod name matching app=<label>.
func podNameByLabel(namespace, label string) (string, error) {
	pods, err := kubeClient.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "app=" + label})
	if err != nil {
		return "", err
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pods with label app=%s in namespace %s", label, namespace)
	}
	return pods.Items[0].Name, nil
}

// podEnvValue returns the value of envName from the pod's environment.
func podEnvValue(namespace, podName, envName string) (string, error) {
	out, err := runCmd("oc", "exec", "-n", namespace, podName, "--", "printenv", envName)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// deletePod deletes podName in namespace.
func deletePod(namespace, podName string) error {
	return kubeClient.CoreV1().Pods(namespace).Delete(context.Background(), podName, metav1.DeleteOptions{})
}

// waitForPodDeleted polls until podName is gone from namespace.
func waitForPodDeleted(namespace, podName string) {
	Eventually(func() error {
		_, err := kubeClient.CoreV1().Pods(namespace).Get(context.Background(), podName, metav1.GetOptions{})
		return err
	}, pollTimeout, pollInterval).Should(Satisfy(apierrors.IsNotFound), "pod %s/%s was not deleted", namespace, podName)
}

// waitForPodMountFailure polls until pod events contain a FailedMount whose
// message matches wantSubstring, matching azure.bats's negative namespace test.
func waitForPodMountFailure(namespace, podName, wantSubstring string) {
	Eventually(func() (bool, error) {
		events, err := kubeClient.CoreV1().Events(namespace).List(context.Background(), metav1.ListOptions{
			FieldSelector: fmt.Sprintf("involvedObject.name=%s", podName),
		})
		if err != nil {
			return false, err
		}
		for _, event := range events.Items {
			if event.Reason == "FailedMount" && strings.Contains(event.Message, wantSubstring) {
				return true, nil
			}
		}
		return false, nil
	}, pollTimeout, pollInterval).Should(BeTrue(), "pod %s/%s did not emit FailedMount containing %q", namespace, podName, wantSubstring)
}

// waitForDeploymentReady polls until deployment has ready replicas.
func waitForDeploymentReady(namespace, name string) {
	Eventually(func() (bool, error) {
		deploy, err := kubeClient.AppsV1().Deployments(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		want := int32(1)
		if deploy.Spec.Replicas != nil {
			want = *deploy.Spec.Replicas
		}
		return deploy.Status.ReadyReplicas == want, nil
	}, pollTimeout, pollInterval).Should(BeTrue(), "deployment %s/%s did not become ready", namespace, name)
}
