package operator

import (
	"testing"

	opv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func strPtr(s string) *string {
	return &s
}

func int32Ptr(i int32) *int32 {
	return &i
}

func int64Ptr(i int64) *int64 {
	return &i
}

func newTestDaemonSet() *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: csiDriverContainerName,
							Args: []string{
								"--endpoint=$(CSI_ENDPOINT)",
								"--enable-secret-rotation=true",
								"--rotation-poll-interval=2m",
							},
						},
					},
				},
			},
		},
	}
}

func TestGetRotationConfig(t *testing.T) {
	tests := []struct {
		name                 string
		ccd                  *opv1.ClusterCSIDriver
		expectedEnable       string
		expectedPollInterval string
	}{
		{
			name: "defaults when driverType is not SecretsStore",
			ccd: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.AWSDriverType,
					},
				},
			},
			expectedEnable:       "true",
			expectedPollInterval: "2m",
		},
		{
			name: "defaults when driverConfig.secretsStore is nil",
			ccd: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
					},
				},
			},
			expectedEnable:       "true",
			expectedPollInterval: "2m",
		},
		{
			name: "defaults when secretRotation is nil",
			ccd: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType:   opv1.SecretsStoreDriverType,
						SecretsStore: &opv1.SecretsStoreCSIDriverConfigSpec{},
					},
				},
			},
			expectedEnable:       "true",
			expectedPollInterval: "2m",
		},
		{
			name: "rotation disabled (type None)",
			ccd: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: &opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: &opv1.SecretsStoreSecretRotation{
								Type: opv1.SecretRotationNone,
							},
						},
					},
				},
			},
			expectedEnable:       "false",
			expectedPollInterval: "2m",
		},
		{
			name: "rotation Custom with default interval",
			ccd: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: &opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: &opv1.SecretsStoreSecretRotation{
								Type:   opv1.SecretRotationCustom,
								Custom: &opv1.CustomSecretRotation{},
							},
						},
					},
				},
			},
			expectedEnable:       "true",
			expectedPollInterval: "2m",
		},
		{
			name: "custom poll interval",
			ccd: &opv1.ClusterCSIDriver{
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: &opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: &opv1.SecretsStoreSecretRotation{
								Type: opv1.SecretRotationCustom,
								Custom: &opv1.CustomSecretRotation{
									RotationPollIntervalSeconds: int32Ptr(300),
								},
							},
						},
					},
				},
			},
			expectedEnable:       "true",
			expectedPollInterval: "5m0s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enable, interval := getRotationConfig(tt.ccd)
			if enable != tt.expectedEnable {
				t.Errorf("expected enable=%q, got %q", tt.expectedEnable, enable)
			}
			if interval != tt.expectedPollInterval {
				t.Errorf("expected interval=%q, got %q", tt.expectedPollInterval, interval)
			}
		})
	}
}

func TestWithSecretRotationDaemonSetHook_ReplacesPlaceholders(t *testing.T) {
	tests := []struct {
		name         string
		ccd          *opv1.ClusterCSIDriver
		expectedArgs []string
	}{
		{
			name: "defaults when no config",
			ccd: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
					},
				},
			},
			expectedArgs: []string{
				"--endpoint=$(CSI_ENDPOINT)",
				"--enable-secret-rotation=true",
				"--rotation-poll-interval=2m",
			},
		},
		{
			name: "rotation None (disabled)",
			ccd: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: &opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: &opv1.SecretsStoreSecretRotation{
								Type: opv1.SecretRotationNone,
							},
						},
					},
				},
			},
			expectedArgs: []string{
				"--endpoint=$(CSI_ENDPOINT)",
				"--enable-secret-rotation=false",
				"--rotation-poll-interval=2m",
			},
		},
		{
			name: "rotation Custom with custom interval",
			ccd: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: &opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: &opv1.SecretsStoreSecretRotation{
								Type: opv1.SecretRotationCustom,
								Custom: &opv1.CustomSecretRotation{
									RotationPollIntervalSeconds: int32Ptr(300),
								},
							},
						},
					},
				},
			},
			expectedArgs: []string{
				"--endpoint=$(CSI_ENDPOINT)",
				"--enable-secret-rotation=true",
				"--rotation-poll-interval=5m0s",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lister := newFakeClusterCSIDriverLister(tt.ccd)
			hook := WithSecretRotationDaemonSetHook(lister)

			ds := newTestDaemonSet()
			if err := hook(nil, ds); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			args := ds.Spec.Template.Spec.Containers[0].Args
			if len(args) != len(tt.expectedArgs) {
				t.Fatalf("expected %d args, got %d: %v", len(tt.expectedArgs), len(args), args)
			}
			for i, expected := range tt.expectedArgs {
				if args[i] != expected {
					t.Errorf("arg[%d]: expected %q, got %q", i, expected, args[i])
				}
			}
		})
	}
}

func TestWithSecretRotationDaemonSetHook_MissingContainer(t *testing.T) {
	ds := &appsv1.DaemonSet{
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "other-container",
							Args: []string{"--some-arg=value"},
						},
					},
				},
			},
		},
	}

	ccd := &opv1.ClusterCSIDriver{
		ObjectMeta: metav1.ObjectMeta{Name: providerName},
		Spec: opv1.ClusterCSIDriverSpec{
			DriverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
			},
		},
	}

	lister := newFakeClusterCSIDriverLister(ccd)
	hook := WithSecretRotationDaemonSetHook(lister)

	if err := hook(nil, ds); err == nil {
		t.Fatal("expected error when container not found, got nil")
	}
}
