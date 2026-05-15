package operator

import (
	"bytes"
	"encoding/json"

	opv1 "github.com/openshift/api/operator/v1"
	operatorlister "github.com/openshift/client-go/operator/listers/operator/v1"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/klog/v2"

	"github.com/openshift/secrets-store-csi-driver-operator/assets"
)

const (
	csiDriverAssetFile = "csidriver.yaml"
)

// dynamicAssetFunc returns an AssetFunc that performs namespace replacement for all
// files, and additionally enriches csidriver.yaml with requiresRepublish and tokenRequests
// based on the ClusterCSIDriver configuration.
func dynamicAssetFunc(namespace string, clusterCSIDriverLister operatorlister.ClusterCSIDriverLister) resourceapply.AssetFunc {
	return func(name string) ([]byte, error) {
		content, err := assets.ReadFile(name)
		if err != nil {
			panic(err)
		}
		content = bytes.ReplaceAll(content, []byte(namespaceKey), []byte(namespace))

		if name == csiDriverAssetFile {
			return enrichCSIDriverYAML(content, clusterCSIDriverLister)
		}

		return content, nil
	}
}

// enrichCSIDriverYAML parses the base CSIDriver YAML template and sets
// requiresRepublish and tokenRequests based on ClusterCSIDriver configuration.
func enrichCSIDriverYAML(baseContent []byte, lister operatorlister.ClusterCSIDriverLister) ([]byte, error) {
	csiDriver := resourceread.ReadCSIDriverV1OrDie(baseContent)

	requiresRepublish := true
	var tokenRequests []storagev1.TokenRequest

	ccd, err := lister.Get(providerName)
	if err != nil {
		klog.V(4).InfoS("failed to get ClusterCSIDriver for CSIDriver enrichment, using defaults", "error", err)
	} else {
		requiresRepublish, tokenRequests = getCSIDriverConfig(ccd)
	}

	csiDriver.Spec.RequiresRepublish = &requiresRepublish
	if len(tokenRequests) > 0 {
		csiDriver.Spec.TokenRequests = tokenRequests
	}

	return marshalCSIDriverToYAML(csiDriver)
}

// getCSIDriverConfig reads the ClusterCSIDriver and returns the desired
// requiresRepublish value and tokenRequests list for the storage.k8s.io CSIDriver object.
func getCSIDriverConfig(ccd *opv1.ClusterCSIDriver) (bool, []storagev1.TokenRequest) {
	requiresRepublish := true
	var tokenRequests []storagev1.TokenRequest

	if ccd.Spec.DriverConfig.DriverType != opv1.SecretsStoreDriverType {
		return requiresRepublish, tokenRequests
	}

	ss := ccd.Spec.DriverConfig.SecretsStore
	if ss == nil {
		return requiresRepublish, tokenRequests
	}

	if ss.SecretRotation != nil && ss.SecretRotation.Enabled != nil {
		requiresRepublish = *ss.SecretRotation.Enabled
	}

	for _, tr := range ss.TokenRequests {
		tokenReq := storagev1.TokenRequest{
			Audience: tr.Audience,
		}
		if tr.ExpirationSeconds != nil {
			tokenReq.ExpirationSeconds = tr.ExpirationSeconds
		}
		tokenRequests = append(tokenRequests, tokenReq)
	}

	return requiresRepublish, tokenRequests
}

// marshalCSIDriverToYAML serializes a CSIDriver object to JSON (which is valid YAML).
// We use JSON because the static resources controller deserializes with ReadGenericWithUnstructured
// which accepts both JSON and YAML, and JSON is simpler to produce deterministically.
func marshalCSIDriverToYAML(csiDriver *storagev1.CSIDriver) ([]byte, error) {
	return json.Marshal(csiDriver)
}
