# secrets-store-csi-driver-operator

An operator to deploy the [Secrets Store CSI Driver](https://github.com/openshift/secrets-store-csi-driver).

# Quick start

To build and run the operator locally:

```shell
# Create only the resources the operator needs to run via CLI
oc apply -f - <<EOF
apiVersion: operator.openshift.io/v1
kind: ClusterCSIDriver
metadata:
    name: secrets-store.csi.k8s.io
spec:
  logLevel: Normal
  managementState: Managed
  operatorLogLevel: Trace
EOF

# Build the operator
make

# Set the environment variables
export OPERATOR_NAME=secrets-store-csi-driver-operator
export DRIVER_IMAGE=registry.k8s.io/csi-secrets-store/driver:v1.3.3
export NODE_DRIVER_REGISTRAR_IMAGE=quay.io/openshift/origin-csi-node-driver-registrar:latest
export LIVENESS_PROBE_IMAGE=quay.io/openshift/origin-csi-livenessprobe:latest

# Run the operator via CLI
./secrets-store-csi-driver-operator start --kubeconfig $KUBECONFIG --namespace openshift-cluster-csi-drivers
```

## Bumping OCP version in CSV and OLM metadata

This updates the package versions in `config/manifests/secrets-store-csi-driver-operator.package.yaml`, `config/manifests/stable/secrets-store-csi-driver-operator.clusterserviceversion.yaml`, `README.md` and `Makefile` to 4.20:
```
./hack/update-metadata.sh 4.20
```

# OLM

To build bundle and index images, use the `hack/create-bundle` script:

```shell
cd hack
./create-bundle registry.ci.openshift.org/ocp/5.0:secrets-store-csi-driver registry.ci.openshift.org/ocp/5.0:secrets-store-csi-driver-operator quay.io/<my_user>/secrets-store-bundle quay.io/<my_user>/secrets-store-index
```

At the end it will print a command that creates `Subscription` for the newly created index image.

# Using the must-gather image

The `must-gather` image for secrets-store-csi-driver-operator supplements the [openshift/must-gather](https://github.com/openshift/must-gather) image to gather Secrets Store related resources.

```shell
oc adm must-gather --image=quay.io/openshift/origin-secrets-store-csi-mustgather:latest
```

This command creates a must-gather containing:
- Logs and resources in the operator namespace (`openshift-cluster-csi-drivers`)
- `SecretProviderClass` and `SecretProviderClassPodStatus` objects
- `ClusterCSIDriver` and `CSIDriver` objects

To build the `must-gather` image locally:

```shell
REPO=quay.io/<user>/secrets-store-csi-mustgather:latest
docker build -t ${REPO} -f Dockerfile.mustgather .
```

# Configuring rotation and workload identity federation

The operator reads the `ClusterCSIDriver` named `secrets-store.csi.k8s.io` and
can now derive Secrets Store-specific behavior from `spec.driverConfig`.

## Secret rotation

To disable automatic secret rotation:

```yaml
apiVersion: operator.openshift.io/v1
kind: ClusterCSIDriver
metadata:
  name: secrets-store.csi.k8s.io
spec:
  managementState: Managed
  driverConfig:
    driverType: SecretsStore
    secretsStore:
      secretRotation:
        type: None
```

To enable rotation with a custom poll interval of 5 minutes:

```yaml
apiVersion: operator.openshift.io/v1
kind: ClusterCSIDriver
metadata:
  name: secrets-store.csi.k8s.io
spec:
  managementState: Managed
  driverConfig:
    driverType: SecretsStore
    secretsStore:
      secretRotation:
        type: Custom
        custom:
          rotationPollIntervalSeconds: 300
```

When `secretRotation` is omitted, the operator keeps the default platform
behavior of rotation enabled with a 2 minute poll interval.

## Token requests for workload identity federation

To let the operator manage service account token audiences:

```yaml
apiVersion: operator.openshift.io/v1
kind: ClusterCSIDriver
metadata:
  name: secrets-store.csi.k8s.io
spec:
  managementState: Managed
  driverConfig:
    driverType: SecretsStore
    secretsStore:
      tokenRequests:
        type: Managed
        managed:
          audiences:
            - audience: sts.amazonaws.com
              expirationSeconds: 3600
            - audience: api://AzureADTokenExchange
```

To clear all operator-managed token audiences, keep `type: Managed` and set an
explicit empty audience list:

```yaml
apiVersion: operator.openshift.io/v1
kind: ClusterCSIDriver
metadata:
  name: secrets-store.csi.k8s.io
spec:
  managementState: Managed
  driverConfig:
    driverType: SecretsStore
    secretsStore:
      tokenRequests:
        type: Managed
        managed:
          audiences: []
```

When `tokenRequests` is omitted or left as `type: Unmanaged`, the operator
preserves any existing token requests already present on the `CSIDriver`
resource. Once `type: Managed` is set, the operator-managed ownership model is
one-way and should not be treated as reversible.

## Verifying the effective configuration

Inspect the rendered `CSIDriver`:

```shell
oc get csidriver secrets-store.csi.k8s.io -o yaml
```

Inspect the `csi-driver` container arguments on the node DaemonSet:

```shell
oc get ds -n openshift-cluster-csi-drivers secrets-store-csi-driver-node -o jsonpath='{.spec.template.spec.containers[?(@.name=="csi-driver")].args}'
```
