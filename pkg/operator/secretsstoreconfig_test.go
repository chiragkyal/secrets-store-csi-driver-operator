package operator

import (
	"testing"

	opv1 "github.com/openshift/api/operator/v1"
)

// TestResolveSecretsStoreConfig_Smoke is a smoke test proving the resolver compiles
// and behaves correctly for its main branches. Full nil-safety/edge-case branch
// coverage is added by task T2_3.
func TestResolveSecretsStoreConfig_Smoke(t *testing.T) {
	cases := []struct {
		name                  string
		spec                  *opv1.ClusterCSIDriverSpec
		expectRotationEnabled bool
		expectIntervalSeconds int32
		expectManaged         bool
	}{
		{
			name:                  "nil spec returns defaults",
			spec:                  nil,
			expectRotationEnabled: true,
			expectIntervalSeconds: 120,
			expectManaged:         false,
		},
		{
			name: "driverType not SecretsStore returns defaults",
			spec: &opv1.ClusterCSIDriverSpec{
				DriverConfig: opv1.CSIDriverConfigSpec{DriverType: opv1.AWSDriverType},
			},
			expectRotationEnabled: true,
			expectIntervalSeconds: 120,
			expectManaged:         false,
		},
		{
			name: "SecretsStore with no sub-fields set returns defaults",
			spec: &opv1.ClusterCSIDriverSpec{
				DriverConfig: opv1.CSIDriverConfigSpec{DriverType: opv1.SecretsStoreDriverType},
			},
			expectRotationEnabled: true,
			expectIntervalSeconds: 120,
			expectManaged:         false,
		},
		{
			name: "secretRotation type None disables rotation",
			spec: &opv1.ClusterCSIDriverSpec{
				DriverConfig: opv1.CSIDriverConfigSpec{
					DriverType: opv1.SecretsStoreDriverType,
					SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
						SecretRotation: opv1.SecretsStoreSecretRotation{Type: opv1.SecretRotationNone},
					},
				},
			},
			expectRotationEnabled: false,
			expectIntervalSeconds: 120,
			expectManaged:         false,
		},
		{
			name: "secretRotation type Custom sets custom interval",
			spec: &opv1.ClusterCSIDriverSpec{
				DriverConfig: opv1.CSIDriverConfigSpec{
					DriverType: opv1.SecretsStoreDriverType,
					SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
						SecretRotation: opv1.SecretsStoreSecretRotation{
							Type:   opv1.SecretRotationCustom,
							Custom: opv1.CustomSecretRotation{RotationPollIntervalSeconds: 300},
						},
					},
				},
			},
			expectRotationEnabled: true,
			expectIntervalSeconds: 300,
			expectManaged:         false,
		},
		{
			name: "tokenRequests type Managed sets Managed true",
			spec: &opv1.ClusterCSIDriverSpec{
				DriverConfig: opv1.CSIDriverConfigSpec{
					DriverType: opv1.SecretsStoreDriverType,
					SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
						TokenRequests: opv1.SecretsStoreTokenRequests{Type: opv1.TokenRequestsManaged},
					},
				},
			},
			expectRotationEnabled: true,
			expectIntervalSeconds: 120,
			expectManaged:         true,
		},
		{
			name: "tokenRequests type Unmanaged leaves Managed false",
			spec: &opv1.ClusterCSIDriverSpec{
				DriverConfig: opv1.CSIDriverConfigSpec{
					DriverType: opv1.SecretsStoreDriverType,
					SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
						TokenRequests: opv1.SecretsStoreTokenRequests{Type: opv1.TokenRequestsUnmanaged},
					},
				},
			},
			expectRotationEnabled: true,
			expectIntervalSeconds: 120,
			expectManaged:         false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rotation, tokenRequests := ResolveSecretsStoreConfig(tc.spec)
			if rotation.Enabled != tc.expectRotationEnabled {
				t.Errorf("expected rotation.Enabled to be %v, got %v", tc.expectRotationEnabled, rotation.Enabled)
			}
			if rotation.RotationPollIntervalSeconds != tc.expectIntervalSeconds {
				t.Errorf("expected rotation.RotationPollIntervalSeconds to be %d, got %d", tc.expectIntervalSeconds, rotation.RotationPollIntervalSeconds)
			}
			if tokenRequests.Managed != tc.expectManaged {
				t.Errorf("expected tokenRequests.Managed to be %v, got %v", tc.expectManaged, tokenRequests.Managed)
			}
		})
	}
}

// TestResolveSecretsStoreConfig_ManagedAudiences covers the audience-list resolution
// branch specifically, including the "explicit empty list clears audiences" case
// (specs.md FR-008).
func TestResolveSecretsStoreConfig_ManagedAudiences(t *testing.T) {
	audience := "sts.amazonaws.com"
	populated := []opv1.SecretsStoreTokenRequest{{Audience: &audience, ExpirationSeconds: 3600}}
	empty := []opv1.SecretsStoreTokenRequest{}

	cases := []struct {
		name         string
		audiences    *[]opv1.SecretsStoreTokenRequest
		expectAudLen int
	}{
		{name: "nil audiences pointer (omitted) resolves to empty slice", audiences: nil, expectAudLen: 0},
		{name: "explicit empty list clears audiences", audiences: &empty, expectAudLen: 0},
		{name: "populated audiences list is passed through", audiences: &populated, expectAudLen: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := &opv1.ClusterCSIDriverSpec{
				DriverConfig: opv1.CSIDriverConfigSpec{
					DriverType: opv1.SecretsStoreDriverType,
					SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
						TokenRequests: opv1.SecretsStoreTokenRequests{
							Type:    opv1.TokenRequestsManaged,
							Managed: opv1.ManagedTokenRequests{Audiences: tc.audiences},
						},
					},
				},
			}
			_, tokenRequests := ResolveSecretsStoreConfig(spec)
			if !tokenRequests.Managed {
				t.Fatalf("expected Managed to be true")
			}
			if len(tokenRequests.Audiences) != tc.expectAudLen {
				t.Errorf("expected %d audiences, got %d", tc.expectAudLen, len(tokenRequests.Audiences))
			}
		})
	}
}
