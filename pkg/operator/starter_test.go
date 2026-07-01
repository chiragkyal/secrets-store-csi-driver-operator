package operator

import (
	"context"
	"testing"

	opv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
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

// makeSecretsStoreSpec returns a ClusterCSIDriverSpec with the given secretRotation settings.
func makeSecretsStoreSpec(rotationType opv1.SecretRotationType, pollIntervalSecs int32) *opv1.ClusterCSIDriverSpec {
	spec := &opv1.ClusterCSIDriverSpec{
		DriverConfig: opv1.CSIDriverConfigSpec{
			DriverType: opv1.SecretsStoreDriverType,
		},
	}
	if rotationType != "" {
		spec.DriverConfig.SecretsStore.SecretRotation.Type = rotationType
		if pollIntervalSecs > 0 {
			spec.DriverConfig.SecretsStore.SecretRotation.Custom.RotationPollIntervalSeconds = pollIntervalSecs
		}
	}
	return spec
}

func TestGetRotationConfig(t *testing.T) {
	cases := []struct {
		name                  string
		spec                  *opv1.ClusterCSIDriverSpec
		wantRequiresRepublish bool
		wantEnableRotation    bool
		wantPollInterval      string
	}{
		{
			name:                  "nil spec returns defaults",
			spec:                  nil,
			wantRequiresRepublish: true,
			wantEnableRotation:    true,
			wantPollInterval:      "2m0s",
		},
		{
			name: "non-SecretsStore driverType returns defaults",
			spec: &opv1.ClusterCSIDriverSpec{
				DriverConfig: opv1.CSIDriverConfigSpec{DriverType: opv1.AWSDriverType},
			},
			wantRequiresRepublish: true,
			wantEnableRotation:    true,
			wantPollInterval:      "2m0s",
		},
		{
			name:                  "SecretsStore with zero-value SecretRotation returns defaults",
			spec:                  makeSecretsStoreSpec("", 0),
			wantRequiresRepublish: true,
			wantEnableRotation:    true,
			wantPollInterval:      "2m0s",
		},
		{
			name:                  "SecretRotation.type None disables rotation",
			spec:                  makeSecretsStoreSpec(opv1.SecretRotationNone, 0),
			wantRequiresRepublish: false,
			wantEnableRotation:    false,
			wantPollInterval:      "2m0s",
		},
		{
			name:                  "SecretRotation.type Custom with zero interval returns defaults",
			spec:                  makeSecretsStoreSpec(opv1.SecretRotationCustom, 0),
			wantRequiresRepublish: true,
			wantEnableRotation:    true,
			wantPollInterval:      "2m0s",
		},
		{
			name:                  "SecretRotation.type Custom with 300s returns 5m0s",
			spec:                  makeSecretsStoreSpec(opv1.SecretRotationCustom, 300),
			wantRequiresRepublish: true,
			wantEnableRotation:    true,
			wantPollInterval:      "5m0s",
		},
		{
			name:                  "SecretRotation.type Custom with 3600s returns 1h0m0s",
			spec:                  makeSecretsStoreSpec(opv1.SecretRotationCustom, 3600),
			wantRequiresRepublish: true,
			wantEnableRotation:    true,
			wantPollInterval:      "1h0m0s",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotRepublish, gotEnable, gotInterval := getRotationConfig(tc.spec)
			if gotRepublish != tc.wantRequiresRepublish {
				t.Fatalf("requiresRepublish: want %v, got %v", tc.wantRequiresRepublish, gotRepublish)
			}
			if gotEnable != tc.wantEnableRotation {
				t.Fatalf("enableRotation: want %v, got %v", tc.wantEnableRotation, gotEnable)
			}
			if gotInterval != tc.wantPollInterval {
				t.Fatalf("pollInterval: want %q, got %q", tc.wantPollInterval, gotInterval)
			}
		})
	}
}

// newFakeDynamicWithCSIDriver returns a fake dynamic client pre-populated with a CSIDriver
// that has the given tokenRequests on its spec.
func newFakeDynamicWithCSIDriver(tokenRequests []storagev1.TokenRequest) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	_ = storagev1.AddToScheme(scheme)
	csiDriver := &storagev1.CSIDriver{
		TypeMeta:   metav1.TypeMeta{APIVersion: "storage.k8s.io/v1", Kind: "CSIDriver"},
		ObjectMeta: metav1.ObjectMeta{Name: providerName},
		Spec:       storagev1.CSIDriverSpec{TokenRequests: tokenRequests},
	}
	return dynamicfake.NewSimpleDynamicClient(scheme, csiDriver)
}

func ptr[T any](v T) *T { return &v }

func TestGetTokenRequests(t *testing.T) {
	existingAudience := "api://AzureADTokenExchange"
	existingTokenRequests := []storagev1.TokenRequest{
		{Audience: existingAudience},
	}

	cases := []struct {
		name         string
		spec         *opv1.ClusterCSIDriverSpec
		dynamicSetup func() *dynamicfake.FakeDynamicClient
		wantLen      int
		wantAudience string // first audience if wantLen > 0
		wantNil      bool   // expect nil (not empty) result
	}{
		{
			name:         "nil spec preserves live CSIDriver tokenRequests",
			spec:         nil,
			dynamicSetup: func() *dynamicfake.FakeDynamicClient { return newFakeDynamicWithCSIDriver(existingTokenRequests) },
			wantLen:      1,
			wantAudience: existingAudience,
		},
		{
			name: "non-SecretsStore driverType preserves live tokenRequests",
			spec: &opv1.ClusterCSIDriverSpec{
				DriverConfig: opv1.CSIDriverConfigSpec{DriverType: opv1.AWSDriverType},
			},
			dynamicSetup: func() *dynamicfake.FakeDynamicClient { return newFakeDynamicWithCSIDriver(existingTokenRequests) },
			wantLen:      1,
			wantAudience: existingAudience,
		},
		{
			name: "Unmanaged type preserves live tokenRequests",
			spec: func() *opv1.ClusterCSIDriverSpec {
				s := makeSecretsStoreSpec("", 0)
				s.DriverConfig.SecretsStore.TokenRequests.Type = opv1.TokenRequestsUnmanaged
				return s
			}(),
			dynamicSetup: func() *dynamicfake.FakeDynamicClient { return newFakeDynamicWithCSIDriver(existingTokenRequests) },
			wantLen:      1,
			wantAudience: existingAudience,
		},
		{
			name: "nil spec with no live CSIDriver returns nil without error",
			spec: nil,
			dynamicSetup: func() *dynamicfake.FakeDynamicClient {
				scheme := runtime.NewScheme()
				_ = storagev1.AddToScheme(scheme)
				return dynamicfake.NewSimpleDynamicClient(scheme)
			},
			wantNil: true,
		},
		{
			name: "Managed type with audiences converts correctly",
			spec: func() *opv1.ClusterCSIDriverSpec {
				s := makeSecretsStoreSpec("", 0)
				s.DriverConfig.SecretsStore.TokenRequests.Type = opv1.TokenRequestsManaged
				s.DriverConfig.SecretsStore.TokenRequests.Managed.Audiences = &[]opv1.SecretsStoreTokenRequest{
					{Audience: ptr("sts.amazonaws.com"), ExpirationSeconds: 3600},
				}
				return s
			}(),
			dynamicSetup: func() *dynamicfake.FakeDynamicClient { return nil },
			wantLen:      1,
			wantAudience: "sts.amazonaws.com",
		},
		{
			name: "Managed type with empty audiences clears tokenRequests",
			spec: func() *opv1.ClusterCSIDriverSpec {
				s := makeSecretsStoreSpec("", 0)
				s.DriverConfig.SecretsStore.TokenRequests.Type = opv1.TokenRequestsManaged
				s.DriverConfig.SecretsStore.TokenRequests.Managed.Audiences = &[]opv1.SecretsStoreTokenRequest{}
				return s
			}(),
			dynamicSetup: func() *dynamicfake.FakeDynamicClient { return nil },
			wantLen:      0,
		},
		{
			name: "Managed type with nil Audiences returns nil",
			spec: func() *opv1.ClusterCSIDriverSpec {
				s := makeSecretsStoreSpec("", 0)
				s.DriverConfig.SecretsStore.TokenRequests.Type = opv1.TokenRequestsManaged
				// Managed.Audiences stays nil
				return s
			}(),
			dynamicSetup: func() *dynamicfake.FakeDynamicClient { return nil },
			wantNil:      true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := tc.dynamicSetup()
			got, err := getTokenRequests(context.Background(), tc.spec, client)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantNil {
				if got != nil {
					t.Fatalf("want nil, got %v", got)
				}
				return
			}
			if len(got) != tc.wantLen {
				t.Fatalf("token requests len: want %d, got %d (%v)", tc.wantLen, len(got), got)
			}
			if tc.wantLen > 0 && got[0].Audience != tc.wantAudience {
				t.Fatalf("audience: want %q, got %q", tc.wantAudience, got[0].Audience)
			}
		})
	}
}
