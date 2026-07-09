package operator

import (
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/yaml"

	"github.com/openshift/secrets-store-csi-driver-operator/assets"
)

// This file contains task T5_1's cross-cutting regression suite: with no
// driverConfig configured on ClusterCSIDriver (the pre-feature-upgrade state),
// both dynamic consumers (the CSIDriver AssetFunc from T3_1/T3_2 and the DaemonSet
// hook from T4_1) MUST behave identically to today's static assets (specs.md
// FR-010; plan.md Section 7's highest-priority regression risk). Unlike the
// per-task tests in csidriverasset_test.go/daemonsethook_test.go (which use
// hand-built fixtures), these tests read the ACTUAL embedded production assets via
// the real assets package, so a regression in the real manifests would be caught
// here even if the hand-built fixtures drift out of sync.

// TestUpgradeDefaultParity_CSIDriver confirms the dynamically-generated CSIDriver
// object matches the static assets/csidriver.yaml's implied defaults when no
// ClusterCSIDriver configuration exists.
func TestUpgradeDefaultParity_CSIDriver(t *testing.T) {
	base := func(name string) ([]byte, error) {
		return assets.ReadFile(name)
	}
	wrapped := withSecretsStoreCSIDriverAsset(base, &fakeClusterCSIDriverLister{}, &fakeCSIDriverLister{})

	got, err := wrapped(csidriverAssetName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	driver := decodeTestCSIDriver(t, got)

	if driver.Spec.RequiresRepublish == nil || !*driver.Spec.RequiresRepublish {
		t.Errorf("expected requiresRepublish=true by default (rotation enabled), got %v", driver.Spec.RequiresRepublish)
	}
	if len(driver.Spec.TokenRequests) != 0 {
		t.Errorf("expected no tokenRequests by default (no live object, no driverConfig), got %v", driver.Spec.TokenRequests)
	}
	// Fields this feature does not touch must be untouched, exactly as in the real asset.
	if driver.Spec.PodInfoOnMount == nil || !*driver.Spec.PodInfoOnMount {
		t.Errorf("expected podInfoOnMount to remain true (unrelated to this feature)")
	}
	if driver.Spec.AttachRequired == nil || *driver.Spec.AttachRequired {
		t.Errorf("expected attachRequired to remain false (unrelated to this feature)")
	}
	if driver.Spec.FSGroupPolicy == nil || string(*driver.Spec.FSGroupPolicy) != "File" {
		t.Errorf("expected fsGroupPolicy to remain File (unrelated to this feature)")
	}
}

// TestUpgradeDefaultParity_DaemonSetArgs confirms the rotation hook resolves to the
// same *parsed* rotation behavior as the static assets/node.yaml's hardcoded args
// when no ClusterCSIDriver configuration exists. Per T4_1's documented design
// decision, the interval string format may differ ("120s" vs "2m") but MUST parse
// to the same time.Duration.
func TestUpgradeDefaultParity_DaemonSetArgs(t *testing.T) {
	nodeYAMLBytes, err := assets.ReadFile("node.yaml")
	if err != nil {
		t.Fatalf("unable to read assets/node.yaml: %v", err)
	}
	ds := &appsv1.DaemonSet{}
	if err := yaml.Unmarshal(nodeYAMLBytes, ds); err != nil {
		t.Fatalf("unable to unmarshal assets/node.yaml: %v", err)
	}

	container, err := findContainer(ds, csiDriverContainerName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	originalEnabled, originalIntervalStr := extractRotationArgs(t, container.Args)
	if originalEnabled != "true" {
		t.Fatalf("expected assets/node.yaml's hardcoded default to be true -- has the static asset changed? got %q", originalEnabled)
	}
	originalDuration, err := time.ParseDuration(originalIntervalStr)
	if err != nil {
		t.Fatalf("unable to parse original interval %q: %v", originalIntervalStr, err)
	}

	hook := withSecretsStoreRotationDaemonSetHook(&fakeClusterCSIDriverLister{})
	if err := hook(nil, ds); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	container, _ = findContainer(ds, csiDriverContainerName)
	newEnabled, newIntervalStr := extractRotationArgs(t, container.Args)
	if newEnabled != originalEnabled {
		t.Errorf("expected --enable-secret-rotation to remain %q with no driverConfig set, got %q", originalEnabled, newEnabled)
	}
	newDuration, err := time.ParseDuration(newIntervalStr)
	if err != nil {
		t.Fatalf("unable to parse new interval %q: %v", newIntervalStr, err)
	}
	if newDuration != originalDuration {
		t.Errorf("expected --rotation-poll-interval to resolve to the same duration (%s) with no driverConfig set, got %q (%s)",
			originalDuration, newIntervalStr, newDuration)
	}
}

func extractRotationArgs(t *testing.T, args []string) (enabled, interval string) {
	t.Helper()
	for _, a := range args {
		if strings.HasPrefix(a, rotationEnabledArgPrefix) {
			enabled = strings.TrimPrefix(a, rotationEnabledArgPrefix)
		}
		if strings.HasPrefix(a, rotationIntervalArgPrefix) {
			interval = strings.TrimPrefix(a, rotationIntervalArgPrefix)
		}
	}
	if enabled == "" || interval == "" {
		t.Fatalf("expected both rotation args present in %v", args)
	}
	return enabled, interval
}
