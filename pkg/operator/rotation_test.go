package operator

import (
	"reflect"
	"testing"
	"time"

	opv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

func TestSetArg(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		prefix   string
		value    string
		expected []string
	}{
		{
			name:     "replaces an existing arg matching the prefix",
			args:     []string{"--enable-secret-rotation=true", "--rotation-poll-interval=2m"},
			prefix:   "--rotation-poll-interval=",
			value:    "5m0s",
			expected: []string{"--enable-secret-rotation=true", "--rotation-poll-interval=5m0s"},
		},
		{
			name:     "appends the arg when no existing element matches the prefix",
			args:     []string{"--enable-secret-rotation=true"},
			prefix:   "--rotation-poll-interval=",
			value:    "2m0s",
			expected: []string{"--enable-secret-rotation=true", "--rotation-poll-interval=2m0s"},
		},
		{
			name:     "does not reorder or otherwise affect unrelated args",
			args:     []string{"--a=1", "--rotation-poll-interval=2m", "--b=2"},
			prefix:   "--rotation-poll-interval=",
			value:    "10s",
			expected: []string{"--a=1", "--rotation-poll-interval=10s", "--b=2"},
		},
		{
			name:     "appends into an empty args slice",
			args:     []string{},
			prefix:   "--enable-secret-rotation=",
			value:    "false",
			expected: []string{"--enable-secret-rotation=false"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := setArg(tc.args, tc.prefix, tc.value)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Fatalf("expected args to be %v, got %v", tc.expected, got)
			}
		})
	}
}

func TestGetSecretRotationConfig(t *testing.T) {
	cases := []struct {
		name             string
		driverConfig     opv1.CSIDriverConfigSpec
		expectedEnabled  bool
		expectedInterval time.Duration
	}{
		{
			name:             "nil driverConfig (driverType unset) returns defaults",
			driverConfig:     opv1.CSIDriverConfigSpec{},
			expectedEnabled:  true,
			expectedInterval: 2 * time.Minute,
		},
		{
			name: "driverType not SecretsStore returns defaults",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.AWSDriverType,
			},
			expectedEnabled:  true,
			expectedInterval: 2 * time.Minute,
		},
		{
			name: "driverType SecretsStore with nil secretsStore returns defaults",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
			},
			expectedEnabled:  true,
			expectedInterval: 2 * time.Minute,
		},
		{
			name: "secretRotation zero value (type unset) returns defaults",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType:   opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{},
			},
			expectedEnabled:  true,
			expectedInterval: 2 * time.Minute,
		},
		{
			name: "type None disables rotation",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					SecretRotation: opv1.SecretsStoreSecretRotation{
						Type: opv1.SecretRotationNone,
					},
				},
			},
			expectedEnabled:  false,
			expectedInterval: 2 * time.Minute,
		},
		{
			name: "type Custom with explicit minimumRefreshAge uses that interval",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					SecretRotation: opv1.SecretsStoreSecretRotation{
						Type: opv1.SecretRotationCustom,
						Custom: opv1.CustomSecretRotation{
							MinimumRefreshAge: 300,
						},
					},
				},
			},
			expectedEnabled:  true,
			expectedInterval: 5 * time.Minute,
		},
		{
			name: "type Custom with omitted minimumRefreshAge defaults to 120s",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					SecretRotation: opv1.SecretsStoreSecretRotation{
						Type: opv1.SecretRotationCustom,
					},
				},
			},
			expectedEnabled:  true,
			expectedInterval: 120 * time.Second,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enabled, interval := getSecretRotationConfig(tc.driverConfig)
			if enabled != tc.expectedEnabled {
				t.Errorf("expected enabled to be %v, got %v", tc.expectedEnabled, enabled)
			}
			if interval != tc.expectedInterval {
				t.Errorf("expected interval to be %v, got %v", tc.expectedInterval, interval)
			}
		})
	}
}

// newFakeClusterCSIDriverLister builds a cache.GenericLister backed by an
// in-memory indexer, pre-populated with driver if non-nil. This mirrors how
// starter.go's dynamicInformers.ForResource(gvr).Lister() is constructed,
// without requiring a live informer or API server.
func newFakeClusterCSIDriverLister(t *testing.T, driver *opv1.ClusterCSIDriver) cache.GenericLister {
	t.Helper()
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	if driver != nil {
		obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(driver)
		if err != nil {
			t.Fatalf("failed to convert ClusterCSIDriver to unstructured: %v", err)
		}
		if err := indexer.Add(&unstructured.Unstructured{Object: obj}); err != nil {
			t.Fatalf("failed to add ClusterCSIDriver to indexer: %v", err)
		}
	}
	return cache.NewGenericLister(indexer, opv1.SchemeGroupVersion.WithResource("clustercsidrivers").GroupResource())
}

// newTestDaemonSet returns a DaemonSet with a csi-driver container carrying
// the pre-feature, hardcoded rotation args from assets/node.yaml, matching
// the baseline this feature must preserve for unconfigured clusters.
func newTestDaemonSet() *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "secrets-store-csi-driver-node", Namespace: "openshift-cluster-csi-drivers"},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: csiDriverContainerName,
							Args: []string{
								"--enable-secret-rotation=true",
								"--rotation-poll-interval=2m",
							},
						},
					},
				},
			},
		},
	}
}

func TestWithSecretRotationDaemonSetHook(t *testing.T) {
	const driverName = "secrets-store.csi.k8s.io"

	cases := []struct {
		name         string
		driver       *opv1.ClusterCSIDriver
		expectedArgs []string
	}{
		{
			name:   "ClusterCSIDriver not found leaves args untouched",
			driver: nil,
			expectedArgs: []string{
				"--enable-secret-rotation=true",
				"--rotation-poll-interval=2m",
			},
		},
		{
			name: "no driverConfig set keeps defaults",
			driver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: driverName},
			},
			expectedArgs: []string{
				"--enable-secret-rotation=true",
				"--rotation-poll-interval=2m0s",
			},
		},
		{
			name: "type None disables rotation",
			driver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: driverName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: opv1.SecretsStoreSecretRotation{
								Type: opv1.SecretRotationNone,
							},
						},
					},
				},
			},
			expectedArgs: []string{
				"--enable-secret-rotation=false",
				"--rotation-poll-interval=2m0s",
			},
		},
		{
			name: "type Custom with explicit interval sets that interval",
			driver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: driverName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: opv1.SecretsStoreSecretRotation{
								Type: opv1.SecretRotationCustom,
								Custom: opv1.CustomSecretRotation{
									MinimumRefreshAge: 300,
								},
							},
						},
					},
				},
			},
			expectedArgs: []string{
				"--enable-secret-rotation=true",
				"--rotation-poll-interval=5m0s",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lister := newFakeClusterCSIDriverLister(t, tc.driver)
			hook := WithSecretRotationDaemonSetHook(lister, driverName)

			daemonSet := newTestDaemonSet()
			if err := hook(&opv1.OperatorSpec{}, daemonSet); err != nil {
				t.Fatalf("unexpected error from hook: %v", err)
			}

			gotArgs := daemonSet.Spec.Template.Spec.Containers[0].Args
			if !reflect.DeepEqual(gotArgs, tc.expectedArgs) {
				t.Fatalf("expected args to be %v, got %v", tc.expectedArgs, gotArgs)
			}
		})
	}
}

func TestWithSecretRotationDaemonSetHookMissingContainer(t *testing.T) {
	const driverName = "secrets-store.csi.k8s.io"

	// A present (but otherwise unconfigured) ClusterCSIDriver so the hook
	// proceeds past its NotFound early-return and reaches the container
	// lookup, which is what this test exercises.
	driver := &opv1.ClusterCSIDriver{
		ObjectMeta: metav1.ObjectMeta{Name: driverName},
	}
	lister := newFakeClusterCSIDriverLister(t, driver)
	hook := WithSecretRotationDaemonSetHook(lister, driverName)

	daemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "secrets-store-csi-driver-node", Namespace: "openshift-cluster-csi-drivers"},
	}

	if err := hook(&opv1.OperatorSpec{}, daemonSet); err == nil {
		t.Fatalf("expected an error when the csi-driver container is missing, got nil")
	}
}
