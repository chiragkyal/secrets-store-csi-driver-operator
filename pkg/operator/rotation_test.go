package operator

import (
	"reflect"
	"testing"
	"time"

	opv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivernodeservicecontroller"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	fakekubeclient "k8s.io/client-go/kubernetes/fake"
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
				"--rotation-poll-interval=2m",
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
				"--rotation-poll-interval=2m",
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
				"--rotation-poll-interval=5m",
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

// TestCABundleAndRotationHooksCoexist regression-checks Constitution
// Principle VIII: the pre-existing WithCABundleDaemonSetHook must remain
// registered and functionally unchanged now that WithSecretRotationDaemonSetHook
// has been added to the same optionalDaemonSetHooks variadic list in
// starter.go. Applies both hooks, in the exact order they are registered in
// starter.go, to a single DaemonSet fixture, and asserts neither hook's
// mutations clobber the other's.
func TestCABundleAndRotationHooksCoexist(t *testing.T) {
	const (
		driverName         = "secrets-store.csi.k8s.io"
		configMapNamespace = "openshift-cluster-csi-drivers"
		configMapName      = "secrets-store-csi-driver-trusted-ca-bundle"
		caBundleVolumeName = "non-standard-root-system-trust-ca-bundle"
	)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: configMapNamespace},
		Data:       map[string]string{"ca-bundle.crt": "fake-ca-bundle-contents"},
	}
	fakeClient := fakekubeclient.NewSimpleClientset(cm)
	informerFactory := informers.NewSharedInformerFactory(fakeClient, 0)
	cmInformer := informerFactory.Core().V1().ConfigMaps()
	if err := cmInformer.Informer().GetIndexer().Add(cm); err != nil {
		t.Fatalf("failed to seed ConfigMap informer indexer: %v", err)
	}

	caBundleHook := csidrivernodeservicecontroller.WithCABundleDaemonSetHook(configMapNamespace, configMapName, cmInformer)

	driver := &opv1.ClusterCSIDriver{
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
	}
	rotationHook := WithSecretRotationDaemonSetHook(newFakeClusterCSIDriverLister(t, driver), driverName)

	daemonSet := newTestDaemonSet()
	// Matches node.yaml's annotation naming convention for the containers
	// WithCABundleDaemonSetHook should inject the CA bundle into.
	daemonSet.Annotations = map[string]string{
		"config.openshift.io/inject-proxy-cabundle": csiDriverContainerName,
	}

	// Apply in the exact order starter.go registers them: CA bundle hook
	// first, then the rotation hook.
	if err := caBundleHook(&opv1.OperatorSpec{}, daemonSet); err != nil {
		t.Fatalf("unexpected error from CA bundle hook: %v", err)
	}
	if err := rotationHook(&opv1.OperatorSpec{}, daemonSet); err != nil {
		t.Fatalf("unexpected error from rotation hook: %v", err)
	}

	// The CA bundle hook's mutations must have survived the rotation hook
	// running afterward.
	foundVolume := false
	for _, v := range daemonSet.Spec.Template.Spec.Volumes {
		if v.Name == caBundleVolumeName {
			foundVolume = true
			break
		}
	}
	if !foundVolume {
		t.Errorf("expected CA bundle volume %q to be present after both hooks ran", caBundleVolumeName)
	}

	container := daemonSet.Spec.Template.Spec.Containers[0]
	foundMount := false
	for _, m := range container.VolumeMounts {
		if m.Name == caBundleVolumeName {
			foundMount = true
			break
		}
	}
	if !foundMount {
		t.Errorf("expected CA bundle volume mount %q on container %q after both hooks ran", caBundleVolumeName, container.Name)
	}

	// The rotation hook's mutations must have applied correctly and must
	// not have been undone by (or interfere with) the CA bundle hook.
	expectedArgs := []string{
		"--enable-secret-rotation=true",
		"--rotation-poll-interval=5m",
	}
	if !reflect.DeepEqual(container.Args, expectedArgs) {
		t.Errorf("expected rotation args to be %v after both hooks ran, got %v", expectedArgs, container.Args)
	}
}

// TestDefaultPathMatchesPreFeatureBaseline is a dedicated regression anchor
// for FR-003/FR-012/specs.md SC-005: for any cluster that has not
// configured driverConfig.secretsStore (whether the ClusterCSIDriver does
// not exist yet, or exists with driverConfig entirely absent), the rendered
// DaemonSet args MUST be byte-for-byte identical to the pre-feature
// baseline documented in repo-assessment.md §3.2 (assets/node.yaml's
// historically hardcoded values) -- not merely "no error" or "semantically
// equivalent". This test previously caught a real defect: computing the
// default interval via time.Duration.String() rendered "2m0s" instead of
// the baseline literal "2m", which would have triggered an unintended
// DaemonSet rollout on every cluster upgrading into this feature. See
// formatRotationInterval for the fix.
func TestDefaultPathMatchesPreFeatureBaseline(t *testing.T) {
	const (
		baselineEnableRotationArg = "--enable-secret-rotation=true"
		baselinePollIntervalArg   = "--rotation-poll-interval=2m"
		driverName                = "secrets-store.csi.k8s.io"
	)

	cases := []struct {
		name   string
		driver *opv1.ClusterCSIDriver
	}{
		{
			name:   "ClusterCSIDriver does not exist yet",
			driver: nil,
		},
		{
			name: "ClusterCSIDriver exists with driverConfig entirely absent",
			driver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: driverName},
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
			expectedArgs := []string{baselineEnableRotationArg, baselinePollIntervalArg}
			if !reflect.DeepEqual(gotArgs, expectedArgs) {
				t.Fatalf(
					"rendered DaemonSet args diverge from the documented pre-feature baseline (repo-assessment.md §3.2): expected %v, got %v -- this would trigger an unintended DaemonSet rollout for every cluster that has not configured driverConfig.secretsStore",
					expectedArgs, gotArgs,
				)
			}
		})
	}
}
