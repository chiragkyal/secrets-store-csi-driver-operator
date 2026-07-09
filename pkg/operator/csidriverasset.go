package operator

import (
	"fmt"

	opv1 "github.com/openshift/api/operator/v1"
	operatorv1listers "github.com/openshift/client-go/operator/listers/operator/v1"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	storagev1listers "k8s.io/client-go/listers/storage/v1"
	"sigs.k8s.io/yaml"
)

// csidriverAssetName is the asset file name for the CSIDriver manifest, used to
// special-case it in withSecretsStoreCSIDriverAsset below.
const csidriverAssetName = "csidriver.yaml"

// withSecretsStoreCSIDriverAsset wraps a base AssetFunc so that, for csidriver.yaml
// specifically, the returned bytes reflect the resolved secretsStore rotation and
// tokenRequests configuration read from the live ClusterCSIDriver, instead of the
// fully-static base manifest. All other asset names pass through unchanged.
//
// When the resolved tokenRequests configuration is "Managed", spec.tokenRequests is
// set from the resolved audiences. When it is "Unmanaged" or omitted, this preserves
// whatever tokenRequests already exist on the live CSIDriver object (read via
// liveCSIDriverLister) instead of wiping them -- this is the upgrade-safety guarantee
// for clusters with pre-existing, externally-configured tokenRequests (specs.md
// FR-006, User Story 3).
func withSecretsStoreCSIDriverAsset(base resourceapply.AssetFunc, clusterCSIDriverLister operatorv1listers.ClusterCSIDriverLister, liveCSIDriverLister storagev1listers.CSIDriverLister) resourceapply.AssetFunc {
	return func(name string) ([]byte, error) {
		objBytes, err := base(name)
		if err != nil {
			return nil, err
		}
		if name != csidriverAssetName {
			return objBytes, nil
		}

		spec, err := getClusterCSIDriverSpec(clusterCSIDriverLister)
		if err != nil {
			return nil, fmt.Errorf("unable to get ClusterCSIDriver %q for %s: %w", providerName, csidriverAssetName, err)
		}

		driver := resourceread.ReadCSIDriverV1OrDie(objBytes)
		rotation, tokenRequests := ResolveSecretsStoreConfig(spec)

		requiresRepublish := rotation.Enabled
		driver.Spec.RequiresRepublish = &requiresRepublish

		if tokenRequests.Managed {
			driver.Spec.TokenRequests = toStorageTokenRequests(tokenRequests.Audiences)
		} else {
			preserved, err := getLiveTokenRequests(liveCSIDriverLister)
			if err != nil {
				return nil, fmt.Errorf("unable to get live CSIDriver %q for tokenRequests preservation: %w", providerName, err)
			}
			driver.Spec.TokenRequests = preserved
		}

		mutated, err := yaml.Marshal(driver)
		if err != nil {
			return nil, fmt.Errorf("unable to marshal mutated %s: %w", csidriverAssetName, err)
		}
		return mutated, nil
	}
}

// getLiveTokenRequests returns the tokenRequests currently present on the live
// storage.k8s.io/v1 CSIDriver object (the same singleton name as the operator's own
// ClusterCSIDriver, providerName), or nil if the object has not been created yet.
func getLiveTokenRequests(lister storagev1listers.CSIDriverLister) ([]storagev1.TokenRequest, error) {
	live, err := lister.Get(providerName)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return live.Spec.TokenRequests, nil
}

// getClusterCSIDriverSpec returns the live ClusterCSIDriver spec via the lister, or
// nil if the object has not been observed yet (e.g. very early during startup before
// the informer's initial list completes) -- callers should treat a nil spec the same
// as ResolveSecretsStoreConfig treats a nil argument (built-in defaults).
func getClusterCSIDriverSpec(lister operatorv1listers.ClusterCSIDriverLister) (*opv1.ClusterCSIDriverSpec, error) {
	driver, err := lister.Get(providerName)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &driver.Spec, nil
}

// toStorageTokenRequests converts the resolved SecretsStore token-audience list into
// the storagev1.CSIDriver's TokenRequest shape.
func toStorageTokenRequests(audiences []opv1.SecretsStoreTokenRequest) []storagev1.TokenRequest {
	if audiences == nil {
		return nil
	}
	result := make([]storagev1.TokenRequest, 0, len(audiences))
	for _, a := range audiences {
		tr := storagev1.TokenRequest{}
		if a.Audience != nil {
			tr.Audience = *a.Audience
		}
		if a.ExpirationSeconds > 0 {
			exp := int64(a.ExpirationSeconds)
			tr.ExpirationSeconds = &exp
		}
		result = append(result, tr)
	}
	return result
}
