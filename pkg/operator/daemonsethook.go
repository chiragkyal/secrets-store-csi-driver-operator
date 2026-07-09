package operator

import (
	"fmt"
	"strconv"
	"strings"

	opv1 "github.com/openshift/api/operator/v1"
	operatorv1listers "github.com/openshift/client-go/operator/listers/operator/v1"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivernodeservicecontroller"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

const (
	csiDriverContainerName    = "csi-driver"
	rotationEnabledArgPrefix  = "--enable-secret-rotation="
	rotationIntervalArgPrefix = "--rotation-poll-interval="
)

// withSecretsStoreRotationDaemonSetHook returns a DaemonSetHookFunc that sets the
// csi-driver container's --enable-secret-rotation and --rotation-poll-interval args
// from the resolved secretsStore rotation configuration (see ResolveSecretsStoreConfig),
// following the same closure-captured-lister pattern as
// csidrivernodeservicecontroller.WithCABundleDaemonSetHook.
//
// The interval is formatted as "<seconds>s" (a valid Go duration string), which is
// functionally -- but not byte-- identical to the "2m" hardcoded in assets/node.yaml
// today when no driverConfig is configured (both parse to the same 120s duration).
func withSecretsStoreRotationDaemonSetHook(clusterCSIDriverLister operatorv1listers.ClusterCSIDriverLister) csidrivernodeservicecontroller.DaemonSetHookFunc {
	return func(_ *opv1.OperatorSpec, daemonSet *appsv1.DaemonSet) error {
		spec, err := getClusterCSIDriverSpec(clusterCSIDriverLister)
		if err != nil {
			return fmt.Errorf("unable to get ClusterCSIDriver %q for rotation args: %w", providerName, err)
		}
		rotation, _ := ResolveSecretsStoreConfig(spec)

		container, err := findContainer(daemonSet, csiDriverContainerName)
		if err != nil {
			return err
		}

		container.Args = setArgPrefix(container.Args, rotationEnabledArgPrefix, strconv.FormatBool(rotation.Enabled))
		container.Args = setArgPrefix(container.Args, rotationIntervalArgPrefix, fmt.Sprintf("%ds", rotation.RotationPollIntervalSeconds))

		return nil
	}
}

// findContainer returns a pointer to the named container within the DaemonSet's pod
// template so callers can mutate it in place, or an error if not found.
func findContainer(daemonSet *appsv1.DaemonSet, name string) (*corev1.Container, error) {
	containers := daemonSet.Spec.Template.Spec.Containers
	for i := range containers {
		if containers[i].Name == name {
			return &containers[i], nil
		}
	}
	return nil, fmt.Errorf("container %q not found in daemonset %s/%s", name, daemonSet.Namespace, daemonSet.Name)
}

// setArgPrefix replaces the first arg matching the given "--flag=" prefix with
// prefix+value, or appends a new arg with that prefix+value if none is found.
func setArgPrefix(args []string, prefix, value string) []string {
	newArg := prefix + value
	for i, a := range args {
		if strings.HasPrefix(a, prefix) {
			args[i] = newArg
			return args
		}
	}
	return append(args, newArg)
}
