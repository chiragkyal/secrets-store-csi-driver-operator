package operator

import (
	"errors"
	"strings"
	"testing"

	opv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// fakeClusterCSIDriverLister is a minimal hand-written fake for
// operatorv1listers.ClusterCSIDriverLister, following this repo's convention of
// hand-written fakes over third-party mocking frameworks.
type fakeClusterCSIDriverLister struct {
	driver *opv1.ClusterCSIDriver
	err    error
}

func (f *fakeClusterCSIDriverLister) List(selector labels.Selector) ([]*opv1.ClusterCSIDriver, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.driver == nil {
		return nil, nil
	}
	return []*opv1.ClusterCSIDriver{f.driver}, nil
}

func (f *fakeClusterCSIDriverLister) Get(name string) (*opv1.ClusterCSIDriver, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.driver == nil {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "clustercsidrivers"}, name)
	}
	return f.driver, nil
}

const testCSIDriverYAML = `apiVersion: storage.k8s.io/v1
kind: CSIDriver
metadata:
  name: secrets-store.csi.k8s.io
spec:
  podInfoOnMount: true
  attachRequired: false
  fsGroupPolicy: File
  volumeLifecycleModes:
  - Ephemeral
`

func baseAssetFunc(name string) ([]byte, error) {
	if name != csidriverAssetName {
		return nil, errors.New("unexpected asset name in test: " + name)
	}
	return []byte(testCSIDriverYAML), nil
}

func TestWithSecretsStoreCSIDriverAsset_PassThrough(t *testing.T) {
	base := func(name string) ([]byte, error) {
		return []byte("unrelated content for " + name), nil
	}
	wrapped := withSecretsStoreCSIDriverAsset(base, &fakeClusterCSIDriverLister{})

	got, err := wrapped("node_sa.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != "unrelated content for node_sa.yaml" {
		t.Errorf("expected pass-through content for non-csidriver.yaml asset, got %q", string(got))
	}
}

func TestWithSecretsStoreCSIDriverAsset_NoClusterCSIDriverYet(t *testing.T) {
	wrapped := withSecretsStoreCSIDriverAsset(baseAssetFunc, &fakeClusterCSIDriverLister{})

	got, err := wrapped(csidriverAssetName)
	if err != nil {
		t.Fatalf("unexpected error when ClusterCSIDriver not found: %v", err)
	}
	driver := decodeTestCSIDriver(t, got)
	if driver.Spec.RequiresRepublish == nil || !*driver.Spec.RequiresRepublish {
		t.Errorf("expected requiresRepublish=true (default) when ClusterCSIDriver not found, got %v", driver.Spec.RequiresRepublish)
	}
	if len(driver.Spec.TokenRequests) != 0 {
		t.Errorf("expected no tokenRequests when ClusterCSIDriver not found, got %d", len(driver.Spec.TokenRequests))
	}
}

func TestWithSecretsStoreCSIDriverAsset_RotationMirroring(t *testing.T) {
	cases := []struct {
		name                    string
		rotationType            opv1.SecretRotationType
		expectRequiresRepublish bool
	}{
		{name: "omitted rotation defaults to true", rotationType: "", expectRequiresRepublish: true},
		{name: "type Custom sets true", rotationType: opv1.SecretRotationCustom, expectRequiresRepublish: true},
		{name: "type None sets false", rotationType: opv1.SecretRotationNone, expectRequiresRepublish: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			driverCR := &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: opv1.SecretsStoreSecretRotation{Type: tc.rotationType},
						},
					},
				},
			}
			wrapped := withSecretsStoreCSIDriverAsset(baseAssetFunc, &fakeClusterCSIDriverLister{driver: driverCR})

			got, err := wrapped(csidriverAssetName)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			driver := decodeTestCSIDriver(t, got)
			if driver.Spec.RequiresRepublish == nil || *driver.Spec.RequiresRepublish != tc.expectRequiresRepublish {
				t.Errorf("expected requiresRepublish=%v, got %v", tc.expectRequiresRepublish, driver.Spec.RequiresRepublish)
			}
		})
	}
}

func TestWithSecretsStoreCSIDriverAsset_ManagedTokenRequests(t *testing.T) {
	audience := "sts.amazonaws.com"
	driverCR := &opv1.ClusterCSIDriver{
		ObjectMeta: metav1.ObjectMeta{Name: providerName},
		Spec: opv1.ClusterCSIDriverSpec{
			DriverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					TokenRequests: opv1.SecretsStoreTokenRequests{
						Type: opv1.TokenRequestsManaged,
						Managed: opv1.ManagedTokenRequests{
							Audiences: &[]opv1.SecretsStoreTokenRequest{
								{Audience: &audience, ExpirationSeconds: 3600},
							},
						},
					},
				},
			},
		},
	}
	wrapped := withSecretsStoreCSIDriverAsset(baseAssetFunc, &fakeClusterCSIDriverLister{driver: driverCR})

	got, err := wrapped(csidriverAssetName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	driver := decodeTestCSIDriver(t, got)
	if len(driver.Spec.TokenRequests) != 1 {
		t.Fatalf("expected 1 tokenRequest, got %d", len(driver.Spec.TokenRequests))
	}
	if driver.Spec.TokenRequests[0].Audience != audience {
		t.Errorf("expected audience %q, got %q", audience, driver.Spec.TokenRequests[0].Audience)
	}
	if driver.Spec.TokenRequests[0].ExpirationSeconds == nil || *driver.Spec.TokenRequests[0].ExpirationSeconds != 3600 {
		t.Errorf("expected expirationSeconds=3600, got %v", driver.Spec.TokenRequests[0].ExpirationSeconds)
	}
}

func TestWithSecretsStoreCSIDriverAsset_ListerError(t *testing.T) {
	wrapped := withSecretsStoreCSIDriverAsset(baseAssetFunc, &fakeClusterCSIDriverLister{err: errors.New("boom")})

	_, err := wrapped(csidriverAssetName)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected error wrapping lister failure, got %v", err)
	}
}

func decodeTestCSIDriver(t *testing.T, b []byte) *storagev1.CSIDriver {
	t.Helper()
	return resourceread.ReadCSIDriverV1OrDie(b)
}
