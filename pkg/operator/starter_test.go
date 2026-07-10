package operator

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	opv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
)

type FakeOperator struct {
	metav1.ObjectMeta
	Spec   opv1.OperatorSpec
	Status opv1.OperatorStatus
}

func TestGetOperatorSyncState(t *testing.T) {
	deletionTimestamp := metav1.Now()

	cases := []struct {
		name          string
		operator      *FakeOperator
		expectedState opv1.ManagementState
	}{
		{
			name: "should return managed when the operator state is managed",
			operator: &FakeOperator{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec:       opv1.OperatorSpec{ManagementState: opv1.Managed},
			},

			expectedState: opv1.Managed,
		},
		{
			name: "should return unmanaged when the operator state is unmanaged",
			operator: &FakeOperator{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec:       opv1.OperatorSpec{ManagementState: opv1.Unmanaged},
			},
			expectedState: opv1.Unmanaged,
		},
		{
			name: "should return removed when the operator state is removed",
			operator: &FakeOperator{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec:       opv1.OperatorSpec{ManagementState: opv1.Removed},
			},
			expectedState: opv1.Removed,
		},
		{
			name: "should return removed when the deletion timestamp is set",
			operator: &FakeOperator{
				ObjectMeta: metav1.ObjectMeta{
					Name:              providerName,
					DeletionTimestamp: &deletionTimestamp,
				},
				Spec: opv1.OperatorSpec{ManagementState: opv1.Managed},
			},
			expectedState: opv1.Removed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			operatorClient := v1helpers.NewFakeOperatorClientWithObjectMeta(&tc.operator.ObjectMeta, &tc.operator.Spec, &tc.operator.Status, nil)
			state := getOperatorSyncState(operatorClient)
			if state != tc.expectedState {
				t.Fatalf("expected sync state to be %v, got %v", tc.expectedState, state)
			}
		})
	}
}

func TestRenderCSIDriverAsset(t *testing.T) {
	managedAudience := "sts.amazonaws.com"
	existingExpiration := int64(1800)
	tests := []struct {
		name                 string
		clusterDriver        *opv1.ClusterCSIDriver
		existingTokenRequests []storagev1.TokenRequest
		wantRequiresRepublish *bool
		wantTokenRequests    []storagev1.TokenRequest
	}{
		{
			name:                  "defaults to republish when cluster driver is absent",
			clusterDriver:         nil,
			existingTokenRequests: nil,
			wantRequiresRepublish: boolPtr(true),
			wantTokenRequests:     nil,
		},
		{
			name: "preserves existing token requests for unmanaged config",
			clusterDriver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
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
			existingTokenRequests: []storagev1.TokenRequest{
				{
					Audience:          "existing",
					ExpirationSeconds: &existingExpiration,
				},
			},
			wantRequiresRepublish: boolPtr(true),
			wantTokenRequests: []storagev1.TokenRequest{
				{
					Audience:          "existing",
					ExpirationSeconds: &existingExpiration,
				},
			},
		},
		{
			name: "disables republish and applies managed token requests",
			clusterDriver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: opv1.SecretsStoreSecretRotation{
								Type: opv1.SecretRotationNone,
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
			},
			existingTokenRequests: nil,
			wantRequiresRepublish: boolPtr(false),
			wantTokenRequests: []storagev1.TokenRequest{
				{
					Audience: managedAudience,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dynamicClient := newFakeClusterCSIDriverDynamicClient(t, tt.clusterDriver)
			kubeClient := kubefake.NewSimpleClientset()
			if len(tt.existingTokenRequests) > 0 {
				_, err := kubeClient.StorageV1().CSIDrivers().Create(
					context.TODO(),
					&storagev1.CSIDriver{
						ObjectMeta: metav1.ObjectMeta{Name: providerName},
						Spec: storagev1.CSIDriverSpec{
							TokenRequests: tt.existingTokenRequests,
						},
					},
					metav1.CreateOptions{},
				)
				if err != nil {
					t.Fatalf("failed to seed existing CSIDriver: %v", err)
				}
			}

			rendered, err := renderCSIDriverAsset([]byte("apiVersion: storage.k8s.io/v1\nkind: CSIDriver\nmetadata:\n  name: secrets-store.csi.k8s.io\nspec:\n  podInfoOnMount: true\n  attachRequired: false\n  fsGroupPolicy: File\n  volumeLifecycleModes:\n  - Ephemeral\n"), kubeClient, dynamicClient)
			if err != nil {
				t.Fatalf("unexpected render error: %v", err)
			}

			var got storagev1.CSIDriver
			if err := json.Unmarshal(rendered, &got); err != nil {
				t.Fatalf("failed to decode rendered asset: %v", err)
			}

			if !reflect.DeepEqual(got.Spec.RequiresRepublish, tt.wantRequiresRepublish) {
				t.Fatalf("expected requiresRepublish=%v, got %v", tt.wantRequiresRepublish, got.Spec.RequiresRepublish)
			}
			if !reflect.DeepEqual(got.Spec.TokenRequests, tt.wantTokenRequests) {
				t.Fatalf("expected token requests %#v, got %#v", tt.wantTokenRequests, got.Spec.TokenRequests)
			}
		})
	}
}

func TestWithSecretRotationDaemonSetHook(t *testing.T) {
	tests := []struct {
		name          string
		clusterDriver *opv1.ClusterCSIDriver
		daemonSet     *appsv1.DaemonSet
		wantArgs      []string
		wantErr       bool
	}{
		{
			name: "updates existing rotation args from custom config",
			clusterDriver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
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
			daemonSet: newTestDaemonSet([]string{
				"--enable-secret-rotation=true",
				"--rotation-poll-interval=2m",
			}),
			wantArgs: []string{
				"--enable-secret-rotation=true",
				"--rotation-poll-interval=5m0s",
			},
		},
		{
			name: "updates existing args when rotation is disabled",
			clusterDriver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
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
			daemonSet: newTestDaemonSet([]string{
				"--enable-secret-rotation=true",
				"--rotation-poll-interval=2m",
			}),
			wantArgs: []string{
				"--enable-secret-rotation=false",
				"--rotation-poll-interval=2m",
			},
		},
		{
			name:          "returns error when csi-driver container is missing",
			clusterDriver: nil,
			daemonSet: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "ns"},
				Spec: appsv1.DaemonSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "other"},
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
			dynamicClient := newFakeClusterCSIDriverDynamicClient(t, tt.clusterDriver)
			hook := withSecretRotationDaemonSetHook(dynamicClient)

			err := hook(&opv1.OperatorSpec{}, tt.daemonSet)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			gotArgs := tt.daemonSet.Spec.Template.Spec.Containers[0].Args
			if !reflect.DeepEqual(gotArgs, tt.wantArgs) {
				t.Fatalf("expected args %#v, got %#v", tt.wantArgs, gotArgs)
			}
		})
	}
}

func newFakeClusterCSIDriverDynamicClient(t *testing.T, clusterDriver *opv1.ClusterCSIDriver) *dynamicfake.FakeDynamicClient {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := opv1.Install(scheme); err != nil {
		t.Fatalf("failed to install operator v1 scheme: %v", err)
	}

	if clusterDriver == nil {
		return dynamicfake.NewSimpleDynamicClient(scheme)
	}

	clusterDriver = clusterDriver.DeepCopy()
	if clusterDriver.APIVersion == "" {
		clusterDriver.APIVersion = opv1.SchemeGroupVersion.String()
	}
	if clusterDriver.Kind == "" {
		clusterDriver.Kind = "ClusterCSIDriver"
	}

	unstructuredObject, err := runtime.DefaultUnstructuredConverter.ToUnstructured(clusterDriver)
	if err != nil {
		t.Fatalf("failed to convert ClusterCSIDriver to unstructured: %v", err)
	}

	return dynamicfake.NewSimpleDynamicClient(
		scheme,
		&unstructured.Unstructured{Object: unstructuredObject},
	)
}

func newTestDaemonSet(args []string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "ns",
		},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "csi-driver",
							Args: args,
						},
					},
				},
			},
		},
	}
}
