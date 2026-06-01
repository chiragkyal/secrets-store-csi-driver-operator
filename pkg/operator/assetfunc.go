package operator

import (
	"bytes"
	"encoding/json"

	opv1 "github.com/openshift/api/operator/v1"
	operatorlister "github.com/openshift/client-go/operator/listers/operator/v1"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	storagev1 "k8s.io/api/storage/v1"
	storagev1listers "k8s.io/client-go/listers/storage/v1"
	"k8s.io/klog/v2"

	"github.com/openshift/secrets-store-csi-driver-operator/assets"
)

const (
	csiDriverAssetFile = "csidriver.yaml"
)

// dynamicAssetFunc returns an AssetFunc that performs namespace replacement for all
// files, and additionally enriches csidriver.yaml with requiresRepublish and tokenRequests
// based on the ClusterCSIDriver configuration.
func dynamicAssetFunc(
	namespace string,
	clusterCSIDriverLister operatorlister.ClusterCSIDriverLister,
	csiDriverLister storagev1listers.CSIDriverLister,
) resourceapply.AssetFunc {
	return func(name string) ([]byte, error) {
		content, err := assets.ReadFile(name)
		if err != nil {
			panic(err)
		}
		content = bytes.ReplaceAll(content, []byte(namespaceKey), []byte(namespace))

		if name == csiDriverAssetFile {
			return enrichCSIDriverYAML(content, clusterCSIDriverLister, csiDriverLister)
		}

		return content, nil
	}
}

// enrichCSIDriverYAML parses the base CSIDriver YAML template and sets
// requiresRepublish and tokenRequests based on ClusterCSIDriver configuration.
func enrichCSIDriverYAML(
	baseContent []byte,
	clusterCSIDriverLister operatorlister.ClusterCSIDriverLister,
	csiDriverLister storagev1listers.CSIDriverLister,
) ([]byte, error) {
	csiDriver := resourceread.ReadCSIDriverV1OrDie(baseContent)

	requiresRepublish := true
	var tokenRequests []storagev1.TokenRequest

	ccd, err := clusterCSIDriverLister.Get(providerName)
	if err != nil {
		klog.V(4).InfoS("failed to get ClusterCSIDriver for CSIDriver enrichment, using defaults", "error", err)
	} else {
		requiresRepublish, tokenRequests = getCSIDriverConfig(ccd, csiDriverLister)
	}

	csiDriver.Spec.RequiresRepublish = &requiresRepublish
	if len(tokenRequests) > 0 {
		csiDriver.Spec.TokenRequests = tokenRequests
	}

	return marshalCSIDriverToYAML(csiDriver)
}

// getCSIDriverConfig reads the ClusterCSIDriver and returns the desired
// requiresRepublish value and tokenRequests list for the storage.k8s.io CSIDriver object.
func getCSIDriverConfig(ccd *opv1.ClusterCSIDriver, csiDriverLister storagev1listers.CSIDriverLister) (bool, []storagev1.TokenRequest) {
	requiresRepublish := true
	var tokenRequests []storagev1.TokenRequest

	if ccd.Spec.DriverConfig.DriverType != opv1.SecretsStoreDriverType {
		return requiresRepublish, tokenRequests
	}

	ss := ccd.Spec.DriverConfig.SecretsStore
	if ss == nil {
		return requiresRepublish, tokenRequests
	}

	if ss.SecretRotation != nil && ss.SecretRotation.Policy == opv1.SecretRotationDisabled {
		requiresRepublish = false
	}

	tokenRequests = resolveTokenRequests(ss.TokenRequests, csiDriverLister)

	return requiresRepublish, tokenRequests
}

// resolveTokenRequests determines the tokenRequests to set on the CSIDriver based on
// the TokenRequestsPolicy. When Unmanaged (or nil), it preserves the existing
// tokenRequests from the live CSIDriver object. When Managed, it uses the audiences
// from ClusterCSIDriver.
func resolveTokenRequests(tr *opv1.SecretsStoreTokenRequests, csiDriverLister storagev1listers.CSIDriverLister) []storagev1.TokenRequest {
	if tr == nil || tr.Policy == "" || tr.Policy == opv1.TokenRequestsUnmanaged {
		return getExistingTokenRequests(csiDriverLister)
	}

	var tokenRequests []storagev1.TokenRequest
	for _, audience := range tr.Audiences {
		a := ""
		if audience.Audience != nil {
			a = *audience.Audience
		}
		tokenReq := storagev1.TokenRequest{
			Audience: a,
		}
		if audience.ExpirationSeconds != nil {
			tokenReq.ExpirationSeconds = audience.ExpirationSeconds
		}
		tokenRequests = append(tokenRequests, tokenReq)
	}
	return tokenRequests
}

// getExistingTokenRequests reads the current CSIDriver object from the cluster
// and returns its tokenRequests, preserving any user-configured values.
func getExistingTokenRequests(csiDriverLister storagev1listers.CSIDriverLister) []storagev1.TokenRequest {
	if csiDriverLister == nil {
		return nil
	}
	existing, err := csiDriverLister.Get(providerName)
	if err != nil {
		klog.V(4).InfoS("failed to get existing CSIDriver, returning empty tokenRequests", "error", err)
		return nil
	}
	return existing.Spec.TokenRequests
}

// marshalCSIDriverToYAML serializes a CSIDriver object to JSON (which is valid YAML).
func marshalCSIDriverToYAML(csiDriver *storagev1.CSIDriver) ([]byte, error) {
	return json.Marshal(csiDriver)
}
