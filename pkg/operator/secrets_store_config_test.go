package operator

import (
	"reflect"
	"testing"
	"time"

	opv1 "github.com/openshift/api/operator/v1"
	storagev1 "k8s.io/api/storage/v1"
)

func TestEffectiveSecretRotation(t *testing.T) {
	tests := []struct {
		name          string
		clusterDriver *opv1.ClusterCSIDriver
		wantEnabled   bool
		wantInterval  time.Duration
		wantErr       bool
	}{
		{
			name:          "defaults when cluster driver is nil",
			clusterDriver: nil,
			wantEnabled:   true,
			wantInterval:  defaultSecretRotationPollInterval,
		},
		{
			name: "defaults when driver type is not secrets store",
			clusterDriver: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.AWSDriverType,
					},
				},
			},
			wantEnabled:  true,
			wantInterval: defaultSecretRotationPollInterval,
		},
		{
			name: "defaults when secrets store rotation type is omitted",
			clusterDriver: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
					},
				},
			},
			wantEnabled:  true,
			wantInterval: defaultSecretRotationPollInterval,
		},
		{
			name: "disables rotation when type is none",
			clusterDriver: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: opv1.SecretsStoreSecretRotation{
								Type: opv1.SecretRotationNone,
							},
						},
					},
				},
			},
			wantEnabled:  false,
			wantInterval: defaultSecretRotationPollInterval,
		},
		{
			name: "defaults custom rotation interval when omitted",
			clusterDriver: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: opv1.SecretsStoreSecretRotation{
								Type:   opv1.SecretRotationCustom,
								Custom: opv1.CustomSecretRotation{},
							},
						},
					},
				},
			},
			wantEnabled:  true,
			wantInterval: defaultSecretRotationPollInterval,
		},
		{
			name: "uses custom rotation interval when provided",
			clusterDriver: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: opv1.SecretsStoreSecretRotation{
								Type: opv1.SecretRotationCustom,
								Custom: opv1.CustomSecretRotation{
									RotationPollIntervalSeconds: 300,
								},
							},
						},
					},
				},
			},
			wantEnabled:  true,
			wantInterval: 5 * time.Minute,
		},
		{
			name: "returns error on unsupported rotation type",
			clusterDriver: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: opv1.SecretsStoreSecretRotation{
								Type: opv1.SecretRotationType("Broken"),
							},
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotEnabled, gotInterval, err := effectiveSecretRotation(tt.clusterDriver)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotEnabled != tt.wantEnabled {
				t.Fatalf("expected enabled=%v, got %v", tt.wantEnabled, gotEnabled)
			}
			if gotInterval != tt.wantInterval {
				t.Fatalf("expected interval=%v, got %v", tt.wantInterval, gotInterval)
			}
		})
	}
}

func TestEffectiveTokenRequests(t *testing.T) {
	existingExpiration := int64(900)
	existing := []storagev1.TokenRequest{
		{
			Audience:          "existing",
			ExpirationSeconds: &existingExpiration,
		},
	}
	managedAudience := "sts.amazonaws.com"
	tests := []struct {
		name          string
		clusterDriver *opv1.ClusterCSIDriver
		existing      []storagev1.TokenRequest
		want          []storagev1.TokenRequest
		wantErr       bool
	}{
		{
			name:          "preserves existing when cluster driver is nil",
			clusterDriver: nil,
			existing:      existing,
			want:          existing,
		},
		{
			name: "preserves existing when driver type is not secrets store",
			clusterDriver: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.AWSDriverType,
					},
				},
			},
			existing: existing,
			want:     existing,
		},
		{
			name: "preserves existing when token request type is omitted",
			clusterDriver: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
					},
				},
			},
			existing: existing,
			want:     existing,
		},
		{
			name: "preserves existing when token request type is unmanaged",
			clusterDriver: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							TokenRequests: opv1.SecretsStoreTokenRequests{
								Type: opv1.TokenRequestsUnmanaged,
							},
						},
					},
				},
			},
			existing: existing,
			want:     existing,
		},
		{
			name: "returns nil when managed audiences are omitted",
			clusterDriver: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							TokenRequests: opv1.SecretsStoreTokenRequests{
								Type:    opv1.TokenRequestsManaged,
								Managed: opv1.ManagedTokenRequests{},
							},
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "returns empty slice when managed audiences are empty",
			clusterDriver: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							TokenRequests: opv1.SecretsStoreTokenRequests{
								Type: opv1.TokenRequestsManaged,
								Managed: opv1.ManagedTokenRequests{
									Audiences: &[]opv1.SecretsStoreTokenRequest{},
								},
							},
						},
					},
				},
			},
			want: []storagev1.TokenRequest{},
		},
		{
			name: "maps managed audiences into token requests",
			clusterDriver: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							TokenRequests: opv1.SecretsStoreTokenRequests{
								Type: opv1.TokenRequestsManaged,
								Managed: opv1.ManagedTokenRequests{
									Audiences: &[]opv1.SecretsStoreTokenRequest{
										{
											Audience:          &managedAudience,
											ExpirationSeconds: 3600,
										},
									},
								},
							},
						},
					},
				},
			},
			want: []storagev1.TokenRequest{
				{
					Audience:          managedAudience,
					ExpirationSeconds: int64Ptr(3600),
				},
			},
		},
		{
			name: "returns error when managed audience is nil",
			clusterDriver: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							TokenRequests: opv1.SecretsStoreTokenRequests{
								Type: opv1.TokenRequestsManaged,
								Managed: opv1.ManagedTokenRequests{
									Audiences: &[]opv1.SecretsStoreTokenRequest{
										{},
									},
								},
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "returns error on unsupported token request type",
			clusterDriver: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							TokenRequests: opv1.SecretsStoreTokenRequests{
								Type: opv1.TokenRequestsType("Broken"),
							},
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := effectiveTokenRequests(tt.clusterDriver, tt.existing)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expected token requests %#v, got %#v", tt.want, got)
			}

			if len(tt.existing) > 0 && len(got) > 0 && &got[0] == &tt.existing[0] {
				t.Fatalf("expected cloned token requests, got shared slice")
			}
			if len(tt.existing) > 0 && len(got) > 0 && tt.existing[0].ExpirationSeconds != nil && got[0].ExpirationSeconds == tt.existing[0].ExpirationSeconds {
				t.Fatalf("expected cloned expiration seconds pointer, got shared pointer")
			}
		})
	}
}

func TestEffectiveSecretsStoreDriverConfig(t *testing.T) {
	existingExpiration := int64(1200)
	existing := []storagev1.TokenRequest{
		{
			Audience:          "existing",
			ExpirationSeconds: &existingExpiration,
		},
	}
	managedAudience := "api://AzureADTokenExchange"
	clusterDriver := &opv1.ClusterCSIDriver{
		Spec: opv1.ClusterCSIDriverSpec{
			DriverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					SecretRotation: opv1.SecretsStoreSecretRotation{
						Type: opv1.SecretRotationCustom,
						Custom: opv1.CustomSecretRotation{
							RotationPollIntervalSeconds: 600,
						},
					},
					TokenRequests: opv1.SecretsStoreTokenRequests{
						Type: opv1.TokenRequestsManaged,
						Managed: opv1.ManagedTokenRequests{
							Audiences: &[]opv1.SecretsStoreTokenRequest{
								{
									Audience: &managedAudience,
								},
							},
						},
					},
				},
			},
		},
	}

	got, err := effectiveSecretsStoreDriverConfig(clusterDriver, existing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := secretsStoreDriverConfig{
		rotationEnabled:      true,
		rotationPollInterval: 10 * time.Minute,
		requiresRepublish:    true,
		tokenRequests: []storagev1.TokenRequest{
			{
				Audience: managedAudience,
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected config %#v, got %#v", want, got)
	}

	if reflect.DeepEqual(got.tokenRequests, existing) {
		t.Fatalf("expected managed token requests to replace existing ones")
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}
