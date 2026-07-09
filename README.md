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

The `ClusterCSIDriver` named `secrets-store.csi.k8s.io` exposes an optional
`spec.driverConfig.secretsStore` configuration surface (set
`spec.driverConfig.driverType: SecretsStore` to use it) for two independent
concerns:

- **Secret rotation** — whether the driver periodically re-fetches mounted
  secrets, and how often.
- **Workload identity federation (WIF) token audiences** — service account
  token audiences the driver receives from kubelet, for authenticating to
  external cloud secret providers (e.g. AWS, Azure).

When `driverConfig.secretsStore` is omitted entirely, behavior is unchanged
from before this configuration surface existed: rotation stays enabled with
a 2-minute interval, and any pre-existing `CSIDriver.spec.tokenRequests` is
left untouched.

### Secret rotation

```yaml
apiVersion: operator.openshift.io/v1
kind: ClusterCSIDriver
metadata:
  name: secrets-store.csi.k8s.io
spec:
  driverConfig:
    driverType: SecretsStore
    secretsStore:
      secretRotation:
        type: Custom       # or "None" to disable rotation entirely
        custom:
          minimumRefreshAge: 120   # seconds; omit to use the default (120s)
```

- `secretRotation.type: None` disables periodic secret refresh; secrets are
  only fetched at initial pod mount time.
- `secretRotation.type: Custom` enables rotation; `custom.minimumRefreshAge`
  is the minimum time in seconds between rotation attempts (1–31560000,
  i.e. up to ~1 year). Omitting `custom` (or `minimumRefreshAge`) under
  `Custom` falls back to a 120-second default.
- Omitting `secretRotation` entirely (or omitting `driverConfig.secretsStore`
  altogether) keeps the pre-feature default: rotation enabled, 2-minute
  interval.

### Workload identity federation (WIF) token audiences

```yaml
apiVersion: operator.openshift.io/v1
kind: ClusterCSIDriver
metadata:
  name: secrets-store.csi.k8s.io
spec:
  driverConfig:
    driverType: SecretsStore
    secretsStore:
      tokenRequests:
        type: Managed
        managed:
          audiences:
          - audience: sts.amazonaws.com
            expirationSeconds: 3600   # optional; 600-315360000 (10min-10yr)
          - audience: api://AzureADTokenExchange
```

- `tokenRequests.type: Managed` makes `managed.audiences` the sole source of
  truth for the `CSIDriver.spec.tokenRequests` field, replacing any
  previously configured values.
- Setting `managed.audiences` to an explicit empty list (`audiences: []`)
  clears all operator-managed audiences.
- **Once `tokenRequests.type` has been set to `Managed`, it cannot be
  reverted back to `Unmanaged`.** To stop managing audiences, clear them to
  an empty list instead (see above).
- Omitting `tokenRequests` entirely preserves whatever `tokenRequests` is
  already configured on the `CSIDriver` object (e.g. one set manually before
  this configuration surface existed), without disruption.

### Verifying the configuration

Inspect the driver DaemonSet's rotation args:

```shell
oc get ds -n openshift-cluster-csi-drivers secrets-store-csi-driver-node -o jsonpath='{.spec.template.spec.containers[?(@.name=="csi-driver")].args}'
```

Inspect the `CSIDriver` object's `requiresRepublish`/`tokenRequests` fields:

```shell
oc get csidriver secrets-store.csi.k8s.io -o yaml
```

Confirm that `spec.requiresRepublish` and `spec.tokenRequests` match the
`ClusterCSIDriver` configuration above. If the operator fails to apply
either the `CSIDriver` or the DaemonSet, check the `ClusterCSIDriver`'s
status conditions for a `Degraded` condition.

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
