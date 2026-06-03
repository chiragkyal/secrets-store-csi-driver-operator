package operator

import (
	"encoding/json"
	"testing"

	opv1 "github.com/openshift/api/operator/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDynamicAssetFunc_NamespaceReplacement(t *testing.T) {
	ccd := &opv1.ClusterCSIDriver{
		ObjectMeta: metav1.ObjectMeta{Name: providerName},
		Spec: opv1.ClusterCSIDriverSpec{
			DriverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
			},
		},
	}
	ccdLister := newFakeClusterCSIDriverLister(ccd)
	csiLister := newFakeCSIDriverLister(nil)
	af := dynamicAssetFunc("openshift-cluster-csi-drivers", ccdLister, csiLister)

	content, err := af("node_sa.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(content) == "" {
		t.Fatal("expected non-empty content")
	}
	if contains(content, []byte(namespaceKey)) {
		t.Errorf("expected namespace placeholder to be replaced, still found %q", namespaceKey)
	}
	if !contains(content, []byte("openshift-cluster-csi-drivers")) {
		t.Error("expected replaced namespace in content")
	}
}

func TestEnrichCSIDriverYAML_Defaults(t *testing.T) {
	ccd := &opv1.ClusterCSIDriver{
		ObjectMeta: metav1.ObjectMeta{Name: providerName},
		Spec: opv1.ClusterCSIDriverSpec{
			DriverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
			},
		},
	}
	ccdLister := newFakeClusterCSIDriverLister(ccd)
	csiLister := newFakeCSIDriverLister(nil)
	af := dynamicAssetFunc("test-ns", ccdLister, csiLister)

	content, err := af(csiDriverAssetFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	csiDriver := &storagev1.CSIDriver{}
	if err := json.Unmarshal(content, csiDriver); err != nil {
		t.Fatalf("failed to unmarshal CSIDriver: %v", err)
	}

	if csiDriver.Spec.RequiresRepublish == nil || !*csiDriver.Spec.RequiresRepublish {
		t.Error("expected requiresRepublish to be true by default")
	}
	if len(csiDriver.Spec.TokenRequests) != 0 {
		t.Errorf("expected no tokenRequests by default, got %d", len(csiDriver.Spec.TokenRequests))
	}
}

func TestEnrichCSIDriverYAML_RotationDisabled(t *testing.T) {
	ccd := &opv1.ClusterCSIDriver{
		ObjectMeta: metav1.ObjectMeta{Name: providerName},
		Spec: opv1.ClusterCSIDriverSpec{
			DriverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: &opv1.SecretsStoreCSIDriverConfigSpec{
					SecretRotation: &opv1.SecretsStoreSecretRotation{
						Policy: opv1.SecretRotationDisabled,
					},
				},
			},
		},
	}
	ccdLister := newFakeClusterCSIDriverLister(ccd)
	csiLister := newFakeCSIDriverLister(nil)
	af := dynamicAssetFunc("test-ns", ccdLister, csiLister)

	content, err := af(csiDriverAssetFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	csiDriver := &storagev1.CSIDriver{}
	if err := json.Unmarshal(content, csiDriver); err != nil {
		t.Fatalf("failed to unmarshal CSIDriver: %v", err)
	}

	if csiDriver.Spec.RequiresRepublish == nil || *csiDriver.Spec.RequiresRepublish {
		t.Error("expected requiresRepublish to be false when rotation is disabled")
	}
}

func TestEnrichCSIDriverYAML_ManagedWithAudiences(t *testing.T) {
	ccd := &opv1.ClusterCSIDriver{
		ObjectMeta: metav1.ObjectMeta{Name: providerName},
		Spec: opv1.ClusterCSIDriverSpec{
			DriverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: &opv1.SecretsStoreCSIDriverConfigSpec{
					TokenRequests: &opv1.SecretsStoreTokenRequests{
						Policy: opv1.TokenRequestsManaged,
						Audiences: []opv1.SecretsStoreTokenRequest{
							{
								Audience:          strPtr("sts.amazonaws.com"),
								ExpirationSeconds: int64Ptr(3600),
							},
							{
								Audience: strPtr("api://AzureADTokenExchange"),
							},
						},
					},
				},
			},
		},
	}
	ccdLister := newFakeClusterCSIDriverLister(ccd)
	csiLister := newFakeCSIDriverLister(nil)
	af := dynamicAssetFunc("test-ns", ccdLister, csiLister)

	content, err := af(csiDriverAssetFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	csiDriver := &storagev1.CSIDriver{}
	if err := json.Unmarshal(content, csiDriver); err != nil {
		t.Fatalf("failed to unmarshal CSIDriver: %v", err)
	}

	if len(csiDriver.Spec.TokenRequests) != 2 {
		t.Fatalf("expected 2 tokenRequests, got %d", len(csiDriver.Spec.TokenRequests))
	}

	if csiDriver.Spec.TokenRequests[0].Audience != "sts.amazonaws.com" {
		t.Errorf("expected audience %q, got %q", "sts.amazonaws.com", csiDriver.Spec.TokenRequests[0].Audience)
	}
	if csiDriver.Spec.TokenRequests[0].ExpirationSeconds == nil || *csiDriver.Spec.TokenRequests[0].ExpirationSeconds != 3600 {
		t.Errorf("expected expirationSeconds 3600, got %v", csiDriver.Spec.TokenRequests[0].ExpirationSeconds)
	}

	if csiDriver.Spec.TokenRequests[1].Audience != "api://AzureADTokenExchange" {
		t.Errorf("expected audience %q, got %q", "api://AzureADTokenExchange", csiDriver.Spec.TokenRequests[1].Audience)
	}
}

func TestEnrichCSIDriverYAML_UnmanagedPreservesExisting(t *testing.T) {
	existingCSIDriver := &storagev1.CSIDriver{
		ObjectMeta: metav1.ObjectMeta{Name: providerName},
		Spec: storagev1.CSIDriverSpec{
			TokenRequests: []storagev1.TokenRequest{
				{Audience: "api://AzureADTokenExchange"},
			},
		},
	}

	ccd := &opv1.ClusterCSIDriver{
		ObjectMeta: metav1.ObjectMeta{Name: providerName},
		Spec: opv1.ClusterCSIDriverSpec{
			DriverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: &opv1.SecretsStoreCSIDriverConfigSpec{
					TokenRequests: &opv1.SecretsStoreTokenRequests{
						Policy: opv1.TokenRequestsUnmanaged,
					},
				},
			},
		},
	}
	ccdLister := newFakeClusterCSIDriverLister(ccd)
	csiLister := newFakeCSIDriverLister(existingCSIDriver)
	af := dynamicAssetFunc("test-ns", ccdLister, csiLister)

	content, err := af(csiDriverAssetFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	csiDriver := &storagev1.CSIDriver{}
	if err := json.Unmarshal(content, csiDriver); err != nil {
		t.Fatalf("failed to unmarshal CSIDriver: %v", err)
	}

	if len(csiDriver.Spec.TokenRequests) != 1 {
		t.Fatalf("expected 1 tokenRequest preserved, got %d", len(csiDriver.Spec.TokenRequests))
	}
	if csiDriver.Spec.TokenRequests[0].Audience != "api://AzureADTokenExchange" {
		t.Errorf("expected preserved audience %q, got %q", "api://AzureADTokenExchange", csiDriver.Spec.TokenRequests[0].Audience)
	}
}

func TestEnrichCSIDriverYAML_NilTokenRequestsPreservesExisting(t *testing.T) {
	existingCSIDriver := &storagev1.CSIDriver{
		ObjectMeta: metav1.ObjectMeta{Name: providerName},
		Spec: storagev1.CSIDriverSpec{
			TokenRequests: []storagev1.TokenRequest{
				{Audience: "api://AzureADTokenExchange"},
			},
		},
	}

	ccd := &opv1.ClusterCSIDriver{
		ObjectMeta: metav1.ObjectMeta{Name: providerName},
		Spec: opv1.ClusterCSIDriverSpec{
			DriverConfig: opv1.CSIDriverConfigSpec{
				DriverType:   opv1.SecretsStoreDriverType,
				SecretsStore: &opv1.SecretsStoreCSIDriverConfigSpec{},
			},
		},
	}
	ccdLister := newFakeClusterCSIDriverLister(ccd)
	csiLister := newFakeCSIDriverLister(existingCSIDriver)
	af := dynamicAssetFunc("test-ns", ccdLister, csiLister)

	content, err := af(csiDriverAssetFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	csiDriver := &storagev1.CSIDriver{}
	if err := json.Unmarshal(content, csiDriver); err != nil {
		t.Fatalf("failed to unmarshal CSIDriver: %v", err)
	}

	if len(csiDriver.Spec.TokenRequests) != 1 {
		t.Fatalf("expected 1 tokenRequest preserved when tokenRequests is nil, got %d", len(csiDriver.Spec.TokenRequests))
	}
	if csiDriver.Spec.TokenRequests[0].Audience != "api://AzureADTokenExchange" {
		t.Errorf("expected preserved audience %q, got %q", "api://AzureADTokenExchange", csiDriver.Spec.TokenRequests[0].Audience)
	}
}

func TestEnrichCSIDriverYAML_ManagedEmptyAudiencesClearsTokenRequests(t *testing.T) {
	existingCSIDriver := &storagev1.CSIDriver{
		ObjectMeta: metav1.ObjectMeta{Name: providerName},
		Spec: storagev1.CSIDriverSpec{
			TokenRequests: []storagev1.TokenRequest{
				{Audience: "api://AzureADTokenExchange"},
			},
		},
	}

	ccd := &opv1.ClusterCSIDriver{
		ObjectMeta: metav1.ObjectMeta{Name: providerName},
		Spec: opv1.ClusterCSIDriverSpec{
			DriverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: &opv1.SecretsStoreCSIDriverConfigSpec{
					TokenRequests: &opv1.SecretsStoreTokenRequests{
						Policy: opv1.TokenRequestsManaged,
					},
				},
			},
		},
	}
	ccdLister := newFakeClusterCSIDriverLister(ccd)
	csiLister := newFakeCSIDriverLister(existingCSIDriver)
	af := dynamicAssetFunc("test-ns", ccdLister, csiLister)

	content, err := af(csiDriverAssetFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	csiDriver := &storagev1.CSIDriver{}
	if err := json.Unmarshal(content, csiDriver); err != nil {
		t.Fatalf("failed to unmarshal CSIDriver: %v", err)
	}

	if len(csiDriver.Spec.TokenRequests) != 0 {
		t.Errorf("expected tokenRequests to be cleared when Managed with empty audiences, got %d", len(csiDriver.Spec.TokenRequests))
	}
}

func TestGetCSIDriverConfig(t *testing.T) {
	tests := []struct {
		name                   string
		ccd                    *opv1.ClusterCSIDriver
		existingCSIDriver      *storagev1.CSIDriver
		expectedRepublish      bool
		expectedTokenRequestsN int
	}{
		{
			name: "non-SecretsStore driver type preserves existing tokenRequests",
			ccd: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.AWSDriverType,
					},
				},
			},
			existingCSIDriver: &storagev1.CSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: storagev1.CSIDriverSpec{
					TokenRequests: []storagev1.TokenRequest{
						{Audience: "api://AzureADTokenExchange"},
					},
				},
			},
			expectedRepublish:      true,
			expectedTokenRequestsN: 1,
		},
		{
			name: "non-SecretsStore driver type with no existing CSIDriver",
			ccd: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.AWSDriverType,
					},
				},
			},
			expectedRepublish:      true,
			expectedTokenRequestsN: 0,
		},
		{
			name: "nil secretsStore config preserves existing tokenRequests",
			ccd: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
					},
				},
			},
			existingCSIDriver: &storagev1.CSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: storagev1.CSIDriverSpec{
					TokenRequests: []storagev1.TokenRequest{
						{Audience: "api://AzureADTokenExchange"},
					},
				},
			},
			expectedRepublish:      true,
			expectedTokenRequestsN: 1,
		},
		{
			name: "nil secretsStore config with no existing CSIDriver",
			ccd: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
					},
				},
			},
			expectedRepublish:      true,
			expectedTokenRequestsN: 0,
		},
		{
			name: "rotation enabled with managed token requests",
			ccd: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: &opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: &opv1.SecretsStoreSecretRotation{
								Policy: opv1.SecretRotationEnabled,
							},
							TokenRequests: &opv1.SecretsStoreTokenRequests{
								Policy: opv1.TokenRequestsManaged,
								Audiences: []opv1.SecretsStoreTokenRequest{
									{Audience: strPtr("sts.amazonaws.com")},
								},
							},
						},
					},
				},
			},
			expectedRepublish:      true,
			expectedTokenRequestsN: 1,
		},
		{
			name: "rotation disabled",
			ccd: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: &opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: &opv1.SecretsStoreSecretRotation{
								Policy: opv1.SecretRotationDisabled,
							},
						},
					},
				},
			},
			expectedRepublish:      false,
			expectedTokenRequestsN: 0,
		},
		{
			name: "unmanaged preserves existing",
			ccd: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: &opv1.SecretsStoreCSIDriverConfigSpec{
							TokenRequests: &opv1.SecretsStoreTokenRequests{
								Policy: opv1.TokenRequestsUnmanaged,
							},
						},
					},
				},
			},
			existingCSIDriver: &storagev1.CSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: storagev1.CSIDriverSpec{
					TokenRequests: []storagev1.TokenRequest{
						{Audience: "api://AzureADTokenExchange"},
					},
				},
			},
			expectedRepublish:      true,
			expectedTokenRequestsN: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			csiLister := newFakeCSIDriverLister(tt.existingCSIDriver)
			republish, tokenRequests := getCSIDriverConfig(tt.ccd, csiLister)
			if republish != tt.expectedRepublish {
				t.Errorf("expected requiresRepublish=%v, got %v", tt.expectedRepublish, republish)
			}
			if len(tokenRequests) != tt.expectedTokenRequestsN {
				t.Errorf("expected %d tokenRequests, got %d", tt.expectedTokenRequestsN, len(tokenRequests))
			}
		})
	}
}

func contains(data, substr []byte) bool {
	return len(data) > 0 && len(substr) > 0 && string(data) != "" && len(substr) <= len(data) && bytesContains(data, substr)
}

func bytesContains(data, substr []byte) bool {
	for i := 0; i <= len(data)-len(substr); i++ {
		if string(data[i:i+len(substr)]) == string(substr) {
			return true
		}
	}
	return false
}
