package operator

import (
	"errors"
	"strings"
	"testing"

	opv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// buildTestDaemonSet returns a DaemonSet fixture matching assets/node.yaml's
// csi-driver container args shape (subset relevant to rotation flags).
func buildTestDaemonSet() *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "secrets-store-csi-driver-node", Namespace: "openshift-cluster-csi-drivers"},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: csiDriverContainerName,
							Args: []string{
								"--endpoint=$(CSI_ENDPOINT)",
								"--enable-secret-rotation=true",
								"--rotation-poll-interval=2m",
								"--provider-health-check=false",
							},
						},
						{Name: "csi-node-driver-registrar"},
					},
				},
			},
		},
	}
}

func TestWithSecretsStoreRotationDaemonSetHook_Defaults(t *testing.T) {
	hook := withSecretsStoreRotationDaemonSetHook(&fakeClusterCSIDriverLister{})
	ds := buildTestDaemonSet()

	if err := hook(nil, ds); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	container, err := findContainer(ds, csiDriverContainerName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgValue(t, container.Args, rotationEnabledArgPrefix, "true")
	assertArgValue(t, container.Args, rotationIntervalArgPrefix, "120s")
}

func TestWithSecretsStoreRotationDaemonSetHook_CustomInterval(t *testing.T) {
	driverCR := &opv1.ClusterCSIDriver{
		ObjectMeta: metav1.ObjectMeta{Name: providerName},
		Spec: opv1.ClusterCSIDriverSpec{
			DriverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					SecretRotation: opv1.SecretsStoreSecretRotation{
						Type:   opv1.SecretRotationCustom,
						Custom: opv1.CustomSecretRotation{RotationPollIntervalSeconds: 300},
					},
				},
			},
		},
	}
	hook := withSecretsStoreRotationDaemonSetHook(&fakeClusterCSIDriverLister{driver: driverCR})
	ds := buildTestDaemonSet()

	if err := hook(nil, ds); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	container, _ := findContainer(ds, csiDriverContainerName)
	assertArgValue(t, container.Args, rotationEnabledArgPrefix, "true")
	assertArgValue(t, container.Args, rotationIntervalArgPrefix, "300s")
}

func TestWithSecretsStoreRotationDaemonSetHook_RotationDisabled(t *testing.T) {
	driverCR := &opv1.ClusterCSIDriver{
		ObjectMeta: metav1.ObjectMeta{Name: providerName},
		Spec: opv1.ClusterCSIDriverSpec{
			DriverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					SecretRotation: opv1.SecretsStoreSecretRotation{Type: opv1.SecretRotationNone},
				},
			},
		},
	}
	hook := withSecretsStoreRotationDaemonSetHook(&fakeClusterCSIDriverLister{driver: driverCR})
	ds := buildTestDaemonSet()

	if err := hook(nil, ds); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	container, _ := findContainer(ds, csiDriverContainerName)
	assertArgValue(t, container.Args, rotationEnabledArgPrefix, "false")
}

func TestWithSecretsStoreRotationDaemonSetHook_ContainerNotFound(t *testing.T) {
	hook := withSecretsStoreRotationDaemonSetHook(&fakeClusterCSIDriverLister{})
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "some-ds", Namespace: "ns"},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "not-csi-driver"}}},
			},
		},
	}

	err := hook(nil, ds)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error when csi-driver container is missing, got %v", err)
	}
}

func TestWithSecretsStoreRotationDaemonSetHook_ListerError(t *testing.T) {
	hook := withSecretsStoreRotationDaemonSetHook(&fakeClusterCSIDriverLister{err: errors.New("boom")})
	ds := buildTestDaemonSet()

	err := hook(nil, ds)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected error wrapping lister failure, got %v", err)
	}
}

func assertArgValue(t *testing.T, args []string, prefix, expectedValue string) {
	t.Helper()
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			if a != prefix+expectedValue {
				t.Errorf("expected arg %q, got %q", prefix+expectedValue, a)
			}
			return
		}
	}
	t.Errorf("expected an arg with prefix %q, found none in %v", prefix, args)
}
