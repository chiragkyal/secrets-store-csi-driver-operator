package operator

import (
	"fmt"
	"strings"
	"time"

	opv1 "github.com/openshift/api/operator/v1"
	operatorlister "github.com/openshift/client-go/operator/listers/operator/v1"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivernodeservicecontroller"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const (
	enableSecretRotationArg = "--enable-secret-rotation="
	rotationPollIntervalArg = "--rotation-poll-interval="

	defaultEnableSecretRotation = "true"
	defaultRotationPollInterval = "2m"

	csiDriverContainerName = "csi-driver"
)

// WithSecretRotationDaemonSetHook returns a DaemonSetHookFunc that reads the ClusterCSIDriver
// config and sets the rotation-related args on the csi-driver container.
func WithSecretRotationDaemonSetHook(
	clusterCSIDriverLister operatorlister.ClusterCSIDriverLister,
) csidrivernodeservicecontroller.DaemonSetHookFunc {
	return func(_ *opv1.OperatorSpec, ds *appsv1.DaemonSet) error {
		enableRotation := defaultEnableSecretRotation
		pollInterval := defaultRotationPollInterval

		ccd, err := clusterCSIDriverLister.Get(providerName)
		if err != nil {
			klog.V(4).InfoS("failed to get ClusterCSIDriver, using defaults", "error", err)
		} else {
			enableRotation, pollInterval = getRotationConfig(ccd)
		}

		container := findContainerByName(ds, csiDriverContainerName)
		if container == nil {
			return fmt.Errorf("container %q not found in DaemonSet", csiDriverContainerName)
		}

		setArg(container, enableSecretRotationArg, enableRotation)
		setArg(container, rotationPollIntervalArg, pollInterval)

		return nil
	}
}

// findContainerByName returns a pointer to the named container, or nil.
func findContainerByName(ds *appsv1.DaemonSet, name string) *corev1.Container {
	for i := range ds.Spec.Template.Spec.Containers {
		if ds.Spec.Template.Spec.Containers[i].Name == name {
			return &ds.Spec.Template.Spec.Containers[i]
		}
	}
	return nil
}

// setArg finds an existing arg by prefix and replaces its value,
// or appends the arg if not present.
func setArg(container *corev1.Container, prefix, value string) {
	target := prefix + value
	for i, arg := range container.Args {
		if strings.HasPrefix(arg, prefix) {
			container.Args[i] = target
			return
		}
	}
	container.Args = append(container.Args, target)
}

// getRotationConfig extracts rotation configuration from the ClusterCSIDriver.
// It returns the enable-secret-rotation value and the rotation-poll-interval value.
func getRotationConfig(ccd *opv1.ClusterCSIDriver) (string, string) {
	enableRotation := defaultEnableSecretRotation
	pollInterval := defaultRotationPollInterval

	if ccd.Spec.DriverConfig.DriverType != opv1.SecretsStoreDriverType {
		return enableRotation, pollInterval
	}

	ss := ccd.Spec.DriverConfig.SecretsStore

	if ss.SecretRotation.Type == "" {
		return enableRotation, pollInterval
	}

	if ss.SecretRotation.Type == opv1.SecretRotationNone {
		enableRotation = "false"
	}

	if ss.SecretRotation.Type == opv1.SecretRotationCustom {
		if ss.SecretRotation.Custom.RotationPollIntervalSeconds != 0 {
			d := time.Duration(ss.SecretRotation.Custom.RotationPollIntervalSeconds) * time.Second
			pollInterval = d.String()
		}
	}

	return enableRotation, pollInterval
}
