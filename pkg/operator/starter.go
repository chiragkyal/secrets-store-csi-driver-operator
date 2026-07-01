package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	storagev1 "k8s.io/api/storage/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	kubeclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	sigsyaml "sigs.k8s.io/yaml"

	opv1 "github.com/openshift/api/operator/v1"
	configclient "github.com/openshift/client-go/config/clientset/versioned"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	applyoperatorv1 "github.com/openshift/client-go/operator/applyconfigurations/operator/v1"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/csi/csicontrollerset"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivernodeservicecontroller"
	goc "github.com/openshift/library-go/pkg/operator/genericoperatorclient"
	"github.com/openshift/library-go/pkg/operator/management"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"github.com/openshift/secrets-store-csi-driver-operator/assets"
)

const (
	operatorName       = "secrets-store-csi-driver-operator"
	operandName        = "secrets-store-csi-driver"
	trustedCAConfigMap = "secrets-store-csi-driver-trusted-ca-bundle"
	providerName       = "secrets-store.csi.k8s.io"
	namespaceKey       = "${NAMESPACE}"
	resync             = 20 * time.Minute
)

func RunOperator(ctx context.Context, controllerConfig *controllercmd.ControllerContext) error {
	operatorNamespace := controllerConfig.OperatorNamespace

	// Create core clientset and informers
	kubeClient := kubeclient.NewForConfigOrDie(rest.AddUserAgent(controllerConfig.KubeConfig, operatorName))
	kubeInformersForNamespaces := v1helpers.NewKubeInformersForNamespaces(kubeClient, operatorNamespace, "")
	configMapInformer := kubeInformersForNamespaces.InformersFor(operatorNamespace).Core().V1().ConfigMaps()

	// Create config clientset and informer. This is used to get the cluster ID
	configClient := configclient.NewForConfigOrDie(rest.AddUserAgent(controllerConfig.KubeConfig, operatorName))
	configInformers := configinformers.NewSharedInformerFactory(configClient, resync)

	// Create GenericOperatorclient. This is used by the library-go controllers created down below
	gvr := opv1.SchemeGroupVersion.WithResource("clustercsidrivers")
	gvk := opv1.SchemeGroupVersion.WithKind("ClusterCSIDriver")
	operatorClient, dynamicInformers, err := goc.NewClusterScopedOperatorClientWithConfigName(
		clock.RealClock{},
		controllerConfig.KubeConfig,
		gvr,
		gvk,
		providerName,
		extractOperatorSpec,
		extractOperatorStatus,
	)
	if err != nil {
		return err
	}

	dynamicClient, err := dynamic.NewForConfig(controllerConfig.KubeConfig)
	if err != nil {
		return err
	}

	csiControllerSet := csicontrollerset.NewCSIControllerSet(
		operatorClient,
		controllerConfig.EventRecorder,
	).WithLogLevelController().WithManagementStateController(
		operandName,
		true, // Set this operator as removable
	).WithConditionalStaticResourcesController(
		"SecretsStoreConditionalStaticResourcesController",
		kubeClient,
		dynamicClient,
		kubeInformersForNamespaces,
		replaceNamespaceFunc(operatorNamespace),
		[]string{
			"node_sa.yaml",
			"csidriver.yaml",
			"cabundle_cm.yaml",
			"rbac/privileged_role.yaml",
			"rbac/node_privileged_binding.yaml",
			"rbac/secretproviderclasses_role.yaml",
			"rbac/secretproviderclasses_binding.yaml",
			"network-policy/allow-ingress-to-metrics-operand.yaml",
		},
		func() bool {
			return getOperatorSyncState(operatorClient) == opv1.Managed
		},
		func() bool {
			return getOperatorSyncState(operatorClient) == opv1.Removed
		},
	).WithCSIConfigObserverController(
		"SecretsStoreDriverCSIConfigObserverController",
		configInformers,
	).WithCSIDriverNodeService(
		"SecretsStoreDriverNodeServiceController",
		replaceNamespaceFunc(operatorNamespace),
		"node.yaml",
		kubeClient,
		kubeInformersForNamespaces.InformersFor(operatorNamespace),
		nil,
		csidrivernodeservicecontroller.WithCABundleDaemonSetHook(
			operatorNamespace,
			trustedCAConfigMap,
			configMapInformer,
		),
	)

	klog.Info("Starting the informers")
	go kubeInformersForNamespaces.Start(ctx.Done())
	go dynamicInformers.Start(ctx.Done())
	go configInformers.Start(ctx.Done())

	klog.Info("Starting controllerset")
	go csiControllerSet.Run(ctx, 1)

	<-ctx.Done()

	return nil
}

func replaceNamespaceFunc(namespace string) resourceapply.AssetFunc {
	return func(name string) ([]byte, error) {
		content, err := assets.ReadFile(name)
		if err != nil {
			panic(err)
		}
		return bytes.ReplaceAll(content, []byte(namespaceKey), []byte(namespace)), nil
	}
}

// getOperatorSyncState returns the management state of the operator to determine
// how to sync conditional resources. It returns one of the following states:
//
//	Managed: resources should be synced
//	Unmanaged: resources should NOT be synced
//	Removed: resources should be deleted
//
// Errors fetching the operator state will log an error and return Unmanaged
// to avoid syncing resources when the actual state is unknown.
func getOperatorSyncState(operatorClient v1helpers.OperatorClientWithFinalizers) opv1.ManagementState {
	opSpec, _, _, err := operatorClient.GetOperatorState()
	if err != nil {
		klog.Errorf("Failed to get operator state: %v", err)
		return opv1.Unmanaged
	}
	// return the state from the operator if it's not managed
	if opSpec.ManagementState != opv1.Managed {
		return opSpec.ManagementState
	}
	meta, err := operatorClient.GetObjectMeta()
	if err != nil {
		klog.Errorf("Failed to get operator object meta: %v", err)
		return opv1.Unmanaged
	}
	// deletion timestamp is treated the same as the state being removed
	if management.IsOperatorRemovable() && meta.DeletionTimestamp != nil {
		klog.Infof("Operator deletion timestamp is set, removing conditional resources")
		return opv1.Removed
	}
	return opv1.Managed
}

// getRotationConfig derives (requiresRepublish, enableRotation, pollInterval) from
// a ClusterCSIDriverSpec. Built-in defaults (true, true, "2m0s") are applied at every
// nil/zero level so upgrade clusters with no driverConfig see identical behavior.
// Accepts the spec directly so callers and unit tests can construct it without a client.
func getRotationConfig(spec *opv1.ClusterCSIDriverSpec) (requiresRepublish bool, enableRotation bool, pollInterval string) {
	const defaultInterval = "2m0s"

	if spec == nil || spec.DriverConfig.DriverType != opv1.SecretsStoreDriverType {
		return true, true, defaultInterval
	}

	rotation := spec.DriverConfig.SecretsStore.SecretRotation
	switch rotation.Type {
	case opv1.SecretRotationNone:
		return false, false, defaultInterval
	case opv1.SecretRotationCustom:
		secs := rotation.Custom.RotationPollIntervalSeconds
		if secs <= 0 {
			return true, true, defaultInterval
		}
		return true, true, (time.Duration(secs) * time.Second).String()
	default:
		// zero-value / omitzero SecretRotation — built-in defaults
		return true, true, defaultInterval
	}
}

// csiDriverGVR is the GVR for the storage.k8s.io CSIDriver cluster-scoped resource.
var csiDriverGVR = schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "csidrivers"}

// clusterCSIDriverGVR is the GVR for the operator.openshift.io ClusterCSIDriver resource.
var clusterCSIDriverGVR = schema.GroupVersionResource{Group: "operator.openshift.io", Version: "v1", Resource: "clustercsidrivers"}

// getTokenRequests returns the desired []storagev1.TokenRequest for the CSIDriver spec.
//
//   - When tokenRequests is absent, zero-value, or type is Unmanaged: reads the existing
//     tokenRequests from the live CSIDriver object so they are preserved across reconciles
//     (upgrade safety — prevents hash change and delete+recreate for clusters that manually
//     patched tokenRequests before this feature existed).
//   - When type is Managed: converts the audiences list from ClusterCSIDriver as sole source
//     of truth. An empty audiences list explicitly clears all tokenRequests.
func getTokenRequests(ctx context.Context, spec *opv1.ClusterCSIDriverSpec, dynamicClient dynamic.Interface) ([]storagev1.TokenRequest, error) {
	isManaged := spec != nil &&
		spec.DriverConfig.DriverType == opv1.SecretsStoreDriverType &&
		spec.DriverConfig.SecretsStore.TokenRequests.Type == opv1.TokenRequestsManaged

	if !isManaged {
		return liveCSIDriverTokenRequests(ctx, dynamicClient)
	}

	audiences := spec.DriverConfig.SecretsStore.TokenRequests.Managed.Audiences
	if audiences == nil {
		// managed with nil audiences — no tokenRequests desired (distinct from empty list)
		return nil, nil
	}
	result := make([]storagev1.TokenRequest, 0, len(*audiences))
	for _, aud := range *audiences {
		tr := storagev1.TokenRequest{}
		if aud.Audience != nil {
			tr.Audience = *aud.Audience
		}
		if aud.ExpirationSeconds > 0 {
			exp := int64(aud.ExpirationSeconds)
			tr.ExpirationSeconds = &exp
		}
		result = append(result, tr)
	}
	return result, nil
}

// liveCSIDriverTokenRequests fetches the current tokenRequests from the live CSIDriver
// object. Returns nil (not an error) when the CSIDriver does not yet exist.
func liveCSIDriverTokenRequests(ctx context.Context, dynamicClient dynamic.Interface) ([]storagev1.TokenRequest, error) {
	obj, err := dynamicClient.Resource(csiDriverGVR).Get(ctx, providerName, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("liveCSIDriverTokenRequests: failed to get CSIDriver %q: %w", providerName, err)
	}
	csiDriver := &storagev1.CSIDriver{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, csiDriver); err != nil {
		return nil, fmt.Errorf("liveCSIDriverTokenRequests: failed to convert CSIDriver: %w", err)
	}
	return csiDriver.Spec.TokenRequests, nil
}

// getClusterCSIDriver reads the ClusterCSIDriver singleton from the cluster via the dynamic
// client. Returns nil without error when the object does not exist yet (e.g., first boot).
func getClusterCSIDriver(ctx context.Context, dynamicClient dynamic.Interface) (*opv1.ClusterCSIDriver, error) {
	obj, err := dynamicClient.Resource(clusterCSIDriverGVR).Get(ctx, providerName, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("getClusterCSIDriver: %w", err)
	}
	ccd := &opv1.ClusterCSIDriver{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, ccd); err != nil {
		return nil, fmt.Errorf("getClusterCSIDriver: convert: %w", err)
	}
	return ccd, nil
}

// enrichedCSIDriverAssetFunc returns a resourceapply.AssetFunc that enriches csidriver.yaml
// with the current rotation and tokenRequests configuration from ClusterCSIDriver at each
// reconcile call. All other assets are forwarded to replaceNamespaceFunc unchanged.
//
// The enrichment reads the live ClusterCSIDriver on every reconcile so that ClusterCSIDriver
// spec changes propagate to the CSIDriver object immediately (once dynamicInformers is wired
// to WithCSIDriverNodeService in T4_2, CSIDriver reconcilation also triggers on CR changes).
func enrichedCSIDriverAssetFunc(namespace string, dynamicClient dynamic.Interface) resourceapply.AssetFunc {
	return func(name string) ([]byte, error) {
		if name != "csidriver.yaml" {
			return replaceNamespaceFunc(namespace)(name)
		}

		ctx := context.Background()

		// Read base manifest
		baseBytes, err := assets.ReadFile("csidriver.yaml")
		if err != nil {
			return nil, fmt.Errorf("enrichedCSIDriverAssetFunc: read csidriver.yaml: %w", err)
		}

		// Deserialize to typed CSIDriver (sigs.k8s.io/yaml converts YAML→JSON internally)
		csiDriver := &storagev1.CSIDriver{}
		if err := sigsyaml.Unmarshal(baseBytes, csiDriver); err != nil {
			return nil, fmt.Errorf("enrichedCSIDriverAssetFunc: unmarshal: %w", err)
		}

		// Read ClusterCSIDriver; fall back to built-in defaults on read error
		ccd, err := getClusterCSIDriver(ctx, dynamicClient)
		if err != nil {
			klog.Warningf("enrichedCSIDriverAssetFunc: could not read ClusterCSIDriver, applying defaults: %v", err)
		}
		var spec *opv1.ClusterCSIDriverSpec
		if ccd != nil {
			spec = &ccd.Spec
		}

		// Apply rotation: set requiresRepublish from rotation config
		requiresRepublish, _, _ := getRotationConfig(spec)
		csiDriver.Spec.RequiresRepublish = &requiresRepublish

		// Apply tokenRequests: Managed uses CR; Unmanaged/nil preserves live CSIDriver values
		tokenRequests, err := getTokenRequests(ctx, spec, dynamicClient)
		if err != nil {
			return nil, fmt.Errorf("enrichedCSIDriverAssetFunc: getTokenRequests: %w", err)
		}
		csiDriver.Spec.TokenRequests = tokenRequests

		// Serialize back to JSON; TypeMeta (apiVersion/kind) is preserved from unmarshal
		return json.Marshal(csiDriver)
	}
}

func extractOperatorSpec(obj *unstructured.Unstructured, fieldManager string) (*applyoperatorv1.OperatorSpecApplyConfiguration, error) {
	castObj := &opv1.ClusterCSIDriver{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, castObj); err != nil {
		return nil, fmt.Errorf("unable to convert to ClusterCSIDriver: %w", err)
	}
	ret, err := applyoperatorv1.ExtractClusterCSIDriver(castObj, fieldManager)
	if err != nil {
		return nil, fmt.Errorf("unable to extract fields for %q: %w", fieldManager, err)
	}
	if ret.Spec == nil {
		return nil, nil
	}
	return &ret.Spec.OperatorSpecApplyConfiguration, nil
}
func extractOperatorStatus(obj *unstructured.Unstructured, fieldManager string) (*applyoperatorv1.OperatorStatusApplyConfiguration, error) {
	castObj := &opv1.ClusterCSIDriver{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, castObj); err != nil {
		return nil, fmt.Errorf("unable to convert to ClusterCSIDriver: %w", err)
	}
	ret, err := applyoperatorv1.ExtractClusterCSIDriverStatus(castObj, fieldManager)
	if err != nil {
		return nil, fmt.Errorf("unable to extract fields for %q: %w", fieldManager, err)
	}

	if ret.Status == nil {
		return nil, nil
	}
	return &ret.Status.OperatorStatusApplyConfiguration, nil
}
