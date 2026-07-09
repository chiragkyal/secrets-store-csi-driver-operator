package operator

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	opv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivernodeservicecontroller"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

// csiDriverContainerName is the name of the driver container in node.yaml
// whose args carry the rotation flags this hook manages.
const csiDriverContainerName = "csi-driver"

const (
	// defaultRotationEnabled and defaultRotationPollInterval match the
	// values hardcoded in assets/node.yaml prior to this feature
	// (--enable-secret-rotation=true, --rotation-poll-interval=2m) and are
	// returned whenever the administrator has expressed no opinion, so
	// existing clusters see no change in behavior (FR-003, FR-012).
	defaultRotationEnabled      = true
	defaultRotationPollInterval = 2 * time.Minute

	// defaultCustomRefreshInterval is the operator-chosen default applied
	// when secretRotation.type is Custom but custom.minimumRefreshAge is
	// omitted, matching the field's documented "reasonable default...
	// currently 120s" in the upstream API.
	defaultCustomRefreshInterval = 120 * time.Second
)

// getSecretRotationConfig computes the effective secret-rotation enable flag
// and poll interval for the Secrets Store CSI driver from a
// ClusterCSIDriver's driverConfig (FR-001, FR-002, FR-003, FR-011).
//
// Bounds validation (1s-~1yr on minimumRefreshAge) is intentionally NOT
// performed here: it is enforced by the upstream CRD schema, so any value
// reaching this function has already been admitted by the API server.
//
// Nil-safety matrix (driverType, secretsStore, and secretRotation are all Go
// value types with `omitzero`/`omitempty` JSON semantics, not pointers --
// "omitted" means the zero value):
//   - driverType != SecretsStore (including the unset/empty value): defaults
//   - driverType == SecretsStore but secretsStore is the zero value: defaults
//   - secretRotation is the zero value (type == ""): defaults
//   - secretRotation.type == None: rotation disabled
//   - secretRotation.type == Custom, custom.minimumRefreshAge > 0: that interval
//   - secretRotation.type == Custom, custom.minimumRefreshAge == 0 (omitted): default interval
func getSecretRotationConfig(driverConfig opv1.CSIDriverConfigSpec) (enabled bool, interval time.Duration) {
	if driverConfig.DriverType != opv1.SecretsStoreDriverType {
		return defaultRotationEnabled, defaultRotationPollInterval
	}

	rotation := driverConfig.SecretsStore.SecretRotation
	switch rotation.Type {
	case opv1.SecretRotationNone:
		return false, defaultRotationPollInterval
	case opv1.SecretRotationCustom:
		if rotation.Custom.MinimumRefreshAge > 0 {
			return true, time.Duration(rotation.Custom.MinimumRefreshAge) * time.Second
		}
		return true, defaultCustomRefreshInterval
	default:
		// Zero value (type == "") or any unrecognized future value: no
		// opinion expressed, keep the defaults.
		return defaultRotationEnabled, defaultRotationPollInterval
	}
}

// formatRotationInterval renders d as a --rotation-poll-interval value.
// time.Duration.String() always includes a trailing zero-valued unit for
// whole-minute/hour durations (e.g. "2m0s" for exactly two minutes), which
// does not match the literal "2m" string historically hardcoded in
// assets/node.yaml and, left unfixed, would cause a needless DaemonSet
// diff/rollout for every cluster that has not configured
// driverConfig.secretsStore -- a regression against FR-003/FR-012/SC-005's
// "zero behavior change for unconfigured clusters" requirement. Whole-minute
// durations are rendered as "Nm"; anything else falls back to the standard
// time.Duration.String() (e.g. "1m30s"), which is a valid Go duration string
// but simply doesn't have a historical literal to preserve.
func formatRotationInterval(d time.Duration) string {
	if d > 0 && d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int64(d/time.Minute))
	}
	return d.String()
}

// setArg finds the element of args whose value starts with prefix and
// replaces it with prefix+value, in place. If no element matches, prefix+value
// is appended. All other elements are left unchanged and in their original
// order. Used to update individual flag arguments (e.g.
// "--rotation-poll-interval=") on a container's args without disturbing
// unrelated flags.
func setArg(args []string, prefix, value string) []string {
	newArg := prefix + value
	for i, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			args[i] = newArg
			return args
		}
	}
	return append(args, newArg)
}

// WithSecretRotationDaemonSetHook returns a DaemonSetHookFunc that sets the
// csi-driver container's "--enable-secret-rotation=" and
// "--rotation-poll-interval=" args from the live ClusterCSIDriver named
// driverName, read via clusterCSIDriverLister.
//
// Modeled directly on csidrivernodeservicecontroller.WithCABundleDaemonSetHook
// (vendor/.../csidrivernodeservicecontroller/helpers.go): the hook closes
// over its own lister rather than relying on its *opv1.OperatorSpec
// parameter, which only carries the generic OperatorSpec fields, never
// ClusterCSIDriverSpec.DriverConfig.
//
// clusterCSIDriverLister is a cache.GenericLister for the clustercsidrivers
// resource (dynamicInformers.ForResource(gvr).Lister() in starter.go) rather
// than a typed lister, because no typed ClusterCSIDriver informer/lister is
// constructed elsewhere in this operator; reusing the existing dynamic
// informer avoids adding a second, redundant informer for the same resource.
func WithSecretRotationDaemonSetHook(
	clusterCSIDriverLister cache.GenericLister,
	driverName string,
) csidrivernodeservicecontroller.DaemonSetHookFunc {
	return func(_ *opv1.OperatorSpec, daemonSet *appsv1.DaemonSet) error {
		uncastObj, err := clusterCSIDriverLister.Get(driverName)
		if apierrors.IsNotFound(err) {
			// The ClusterCSIDriver is not created yet: leave the
			// DaemonSet's rotation args untouched (they already carry
			// the static, pre-feature defaults from node.yaml).
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to get ClusterCSIDriver %q: %w", driverName, err)
		}

		unstructuredObj, ok := uncastObj.(*unstructured.Unstructured)
		if !ok {
			return fmt.Errorf("unexpected type %T for ClusterCSIDriver %q", uncastObj, driverName)
		}

		driver := &opv1.ClusterCSIDriver{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, driver); err != nil {
			return fmt.Errorf("unable to convert to ClusterCSIDriver: %w", err)
		}

		enabled, interval := getSecretRotationConfig(driver.Spec.DriverConfig)

		containers := daemonSet.Spec.Template.Spec.Containers
		for i := range containers {
			if containers[i].Name != csiDriverContainerName {
				continue
			}
			containers[i].Args = setArg(containers[i].Args, "--enable-secret-rotation=", strconv.FormatBool(enabled))
			containers[i].Args = setArg(containers[i].Args, "--rotation-poll-interval=", formatRotationInterval(interval))
			return nil
		}

		return fmt.Errorf("container %q not found in DaemonSet %s/%s", csiDriverContainerName, daemonSet.Namespace, daemonSet.Name)
	}
}
