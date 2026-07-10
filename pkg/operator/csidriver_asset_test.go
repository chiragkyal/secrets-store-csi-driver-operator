package operator

import (
	"reflect"
	"testing"

	opv1 "github.com/openshift/api/operator/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	storagelistersv1 "k8s.io/client-go/listers/storage/v1"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/yaml"
)

func boolPtr(b bool) *bool { return &b }

func stringPtr(s string) *string { return &s }

func TestGetRequiresRepublish(t *testing.T) {
	cases := []struct {
		name         string
		driverConfig opv1.CSIDriverConfigSpec
		expected     bool
	}{
		{
			name:         "nil driverConfig returns true (mirrors default-enabled rotation)",
			driverConfig: opv1.CSIDriverConfigSpec{},
			expected:     true,
		},
		{
			name: "type None returns false",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					SecretRotation: opv1.SecretsStoreSecretRotation{Type: opv1.SecretRotationNone},
				},
			},
			expected: false,
		},
		{
			name: "type Custom returns true",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					SecretRotation: opv1.SecretsStoreSecretRotation{Type: opv1.SecretRotationCustom},
				},
			},
			expected: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := getRequiresRepublish(tc.driverConfig)
			if got == nil {
				t.Fatalf("expected a non-nil *bool, got nil")
			}
			if *got != tc.expected {
				t.Errorf("expected requiresRepublish to be %v, got %v", tc.expected, *got)
			}
		})
	}
}

func TestGetTokenRequests(t *testing.T) {
	existing := []storagev1.TokenRequest{{Audience: "api://AzureADTokenExchange"}}

	cases := []struct {
		name             string
		driverConfig     opv1.CSIDriverConfigSpec
		existing         []storagev1.TokenRequest
		expected         []storagev1.TokenRequest
		expectedExplicit bool // true when expected must be a non-nil, possibly-empty slice (not just "empty-ish")
	}{
		{
			name:         "driverType != SecretsStore preserves existing",
			driverConfig: opv1.CSIDriverConfigSpec{DriverType: opv1.AWSDriverType},
			existing:     existing,
			expected:     existing,
		},
		{
			name: "driverType SecretsStore with zero-value secretsStore preserves existing",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
			},
			existing: existing,
			expected: existing,
		},
		{
			name: "tokenRequests zero value (omitted) preserves existing",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType:   opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{},
			},
			existing: existing,
			expected: existing,
		},
		{
			name:         "no driverConfig and no existing tokenRequests returns nil, no error",
			driverConfig: opv1.CSIDriverConfigSpec{},
			existing:     nil,
			expected:     nil,
		},
		{
			name: "type Unmanaged preserves existing",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					TokenRequests: opv1.SecretsStoreTokenRequests{Type: opv1.TokenRequestsUnmanaged},
				},
			},
			existing: existing,
			expected: existing,
		},
		{
			name: "type Managed with nil audiences pointer defensively preserves existing",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					TokenRequests: opv1.SecretsStoreTokenRequests{Type: opv1.TokenRequestsManaged},
				},
			},
			existing: existing,
			expected: existing,
		},
		{
			name: "type Managed with audiences returns exactly those, replacing existing",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					TokenRequests: opv1.SecretsStoreTokenRequests{
						Type: opv1.TokenRequestsManaged,
						Managed: opv1.ManagedTokenRequests{
							Audiences: &[]opv1.SecretsStoreTokenRequest{
								{Audience: stringPtr("sts.amazonaws.com"), ExpirationSeconds: 3600},
								{Audience: stringPtr("api://AzureADTokenExchange")},
							},
						},
					},
				},
			},
			existing: existing,
			expected: []storagev1.TokenRequest{
				{Audience: "sts.amazonaws.com", ExpirationSeconds: int64Ptr(3600)},
				{Audience: "api://AzureADTokenExchange"},
			},
		},
		{
			name: "type Managed with an explicit empty audiences list clears all tokenRequests",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					TokenRequests: opv1.SecretsStoreTokenRequests{
						Type: opv1.TokenRequestsManaged,
						Managed: opv1.ManagedTokenRequests{
							Audiences: &[]opv1.SecretsStoreTokenRequest{},
						},
					},
				},
			},
			existing:         existing,
			expected:         []storagev1.TokenRequest{},
			expectedExplicit: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := getTokenRequests(tc.driverConfig, tc.existing)
			if tc.expectedExplicit {
				if got == nil {
					t.Fatalf("expected an explicit empty (non-nil) slice, got nil")
				}
				if len(got) != 0 {
					t.Fatalf("expected an explicit empty slice, got %v", got)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Fatalf("expected tokenRequests to be %#v, got %#v", tc.expected, got)
			}
		})
	}
}

func int64Ptr(i int64) *int64 { return &i }

// newFakeCSIDriverLister builds a storagelistersv1.CSIDriverLister backed by
// an in-memory indexer, pre-populated with driver if non-nil.
func newFakeCSIDriverLister(t *testing.T, driver *storagev1.CSIDriver) storagelistersv1.CSIDriverLister {
	t.Helper()
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	if driver != nil {
		if err := indexer.Add(driver); err != nil {
			t.Fatalf("failed to add CSIDriver to indexer: %v", err)
		}
	}
	return storagelistersv1.NewCSIDriverLister(indexer)
}

func TestNewDynamicCSIDriverAssetFunc(t *testing.T) {
	const (
		clusterCSIDriverName = "secrets-store.csi.k8s.io"
		csiDriverName        = "secrets-store.csi.k8s.io"
	)

	baseManifest := []byte(`apiVersion: storage.k8s.io/v1
kind: CSIDriver
metadata:
  name: secrets-store.csi.k8s.io
spec:
  podInfoOnMount: true
  attachRequired: false
`)
	namespaceAssetFunc := func(name string) ([]byte, error) {
		return baseManifest, nil
	}

	cases := []struct {
		name                      string
		clusterCSIDriver          *opv1.ClusterCSIDriver
		existingCSIDriver         *storagev1.CSIDriver
		expectedRequiresRepublish *bool
		expectedTokenRequests     []storagev1.TokenRequest
	}{
		{
			name:                      "no ClusterCSIDriver leaves the base manifest unmutated",
			clusterCSIDriver:          nil,
			existingCSIDriver:         nil,
			expectedRequiresRepublish: nil,
			expectedTokenRequests:     nil,
		},
		{
			name: "no driverConfig set applies defaults (requiresRepublish true, no tokenRequests)",
			clusterCSIDriver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: clusterCSIDriverName},
			},
			existingCSIDriver:         nil,
			expectedRequiresRepublish: boolPtr(true),
			expectedTokenRequests:     nil,
		},
		{
			name: "existing tokenRequests preserved when driverConfig omits tokenRequests",
			clusterCSIDriver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: clusterCSIDriverName},
			},
			existingCSIDriver: &storagev1.CSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: csiDriverName},
				Spec: storagev1.CSIDriverSpec{
					TokenRequests: []storagev1.TokenRequest{{Audience: "api://AzureADTokenExchange"}},
				},
			},
			expectedRequiresRepublish: boolPtr(true),
			expectedTokenRequests:     []storagev1.TokenRequest{{Audience: "api://AzureADTokenExchange"}},
		},
		{
			name: "type None sets requiresRepublish false",
			clusterCSIDriver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: clusterCSIDriverName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: opv1.SecretsStoreSecretRotation{Type: opv1.SecretRotationNone},
						},
					},
				},
			},
			existingCSIDriver:         nil,
			expectedRequiresRepublish: boolPtr(false),
			expectedTokenRequests:     nil,
		},
		{
			name: "type Managed with audiences renders tokenRequests on CSIDriver manifest",
			clusterCSIDriver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: clusterCSIDriverName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							TokenRequests: opv1.SecretsStoreTokenRequests{
								Type: opv1.TokenRequestsManaged,
								Managed: opv1.ManagedTokenRequests{
									Audiences: &[]opv1.SecretsStoreTokenRequest{
										{Audience: stringPtr("sts.amazonaws.com"), ExpirationSeconds: 3600},
										{Audience: stringPtr("api://AzureADTokenExchange")},
									},
								},
							},
						},
					},
				},
			},
			existingCSIDriver: &storagev1.CSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: csiDriverName},
				Spec: storagev1.CSIDriverSpec{
					TokenRequests: []storagev1.TokenRequest{{Audience: "legacy-audience-should-be-replaced"}},
				},
			},
			expectedRequiresRepublish: boolPtr(true),
			expectedTokenRequests: []storagev1.TokenRequest{
				{Audience: "sts.amazonaws.com", ExpirationSeconds: int64Ptr(3600)},
				{Audience: "api://AzureADTokenExchange"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clusterCSIDriverLister := newFakeClusterCSIDriverLister(t, tc.clusterCSIDriver)
			csiDriverLister := newFakeCSIDriverLister(t, tc.existingCSIDriver)

			assetFunc := NewDynamicCSIDriverAssetFunc(
				namespaceAssetFunc,
				clusterCSIDriverLister,
				clusterCSIDriverName,
				csiDriverLister,
				csiDriverName,
			)

			renderedBytes, err := assetFunc("csidriver.yaml")
			if err != nil {
				t.Fatalf("unexpected error from AssetFunc: %v", err)
			}

			rendered := &storagev1.CSIDriver{}
			if err := yaml.Unmarshal(renderedBytes, rendered); err != nil {
				t.Fatalf("rendered manifest is not valid CSIDriver YAML: %v", err)
			}

			if !reflect.DeepEqual(rendered.Spec.RequiresRepublish, tc.expectedRequiresRepublish) {
				t.Errorf("expected requiresRepublish to be %v, got %v",
					derefBool(tc.expectedRequiresRepublish), derefBool(rendered.Spec.RequiresRepublish))
			}
			if !reflect.DeepEqual(rendered.Spec.TokenRequests, tc.expectedTokenRequests) {
				t.Errorf("expected tokenRequests to be %#v, got %#v", tc.expectedTokenRequests, rendered.Spec.TokenRequests)
			}
			// Base manifest fields must survive the round-trip (namespace
			// substitution / base-content preservation, per this task's
			// objective).
			if rendered.Name != "secrets-store.csi.k8s.io" {
				t.Errorf("expected name to be preserved from the base manifest, got %q", rendered.Name)
			}
			if rendered.Spec.AttachRequired == nil || *rendered.Spec.AttachRequired != false {
				t.Errorf("expected attachRequired:false to be preserved from the base manifest")
			}
		})
	}
}

func derefBool(b *bool) interface{} {
	if b == nil {
		return nil
	}
	return *b
}
