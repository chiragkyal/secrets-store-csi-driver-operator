package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	opv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	pollInterval = 2 * time.Second
	// pollTimeout allows for a DaemonSet rolling update across all nodes,
	// not just an API object update.
	pollTimeout = 5 * time.Minute
)

// setSecretsStoreConfig patches the ClusterCSIDriver's driverConfig to
// driverType SecretsStore with the given secretsStore config, via a JSON
// merge patch. This is the declarative configuration path this suite
// exercises in place of the manual `oc patch csidriver ... tokenRequests`
// workaround.
//
// A merge patch is used rather than a Get-mutate-Update round trip because
// ClusterCSIDriverSpec.DriverConfig has no omitempty/omitzero json tag, so a
// full-object Update always serializes driverConfig as present (even when
// restoring it to the Go zero value) -- which the API server rejects for
// this singleton's name via a CEL rule requiring either driverConfig to be
// entirely absent or driverType == "SecretsStore". See restoreDriverConfig
// below, which hits exactly this case when driverConfig was never
// configured before this suite ran.
func setSecretsStoreConfig(secretsStore opv1.SecretsStoreCSIDriverConfigSpec) {
	patchDriverConfig(opv1.CSIDriverConfigSpec{
		DriverType:   opv1.SecretsStoreDriverType,
		SecretsStore: secretsStore,
	})
}

// patchDriverConfig sets spec.driverConfig to driverConfig via a JSON merge
// patch, or removes it entirely (via an explicit null, which RFC 7396
// merge-patch semantics interpret as "delete this key") when driverConfig
// is the zero value.
func patchDriverConfig(driverConfig opv1.CSIDriverConfigSpec) {
	var patch []byte
	if driverConfig == (opv1.CSIDriverConfigSpec{}) {
		patch = []byte(`{"spec":{"driverConfig":null}}`)
	} else {
		var err error
		patch, err = json.Marshal(map[string]any{"spec": map[string]any{"driverConfig": driverConfig}})
		Expect(err).NotTo(HaveOccurred(), "failed to build driverConfig merge patch")
	}

	Eventually(func() error {
		_, err := clusterCSIDriverClient.Patch(context.Background(), driverName, types.MergePatchType, patch, metav1.PatchOptions{})
		return err
	}, pollTimeout, pollInterval).Should(Succeed(), "failed to update ClusterCSIDriver %q driverConfig", driverName)
}

// restoreDriverConfig restores driverConfig to whatever it was when the
// suite started. Best-effort: once this suite's specs transition
// tokenRequests.type to Managed, the API rejects reverting it, so
// restoration is expected to fail after those specs run -- this is logged,
// not treated as a suite failure.
func restoreDriverConfig() {
	var patch []byte
	if originalDriverConfig == (opv1.CSIDriverConfigSpec{}) {
		patch = []byte(`{"spec":{"driverConfig":null}}`)
	} else {
		var err error
		patch, err = json.Marshal(map[string]any{"spec": map[string]any{"driverConfig": originalDriverConfig}})
		if err != nil {
			GinkgoWriter.Printf("unable to build driverConfig merge patch for restore: %v\n", err)
			return
		}
	}

	if _, err := clusterCSIDriverClient.Patch(context.Background(), driverName, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		GinkgoWriter.Printf("unable to restore ClusterCSIDriver %q driverConfig (expected if tokenRequests.type was transitioned to Managed): %v\n", driverName, err)
	}
}

// waitForTokenRequestAudiences polls the live CSIDriver object until its
// tokenRequests' audiences include wantAudience.
func waitForTokenRequestAudiences(wantAudience string) {
	Eventually(func() (bool, error) {
		driver, err := kubeClient.StorageV1().CSIDrivers().Get(context.Background(), driverName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, tr := range driver.Spec.TokenRequests {
			if tr.Audience == wantAudience {
				return true, nil
			}
		}
		return false, nil
	}, pollTimeout, pollInterval).Should(BeTrue(), "CSIDriver %q tokenRequests did not converge to include audience %q", driverName, wantAudience)
}

// waitForDaemonSetRollout polls until every desired replica of the node
// DaemonSet is both updated and available, so a subsequent pod creation
// picks up a driver pod running with the new rotation configuration.
func waitForDaemonSetRollout() {
	Eventually(func() (bool, error) {
		ds, err := kubeClient.AppsV1().DaemonSets(operatorNamespace).Get(context.Background(), daemonSetName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return ds.Status.DesiredNumberScheduled > 0 &&
			ds.Status.UpdatedNumberScheduled == ds.Status.DesiredNumberScheduled &&
			ds.Status.NumberAvailable == ds.Status.DesiredNumberScheduled, nil
	}, pollTimeout, pollInterval).Should(BeTrue(), "DaemonSet %s/%s did not finish rolling out", operatorNamespace, daemonSetName)
}

// daemonSetArgValue returns the value portion of the csi-driver container's
// arg with the given prefix, or an error if the container or arg is absent.
func daemonSetArgValue(prefix string) (string, error) {
	ds, err := kubeClient.AppsV1().DaemonSets(operatorNamespace).Get(context.Background(), daemonSetName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	for _, c := range ds.Spec.Template.Spec.Containers {
		if c.Name != csiDriverContainer {
			continue
		}
		for _, arg := range c.Args {
			if strings.HasPrefix(arg, prefix) {
				return strings.TrimPrefix(arg, prefix), nil
			}
		}
		return "", fmt.Errorf("arg with prefix %q not found on container %q", prefix, csiDriverContainer)
	}
	return "", fmt.Errorf("container %q not found in DaemonSet %s/%s", csiDriverContainer, operatorNamespace, daemonSetName)
}
