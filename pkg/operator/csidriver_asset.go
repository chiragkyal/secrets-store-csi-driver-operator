package operator

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	storagev1listers "k8s.io/client-go/listers/storage/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

// csidriverAssetName is the asset file name for the CSIDriver manifest
const csidriverAssetName = "csidriver.yaml"

// withSecretsStoreCSIDriverAsset wraps a base AssetFunc so that, for
// csidriver.yaml specifically, the returned bytes reflect the resolved
// secretsStore rotation and tokenRequests configuration read from the live
// ClusterCSIDriver, instead of the fully-static base manifest.
func withSecretsStoreCSIDriverAsset(
	base resourceapply.AssetFunc,
	clusterCSIDriverLister clusterCSIDriverGetter,
	csiDriverLister storagev1listers.CSIDriverLister,
	clusterCSIDriverName string,
) resourceapply.AssetFunc {
	return func(name string) ([]byte, error) {
		manifest, err := base(name)
		if err != nil {
			return nil, err
		}
		if name != csidriverAssetName {
			return manifest, nil
		}

		return renderSecretsStoreCSIDriver(manifest, clusterCSIDriverLister, csiDriverLister, clusterCSIDriverName)
	}
}

// renderSecretsStoreCSIDriver decodes the static csidriver.yaml manifest and
// overwrites spec.requiresRepublish and spec.tokenRequests with the values
// resolved from the live ClusterCSIDriver (and, when tokenRequests are
// Unmanaged/omitted, from the live CSIDriver object),
// returning the mutated object re-marshaled to JSON
// for the StaticResourceController to apply.
func renderSecretsStoreCSIDriver(
	manifest []byte,
	clusterCSIDriverLister clusterCSIDriverGetter,
	csiDriverLister storagev1listers.CSIDriverLister,
	clusterCSIDriverName string,
) ([]byte, error) {
	driverConfig, err := getClusterCSIDriverConfig(clusterCSIDriverLister, clusterCSIDriverName)
	if err != nil {
		return nil, err
	}

	existing, err := getExistingCSIDriverSpec(csiDriverLister, clusterCSIDriverName)
	if err != nil {
		return nil, err
	}

	csiDriver := resourceread.ReadCSIDriverV1OrDie(manifest)
	csiDriver.Spec.RequiresRepublish = getRequiresRepublish(driverConfig)
	csiDriver.Spec.TokenRequests = getEffectiveTokenRequests(driverConfig, existing.tokenRequests)

	logCSIDriverSpecIfChanged(csiDriver.Name, existing, csiDriver.Spec)

	return json.Marshal(csiDriver)
}

// existingCSIDriverSpec is the subset of a live storage.k8s.io/v1 CSIDriver
// object's spec that renderSecretsStoreCSIDriver reads and may overwrite.
type existingCSIDriverSpec struct {
	requiresRepublish *bool
	tokenRequests     []storagev1.TokenRequest
}

// getExistingCSIDriverSpec returns the requiresRepublish and tokenRequests
// currently present on the live storage.k8s.io/v1 CSIDriver object named
// name, or the zero value if the object has not been created yet.
func getExistingCSIDriverSpec(lister storagev1listers.CSIDriverLister, name string) (existingCSIDriverSpec, error) {
	existing, err := lister.Get(name)
	if apierrors.IsNotFound(err) {
		return existingCSIDriverSpec{}, nil
	}
	if err != nil {
		return existingCSIDriverSpec{}, fmt.Errorf("failed to get CSIDriver %q: %w", name, err)
	}
	return existingCSIDriverSpec{
		requiresRepublish: existing.Spec.RequiresRepublish,
		tokenRequests:     existing.Spec.TokenRequests,
	}, nil
}

// logCSIDriverSpecIfChanged logs at Info level only when the fields
// renderSecretsStoreCSIDriver owns (requiresRepublish, tokenRequests) are
// actually about to change, instead of on every sync triggered by an
// unrelated resync.
func logCSIDriverSpecIfChanged(name string, existing existingCSIDriverSpec, newSpec storagev1.CSIDriverSpec) {
	requiresRepublishChanged := ptr.Deref(existing.requiresRepublish, false) != ptr.Deref(newSpec.RequiresRepublish, false)
	tokenRequestsChanged := !reflect.DeepEqual(existing.tokenRequests, newSpec.TokenRequests)
	if !requiresRepublishChanged && !tokenRequestsChanged {
		return
	}
	klog.Infof("CSIDriver %q spec changing: requiresRepublish %v -> %v, tokenRequests %v -> %v",
		name, ptr.Deref(existing.requiresRepublish, false), ptr.Deref(newSpec.RequiresRepublish, false), existing.tokenRequests, newSpec.TokenRequests)
}
