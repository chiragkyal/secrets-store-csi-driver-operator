#!/bin/bash

set -o xtrace
set -o nounset
set -o pipefail

# The operator, driver, and e2e-provider pods must already be deployed on the cluster
# before running this test script.
export KUBECONFIG=${KUBECONFIG:-$HOME/.kube/config}
export E2E_PROVIDER_NAMESPACE=${E2E_PROVIDER_NAMESPACE:-openshift-cluster-csi-drivers}
export E2E_PROVIDER_APP_LABEL=${E2E_PROVIDER_APP_LABEL:-csi-secrets-store-e2e-provider}
export E2E_PROVIDER_SELECTOR="app=${E2E_PROVIDER_APP_LABEL}"
export PROVISIONER_NAME="secrets-store.csi.k8s.io"
export E2E_NODE_DAEMONSET_NAME=${E2E_NODE_DAEMONSET_NAME:-secrets-store-csi-driver-node}

# The test namespace is created with a "random" postfix
POSTFIX_CHARS=$(echo $RANDOM | md5sum | head -c5)
export E2E_TEST_NAMESPACE=secrets-store-test-ns-${POSTFIX_CHARS}
export E2E_TEST_SERVICEACCOUNT_NAME=default
export E2E_TEST_SERVICEACCOUNT=system:serviceaccount:${E2E_TEST_NAMESPACE}:${E2E_TEST_SERVICEACCOUNT_NAME}
export E2E_TEST_PROVIDER=e2e-provider
export E2E_TEST_IMAGE=quay.io/openshifttest/busybox:multiarch
export E2E_TEST_POD_TIMEOUT=120 # seconds
export E2E_TEST_CONTAINER_NAME=test-container
export E2E_RECONCILE_TIMEOUT=${E2E_RECONCILE_TIMEOUT:-180} # seconds

E2E_ORIGINAL_CLUSTER_CSI_DRIVER=""

# Check that CSI Driver and E2E Provider pods exist
test_prechecks() {
	echo "Running test prechecks"
	oc get csidriver ${PROVISIONER_NAME} || return 1
	oc wait pod -n ${E2E_PROVIDER_NAMESPACE} --selector=${E2E_PROVIDER_SELECTOR} --for=condition=Ready --timeout=30s || return 1
	echo "test_prechecks PASSED"
	return 0
}

test_setup() {
	echo "Creating test namespace"
	oc new-project ${E2E_TEST_NAMESPACE} || return 1

	E2E_ORIGINAL_CLUSTER_CSI_DRIVER=$(mktemp) || return 1
	oc get clustercsidriver ${PROVISIONER_NAME} -o yaml > "${E2E_ORIGINAL_CLUSTER_CSI_DRIVER}" || return 1

	# Allow creation of privileged pods for this test. The e2e-provider must be
	# privileged to bind to a unix domain socket on the host, and the test pod
	# must be privileged to read files created by the e2e-provider.
	oc adm policy add-scc-to-user privileged ${E2E_TEST_SERVICEACCOUNT} || return 1
	oc label ns ${E2E_TEST_NAMESPACE} security.openshift.io/scc.podSecurityLabelSync=false pod-security.kubernetes.io/enforce=privileged pod-security.kubernetes.io/audit=privileged pod-security.kubernetes.io/warn=privileged --overwrite || return 1

	echo "Creating SecretProviderClass"
	oc apply -f - <<EOF
apiVersion: secrets-store.csi.x-k8s.io/v1
kind: SecretProviderClass
metadata:
  name: ${E2E_TEST_PROVIDER}
  namespace: ${E2E_TEST_NAMESPACE}
spec:
  provider: ${E2E_TEST_PROVIDER}
  parameters:
    objects: |
      array:
        - |
          objectName: foo
          objectVersion: v1
        - |
          objectName: fookey
          objectVersion: v1
EOF
	return $?
}

test_teardown() {
	if [ -n "${E2E_ORIGINAL_CLUSTER_CSI_DRIVER}" ] && [ -f "${E2E_ORIGINAL_CLUSTER_CSI_DRIVER}" ]; then
		echo "Restoring original ClusterCSIDriver"
		oc apply -f "${E2E_ORIGINAL_CLUSTER_CSI_DRIVER}" || return 1
		rm -f "${E2E_ORIGINAL_CLUSTER_CSI_DRIVER}" || return 1
		E2E_ORIGINAL_CLUSTER_CSI_DRIVER=""
	fi

	echo "Deleting test namespace"
	oc delete project ${E2E_TEST_NAMESPACE}
	return $?
}

test_pods_dump() {
	echo "Describing pods in namespace ${E2E_TEST_NAMESPACE}"
	oc describe pods -n ${E2E_TEST_NAMESPACE}
	oc get pods -n ${E2E_TEST_NAMESPACE} -o yaml
	return 0
}

test_pod_create() {
	local TEST_POD_NAME=$1
	echo "Creating test pod ${TEST_POD_NAME}"
	oc apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: ${TEST_POD_NAME}
  namespace: ${E2E_TEST_NAMESPACE}
  labels:
    name: ${TEST_POD_NAME}
spec:
  serviceAccountName: ${E2E_TEST_SERVICEACCOUNT_NAME}
  containers:
  - name: ${E2E_TEST_CONTAINER_NAME}
    image: ${E2E_TEST_IMAGE}
    command:
    - sh
    - -c
    - cat /mnt/test-vol/foo && sleep ${E2E_TEST_POD_TIMEOUT}
    securityContext:
      privileged: true
    volumeMounts:
    - mountPath: /mnt/test-vol
      name: test-vol
      readOnly: true
    terminationMessagePolicy: FallbackToLogsOnError
  volumes:
  - csi:
      driver: ${PROVISIONER_NAME}
      readOnly: true
      volumeAttributes:
        secretProviderClass: ${E2E_TEST_PROVIDER}
    name: test-vol
EOF
	return $?
}

test_pod_delete() {
	local TEST_POD_NAME=$1
	echo "Deleting test pod ${TEST_POD_NAME}"
	oc delete pod/${TEST_POD_NAME} -n ${E2E_TEST_NAMESPACE}
	return $?
}

test_pod_wait() {
	local TEST_POD_NAME=$1
	echo "Waiting for pod ${TEST_POD_NAME} to be ready"
	oc wait pod -n ${E2E_TEST_NAMESPACE} --selector=name=${TEST_POD_NAME} --for=condition=Ready --timeout=${E2E_TEST_POD_TIMEOUT}s
	return $?
}

test_pod_log_check() {
	local TEST_POD_NAME=$1
	echo "Checking logs of pod ${TEST_POD_NAME} for secret value"
	LOG_CONTENTS=$(oc logs pod/${TEST_POD_NAME} -n ${E2E_TEST_NAMESPACE} -c ${E2E_TEST_CONTAINER_NAME})
	EXPECTED_VALUE=secret
	if [ "${LOG_CONTENTS}" != "${EXPECTED_VALUE}" ]; then
		echo "Log contents do not match expected value: ${EXPECTED_VALUE}"
		return 1
	fi
	return 0
}

test_pod_with_secret() {
	local TEST_POD_NAME=test-pod-with-secret
	test_pod_create ${TEST_POD_NAME} || return 1
	test_pod_wait ${TEST_POD_NAME} || return 1
	test_pods_dump
	test_pod_log_check ${TEST_POD_NAME} || return 1
	test_pod_delete ${TEST_POD_NAME} || return 1
	echo "test_pod_with_secret PASSED"
	return 0
}

wait_for_driver_args_contains() {
	local EXPECTED=$1
	local DESCRIPTION=$2
	local ATTEMPTS=$((E2E_RECONCILE_TIMEOUT / 5))

	for _ in $(seq 1 ${ATTEMPTS}); do
		ARGS=$(oc get ds -n ${E2E_PROVIDER_NAMESPACE} ${E2E_NODE_DAEMONSET_NAME} -o jsonpath='{.spec.template.spec.containers[?(@.name=="csi-driver")].args}') || return 1
		if [[ "${ARGS}" == *"${EXPECTED}"* ]]; then
			return 0
		fi
		sleep 5
	done

	echo "Timed out waiting for daemonset args to contain ${EXPECTED} (${DESCRIPTION})"
	return 1
}

wait_for_requires_republish() {
	local EXPECTED=$1
	local ATTEMPTS=$((E2E_RECONCILE_TIMEOUT / 5))

	for _ in $(seq 1 ${ATTEMPTS}); do
		ACTUAL=$(oc get csidriver ${PROVISIONER_NAME} -o jsonpath='{.spec.requiresRepublish}') || return 1
		if [ "${ACTUAL}" = "${EXPECTED}" ]; then
			return 0
		fi
		sleep 5
	done

	echo "Timed out waiting for requiresRepublish=${EXPECTED}"
	return 1
}

wait_for_token_audiences() {
	local EXPECTED=$1
	local ATTEMPTS=$((E2E_RECONCILE_TIMEOUT / 5))

	for _ in $(seq 1 ${ATTEMPTS}); do
		ACTUAL=$(oc get csidriver ${PROVISIONER_NAME} -o jsonpath='{range .spec.tokenRequests[*]}{.audience}{"\n"}{end}') || return 1
		if [ "${ACTUAL}" = "${EXPECTED}" ]; then
			return 0
		fi
		sleep 5
	done

	echo "Timed out waiting for token audiences to match expected value"
	printf 'Expected:\n%s\n' "${EXPECTED}"
	printf 'Actual:\n%s\n' "${ACTUAL}"
	return 1
}

test_default_rotation() {
	echo "Testing default rotation behavior"
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=merge -p '{"spec":{"driverConfig":null}}' || return 1
	wait_for_requires_republish "true" || return 1
	wait_for_driver_args_contains "--enable-secret-rotation=true" "default rotation enabled" || return 1
	wait_for_driver_args_contains "--rotation-poll-interval=2m" "default rotation interval" || return 1
	echo "test_default_rotation PASSED"
	return 0
}

test_disabled_rotation() {
	echo "Testing disabled rotation behavior"
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=merge -p '{"spec":{"driverConfig":{"driverType":"SecretsStore","secretsStore":{"secretRotation":{"type":"None"}}}}}' || return 1
	wait_for_requires_republish "false" || return 1
	wait_for_driver_args_contains "--enable-secret-rotation=false" "rotation disabled" || return 1
	echo "test_disabled_rotation PASSED"
	return 0
}

test_custom_rotation() {
	echo "Testing custom rotation interval behavior"
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=merge -p '{"spec":{"driverConfig":{"driverType":"SecretsStore","secretsStore":{"secretRotation":{"type":"Custom","custom":{"rotationPollIntervalSeconds":300}}}}}}' || return 1
	wait_for_requires_republish "true" || return 1
	wait_for_driver_args_contains "--enable-secret-rotation=true" "custom rotation enabled" || return 1
	wait_for_driver_args_contains "--rotation-poll-interval=5m0s" "custom rotation interval" || return 1
	echo "test_custom_rotation PASSED"
	return 0
}

test_managed_token_requests() {
	echo "Testing managed token requests behavior"
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=merge -p '{"spec":{"driverConfig":{"driverType":"SecretsStore","secretsStore":{"tokenRequests":{"type":"Managed","managed":{"audiences":[{"audience":"sts.amazonaws.com","expirationSeconds":3600}]}}}}}}' || return 1
	wait_for_token_audiences $'sts.amazonaws.com\n' || return 1
	echo "test_managed_token_requests PASSED"
	return 0
}

test_cleared_token_requests() {
	echo "Testing cleared managed token requests behavior"
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=merge -p '{"spec":{"driverConfig":{"driverType":"SecretsStore","secretsStore":{"tokenRequests":{"type":"Managed","managed":{"audiences":[]}}}}}}' || return 1
	wait_for_token_audiences "" || return 1
	echo "test_cleared_token_requests PASSED"
	return 0
}

test_multiple_token_requests() {
	echo "Testing multiple managed token audiences behavior"
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=merge -p '{"spec":{"driverConfig":{"driverType":"SecretsStore","secretsStore":{"tokenRequests":{"type":"Managed","managed":{"audiences":[{"audience":"sts.amazonaws.com","expirationSeconds":3600},{"audience":"api://AzureADTokenExchange"}]}}}}}}' || return 1
	wait_for_token_audiences $'sts.amazonaws.com\napi://AzureADTokenExchange\n' || return 1
	echo "test_multiple_token_requests PASSED"
	return 0
}

test_upgrade_preserves_existing_token_requests() {
	echo "Testing upgrade-style preservation of existing token requests"
	oc patch csidriver ${PROVISIONER_NAME} --type=merge -p '{"spec":{"tokenRequests":[{"audience":"api://AzureADTokenExchange","expirationSeconds":3600}]}}' || return 1
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=merge -p '{"spec":{"driverConfig":null}}' || return 1
	wait_for_token_audiences $'api://AzureADTokenExchange\n' || return 1
	echo "test_upgrade_preserves_existing_token_requests PASSED"
	return 0
}

test_invalid_configuration_sets_degraded() {
	echo "Testing invalid configuration rejection"
	if oc patch clustercsidriver ${PROVISIONER_NAME} --type=merge -p '{"spec":{"driverConfig":{"driverType":"SecretsStore","secretsStore":{"secretRotation":{"type":"Custom","custom":{"rotationPollIntervalSeconds":0}}}}}}'; then
		echo "Expected invalid ClusterCSIDriver patch to fail validation"
		return 1
	fi
	echo "test_invalid_configuration_sets_degraded PASSED"
	return 0
}

test_rotation_and_wif_configuration() {
	test_default_rotation || return 1
	test_disabled_rotation || return 1
	test_custom_rotation || return 1
	test_managed_token_requests || return 1
	test_cleared_token_requests || return 1
	test_multiple_token_requests || return 1
	test_upgrade_preserves_existing_token_requests || return 1
	test_invalid_configuration_sets_degraded || return 1
	echo "test_rotation_and_wif_configuration PASSED"
	return 0
}

test_prechecks
if [ $? -ne 0 ]; then
	echo "test_prechecks FAILED"
	exit 1
fi

test_setup
if [ $? -ne 0 ]; then
	echo "test_setup FAILED"
	test_teardown
	exit 1
fi

test_pod_with_secret
if [ $? -ne 0 ]; then
	echo "test_pod_with_secret FAILED"
	test_pods_dump
	test_teardown
	exit 1
fi

test_rotation_and_wif_configuration
if [ $? -ne 0 ]; then
	echo "test_rotation_and_wif_configuration FAILED"
	test_pods_dump
	test_teardown
	exit 1
fi

test_teardown
if [ $? -ne 0 ]; then
	echo "test_teardown FAILED"
	exit 1
fi

echo "All tests PASSED"
exit 0
