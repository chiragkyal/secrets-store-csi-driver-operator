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

// TestWithSecretsStoreRotationDaemonSetHook_AppendsWhenArgsMissing covers task T4_3:
// setArgPrefix's append branch, which is never exercised by the other tests above
// (their fixture always pre-populates both rotation args).
func TestWithSecretsStoreRotationDaemonSetHook_AppendsWhenArgsMissing(t *testing.T) {
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "secrets-store-csi-driver-node", Namespace: "openshift-cluster-csi-drivers"},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: csiDriverContainerName, Args: []string{"--endpoint=$(CSI_ENDPOINT)"}},
					},
				},
			},
		},
	}
	hook := withSecretsStoreRotationDaemonSetHook(&fakeClusterCSIDriverLister{})

	if err := hook(nil, ds); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	container, _ := findContainer(ds, csiDriverContainerName)
	if len(container.Args) != 3 {
		t.Fatalf("expected 3 args (1 original + 2 appended), got %d: %v", len(container.Args), container.Args)
	}
	assertArgValue(t, container.Args, rotationEnabledArgPrefix, "true")
	assertArgValue(t, container.Args, rotationIntervalArgPrefix, "120s")
}

// TestWithSecretsStoreRotationDaemonSetHook_UnrelatedArgsPreserved covers task T4_3:
// a regression-safety check that the find/replace-by-prefix logic does not disturb
// unrelated args or other containers.
func TestWithSecretsStoreRotationDaemonSetHook_UnrelatedArgsPreserved(t *testing.T) {
	ds := buildTestDaemonSet()
	originalArgCount := len(ds.Spec.Template.Spec.Containers[0].Args)
	otherContainer := ds.Spec.Template.Spec.Containers[1]

	hook := withSecretsStoreRotationDaemonSetHook(&fakeClusterCSIDriverLister{})
	if err := hook(nil, ds); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	container, _ := findContainer(ds, csiDriverContainerName)
	if len(container.Args) != originalArgCount {
		t.Errorf("expected arg count to stay %d (replace, not append, when both flags already present), got %d: %v", originalArgCount, len(container.Args), container.Args)
	}
	found := false
	for _, a := range container.Args {
		if a == "--endpoint=$(CSI_ENDPOINT)" {
			found = true
		}
		if strings.HasPrefix(a, "--provider-health-check=") && a != "--provider-health-check=false" {
			t.Errorf("expected unrelated arg --provider-health-check=false to be untouched, got %q", a)
		}
	}
	if !found {
		t.Errorf("expected unrelated arg --endpoint=$(CSI_ENDPOINT) to be preserved, got %v", container.Args)
	}
	if ds.Spec.Template.Spec.Containers[1].Name != otherContainer.Name {
		t.Errorf("expected other container to be untouched")
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
