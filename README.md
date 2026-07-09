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

## Configuring secret rotation and workload identity federation (WIF)

`driverConfig.secretsStore` on the `ClusterCSIDriver` is entirely optional. When omitted, the driver behaves exactly as it did before this field existed: secret rotation is enabled with a 2-minute poll interval, and any `CSIDriver.spec.tokenRequests` set outside the operator is left untouched.

To customize secret rotation (e.g. disable it, or use a custom poll interval) and/or opt in to operator-managed WIF token audiences:

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
      # Omit secretRotation entirely to keep the 2-minute default, or set
      # type: None to disable rotation.
      secretRotation:
        type: Custom
        custom:
          rotationPollIntervalSeconds: 300
      # Omit tokenRequests entirely to preserve any existing/external
      # CSIDriver.spec.tokenRequests. Set type: Managed to let the operator
      # own the token audience list -- this is a one-way transition and
      # cannot be reverted to Unmanaged afterward.
      tokenRequests:
        type: Managed
        managed:
          audiences:
            - audience: sts.amazonaws.com
              expirationSeconds: 3600
            - audience: api://AzureADTokenExchange
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
