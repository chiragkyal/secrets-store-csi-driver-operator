// Package azure contains a Ginkgo suite that verifies the operator's
// driverConfig.secretsStore.tokenRequests configuration produces real,
// working Azure Workload Identity Federation (WIF): a genuine Key Vault
// secret is fetched by the real Azure provider and mounted into a pod,
// using audiences configured declaratively through ClusterCSIDriver
// instead of a manual `oc patch csidriver` workaround.
//
// This suite creates and destroys real Azure resources (Key Vault, a
// user-assigned managed identity, and a federated identity credential)
// directly through the Azure SDK for Go (no az CLI dependency) and
// requires:
//   - a live OpenShift cluster with Azure Workload Identity enabled (OIDC
//     issuer exposed) and the operator/driver already deployed;
//   - the helm and oc CLIs in $PATH, plus network access to the Azure
//     Resource Manager and Key Vault endpoints;
//   - Azure service principal credentials at
//     $CLUSTER_PROFILE_DIR/osServicePrincipal.json (the standard OpenShift
//     CI convention -- matches the credentials the existing
//     openshift-e2e-azure-csi-secrets-store-azure-test step already uses).
package azure

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	opv1 "github.com/openshift/api/operator/v1"
	operatorv1client "github.com/openshift/client-go/operator/clientset/versioned"
	operatorv1typed "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// driverName is both the ClusterCSIDriver singleton's name and the
	// storage.k8s.io/v1 CSIDriver object's name.
	driverName = "secrets-store.csi.k8s.io"
	// operatorNamespace is where the operator and its node DaemonSet run.
	operatorNamespace = "openshift-cluster-csi-drivers"
	// daemonSetName is the driver's node DaemonSet.
	daemonSetName = "secrets-store-csi-driver-node"
	// csiDriverContainer is the driver container within the DaemonSet.
	csiDriverContainer = "csi-driver"
	// azureWIFAudience is the audience Azure AD Workload Identity expects,
	// matching upstream's azure.bats and the EP's own example.
	azureWIFAudience = "api://AzureADTokenExchange"
	// providerNamespace is where the Azure provider is installed, matching
	// upstream azure.bats (PROVIDER_NAMESPACE=kube-system).
	providerNamespace = "kube-system"
	// providerAppLabel selects the Azure provider's pods.
	providerAppLabel = "csi-secrets-store-provider-azure"
)

var (
	kubeClient             kubernetes.Interface
	clusterCSIDriverClient operatorv1typed.ClusterCSIDriverInterface

	resourceGroup string
	location      string
	oidcIssuer    string
	tenantID      string

	// runSuffix disambiguates resource names across concurrent/repeated
	// runs against the same Azure subscription, matching azure.bats's
	// "$(openssl rand -hex 2)"-style suffixing.
	runSuffix string

	// originalDriverConfig is captured once in BeforeSuite and restored
	// (best-effort) in AfterSuite. Restoration is expected to fail once
	// tokenRequests.type has been transitioned to Managed by this suite's
	// own specs, since that is a one-way transition -- see
	// docs/testing-guidelines.md.
	originalDriverConfig opv1.CSIDriverConfigSpec
)

func TestAzureE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Secrets Store CSI Driver Operator Azure WIF E2E Suite")
}

var _ = BeforeSuite(func() {
	rand.Seed(time.Now().UnixNano())
	runSuffix = fmt.Sprintf("%x", rand.Int31())[:6]

	Expect(azInit()).To(Succeed(), "unable to initialize Azure SDK credentials/clients")

	restConfig, err := loadRestConfig()
	Expect(err).NotTo(HaveOccurred(), "unable to load kubeconfig")

	kubeClient, err = kubernetes.NewForConfig(restConfig)
	Expect(err).NotTo(HaveOccurred(), "unable to build kube client")

	operatorClientset, err := operatorv1client.NewForConfig(restConfig)
	Expect(err).NotTo(HaveOccurred(), "unable to build operator client")
	clusterCSIDriverClient = operatorClientset.OperatorV1().ClusterCSIDrivers()

	driver, err := clusterCSIDriverClient.Get(context.Background(), driverName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "ClusterCSIDriver %q must already exist -- deploy the operator before running this suite", driverName)
	originalDriverConfig = *driver.Spec.DriverConfig.DeepCopy()

	resourceGroup, err = ocGetResourceGroup()
	Expect(err).NotTo(HaveOccurred(), "unable to resolve the cluster's Azure resource group")

	location, err = azResourceGroupLocation(resourceGroup)
	Expect(err).NotTo(HaveOccurred(), "unable to resolve the resource group's location")

	oidcIssuer, err = ocGetOIDCIssuer()
	Expect(err).NotTo(HaveOccurred(), "unable to resolve the cluster's OIDC issuer")

	tenantID, err = azTenantID()
	Expect(err).NotTo(HaveOccurred(), "unable to resolve the Azure tenant ID")

	GinkgoWriter.Printf("resourceGroup=%s location=%s oidcIssuer=%s runSuffix=%s\n", resourceGroup, location, oidcIssuer, runSuffix)
})

var _ = AfterSuite(func() {
	restoreDriverConfig()
})

// loadRestConfig builds a *rest.Config from $KUBECONFIG, falling back to
// ~/.kube/config.
func loadRestConfig() (*rest.Config, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("KUBECONFIG is not set and the home directory could not be determined: %w", err)
		}
		kubeconfig = filepath.Join(home, ".kube", "config")
	}
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}
