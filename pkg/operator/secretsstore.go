package operator

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	opv1 "github.com/openshift/api/operator/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

// defaultRotationEnabled and defaultRotationInterval match the values hardcoded
// in assets/node.yaml prior to https://github.com/openshift/enhancements/pull/2012 feature
// and are returned whenever the administrator has expressed no opinion,
// so existing clusters see no change in behavior on upgrade.
const (
	defaultRotationEnabled  = true
	defaultRotationInterval = 2 * time.Minute
)

// clusterCSIDriverGetter is the minimal read access to a ClusterCSIDriver by
// name that getClusterCSIDriverConfig needs. It is satisfied both by a
// dedicated typed operatorv1listers.ClusterCSIDriverLister and by
// *typedClusterCSIDriverLister below.
type clusterCSIDriverGetter interface {
	Get(name string) (*opv1.ClusterCSIDriver, error)
}

// dynamicClusterCSIDriverLister is the minimal read access
// *typedClusterCSIDriverLister needs from the cache.GenericLister backing a
// dynamic informer.
type dynamicClusterCSIDriverLister interface {
	Get(name string) (runtime.Object, error)
}

// typedClusterCSIDriverLister adapts the cache.GenericLister backing the
// shared dynamic ClusterCSIDriver informer -- the same informer/cache the
// GenericOperatorClient already watches -- into a typed Get, so callers
// work with *opv1.ClusterCSIDriver directly, exactly as they would with a
// dedicated operatorv1listers.ClusterCSIDriverLister, without this operator
// starting a second, independent watch on the same singleton object.
type typedClusterCSIDriverLister struct {
	dynamicLister dynamicClusterCSIDriverLister
}

// newTypedClusterCSIDriverLister wraps dynamicLister -- the Lister of the
// dynamic informer returned by dynamicInformers.ForResource(gvr), which
// GenericOperatorClient's own watch on ClusterCSIDriver already backs -- to
// return typed *opv1.ClusterCSIDriver objects.
func newTypedClusterCSIDriverLister(dynamicLister dynamicClusterCSIDriverLister) *typedClusterCSIDriverLister {
	return &typedClusterCSIDriverLister{dynamicLister: dynamicLister}
}

// Get retrieves the named ClusterCSIDriver and converts it from the
// *unstructured.Unstructured the dynamic lister's cache stores it as to a
// typed *opv1.ClusterCSIDriver, mirroring extractOperatorSpec/
// extractOperatorStatus in starter.go, which perform the same conversion
// for the same underlying object. A NotFound error from the dynamic lister
// is returned unchanged so callers can keep using apierrors.IsNotFound.
func (l *typedClusterCSIDriverLister) Get(name string) (*opv1.ClusterCSIDriver, error) {
	obj, err := l.dynamicLister.Get(name)
	if err != nil {
		return nil, err
	}
	unstr, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("unexpected type %T, expected *unstructured.Unstructured", obj)
	}
	driver := &opv1.ClusterCSIDriver{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstr.Object, driver); err != nil {
		return nil, fmt.Errorf("unable to convert to ClusterCSIDriver: %w", err)
	}
	return driver, nil
}

// getClusterCSIDriverConfig returns the driverConfig of the ClusterCSIDriver named name.
func getClusterCSIDriverConfig(clusterCSIDriverLister clusterCSIDriverGetter, name string) (opv1.CSIDriverConfigSpec, error) {
	driver, err := clusterCSIDriverLister.Get(name)
	if apierrors.IsNotFound(err) {
		logDriverConfigIfChanged(name, opv1.CSIDriverConfigSpec{})
		return opv1.CSIDriverConfigSpec{}, nil
	}
	if err != nil {
		return opv1.CSIDriverConfigSpec{}, fmt.Errorf("failed to get ClusterCSIDriver %q: %w", name, err)
	}

	logDriverConfigIfChanged(name, driver.Spec.DriverConfig)

	return driver.Spec.DriverConfig, nil
}

// driverConfigChangeTracker remembers the last driverConfig observed per
// ClusterCSIDriver name so logDriverConfigIfChanged can log at Info level
// only when the effective configuration actually changes, instead of on
// every sync triggered by an unrelated resync or a status-only update.
var driverConfigChangeTracker = struct {
	mu   sync.Mutex
	seen map[string]opv1.CSIDriverConfigSpec
}{seen: map[string]opv1.CSIDriverConfigSpec{}}

func logDriverConfigIfChanged(name string, driverConfig opv1.CSIDriverConfigSpec) {
	driverConfigChangeTracker.mu.Lock()
	defer driverConfigChangeTracker.mu.Unlock()

	previous, seenBefore := driverConfigChangeTracker.seen[name]
	if seenBefore && reflect.DeepEqual(previous, driverConfig) {
		return
	}
	driverConfigChangeTracker.seen[name] = driverConfig
	if !seenBefore {
		klog.Infof("ClusterCSIDriver %q driverConfig observed: %+v", name, driverConfig)
		return
	}
	klog.Infof("ClusterCSIDriver %q driverConfig changed: %+v -> %+v", name, previous, driverConfig)
}

// getSecretRotationConfig computes the effective secret-rotation enable flag
// and poll interval for the Secrets Store CSI driver from a
// ClusterCSIDriver's driverConfig.
//
//   - driverType != SecretsStore (including the unset/empty value): defaults
//   - driverType == SecretsStore but secretsStore is the zero value: defaults
//   - secretRotation is the zero value (type == ""): defaults
//   - secretRotation.type == None: rotation disabled
//   - secretRotation.type == Custom, custom.minimumRefreshAge > 0: that interval
//   - secretRotation.type == Custom, custom.minimumRefreshAge == 0 (omitted): default interval
func getSecretRotationConfig(driverConfig opv1.CSIDriverConfigSpec) (enabled bool, interval time.Duration) {
	if driverConfig.DriverType != opv1.SecretsStoreDriverType {
		return defaultRotationEnabled, defaultRotationInterval
	}

	rotation := driverConfig.SecretsStore.SecretRotation
	switch rotation.Type {
	case opv1.SecretRotationNone:
		return false, defaultRotationInterval
	case opv1.SecretRotationCustom:
		if rotation.Custom.MinimumRefreshAge > 0 {
			return true, time.Duration(rotation.Custom.MinimumRefreshAge) * time.Second
		}
		return true, defaultRotationInterval
	default:
		// Zero value (type == "") or any unrecognized future value: no
		// opinion expressed, keep the defaults.
		return defaultRotationEnabled, defaultRotationInterval
	}
}

// getRequiresRepublish returns the desired CSIDriver.spec.requiresRepublish
// value. It mirrors secretRotation's effective enable state:
// true when rotation is enabled, false otherwise.
func getRequiresRepublish(driverConfig opv1.CSIDriverConfigSpec) *bool {
	enabled, _ := getSecretRotationConfig(driverConfig)
	return ptr.To(enabled)
}

// getEffectiveTokenRequests computes the desired CSIDriver.spec.tokenRequests
// from driverConfig, given the tokenRequests currently set on the live  CSIDriver object.
//
//   - driverType != SecretsStore (including the unset/empty value): preserve existing
//   - driverType == SecretsStore, secretsStore is the zero value: preserve existing
//   - tokenRequests is the zero value (type == "", i.e. omitted): preserve existing
//   - type == Unmanaged: preserve existing (the managed field is not used)
//   - type == Managed, managed.audiences == nil: preserve existing.
//   - type == Managed, managed.audiences is a non-nil pointer to a slice: return
//     exactly that slice mapped to []storagev1.TokenRequest -- INCLUDING the
//     empty-slice case, which explicitly clears all tokenRequests.
func getEffectiveTokenRequests(driverConfig opv1.CSIDriverConfigSpec, existing []storagev1.TokenRequest) []storagev1.TokenRequest {
	if driverConfig.DriverType != opv1.SecretsStoreDriverType {
		return existing
	}

	tokenRequests := driverConfig.SecretsStore.TokenRequests
	switch tokenRequests.Type {
	case opv1.TokenRequestsManaged:
		if tokenRequests.Managed.Audiences == nil {
			return existing
		}
		audiences := *tokenRequests.Managed.Audiences
		result := make([]storagev1.TokenRequest, 0, len(audiences))
		for _, audience := range audiences {
			tr := storagev1.TokenRequest{Audience: ptr.Deref(audience.Audience, "")}
			if audience.ExpirationSeconds != 0 {
				tr.ExpirationSeconds = ptr.To(int64(audience.ExpirationSeconds))
			}
			result = append(result, tr)
		}
		return result
	case opv1.TokenRequestsUnmanaged:
		return existing
	default:
		// Zero value (type == ""): omitted, preserve existing.
		return existing
	}
}
