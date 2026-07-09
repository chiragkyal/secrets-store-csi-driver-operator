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

# The test namespace is created with a "random" postfix
POSTFIX_CHARS=$(echo $RANDOM | md5sum | head -c5)
export E2E_TEST_NAMESPACE=secrets-store-test-ns-${POSTFIX_CHARS}
export E2E_TEST_SERVICEACCOUNT_NAME=default
export E2E_TEST_SERVICEACCOUNT=system:serviceaccount:${E2E_TEST_NAMESPACE}:${E2E_TEST_SERVICEACCOUNT_NAME}
export E2E_TEST_PROVIDER=e2e-provider
export E2E_TEST_IMAGE=quay.io/openshifttest/busybox:multiarch
export E2E_TEST_POD_TIMEOUT=120 # seconds
export E2E_TEST_CONTAINER_NAME=test-container

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

# --- SSCSI-254: Configurable Secret Rotation e2e scenarios ---
export SECRETS_STORE_NODE_DAEMONSET="secrets-store-csi-driver-node"

# get_rotation_arg extracts the value of a --<flag>= arg from the csi-driver
# container's current args, e.g. get_rotation_arg enable-secret-rotation -> "true".
get_rotation_arg() {
	local FLAG_NAME=$1
	oc get ds -n ${E2E_PROVIDER_NAMESPACE} ${SECRETS_STORE_NODE_DAEMONSET} \
		-o jsonpath="{.spec.template.spec.containers[?(@.name==\"csi-driver\")].args}" \
		| grep -o -- "--${FLAG_NAME}=[^ ]*" | cut -d= -f2
}

get_requires_republish() {
	oc get csidriver ${PROVISIONER_NAME} -o jsonpath='{.spec.requiresRepublish}'
}

wait_for_node_daemonset_rollout() {
	oc rollout status daemonset/${SECRETS_STORE_NODE_DAEMONSET} -n ${E2E_PROVIDER_NAMESPACE} --timeout=60s
	return $?
}

# test_rotation_defaults verifies that with no driverConfig set on ClusterCSIDriver,
# rotation remains enabled at the operator's built-in default (specs.md FR-010).
test_rotation_defaults() {
	echo "Verifying default rotation behavior (no driverConfig set)"
	local REPUBLISH=$(get_requires_republish)
	if [ "${REPUBLISH}" != "true" ]; then
		echo "Expected requiresRepublish=true by default, got '${REPUBLISH}'"
		return 1
	fi
	local ENABLED=$(get_rotation_arg enable-secret-rotation)
	if [ "${ENABLED}" != "true" ]; then
		echo "Expected --enable-secret-rotation=true by default, got '${ENABLED}'"
		return 1
	fi
	echo "test_rotation_defaults PASSED"
	return 0
}

# test_rotation_custom_interval verifies secretRotation.type: Custom with a
# configured rotationPollIntervalSeconds propagates to both CSIDriver.requiresRepublish
# and the DaemonSet's --rotation-poll-interval arg (specs.md FR-002).
test_rotation_custom_interval() {
	local INTERVAL_SECONDS=300
	echo "Setting custom rotation interval to ${INTERVAL_SECONDS}s"
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=merge -p \
		"{\"spec\":{\"driverConfig\":{\"driverType\":\"SecretsStore\",\"secretsStore\":{\"secretRotation\":{\"type\":\"Custom\",\"custom\":{\"rotationPollIntervalSeconds\":${INTERVAL_SECONDS}}}}}}}" || return 1
	wait_for_node_daemonset_rollout || return 1

	local REPUBLISH=$(get_requires_republish)
	if [ "${REPUBLISH}" != "true" ]; then
		echo "Expected requiresRepublish=true with secretRotation.type=Custom, got '${REPUBLISH}'"
		return 1
	fi
	local INTERVAL=$(get_rotation_arg rotation-poll-interval)
	if [ "${INTERVAL}" != "${INTERVAL_SECONDS}s" ]; then
		echo "Expected --rotation-poll-interval=${INTERVAL_SECONDS}s, got '${INTERVAL}'"
		return 1
	fi
	echo "test_rotation_custom_interval PASSED"
	return 0
}

# test_rotation_disabled verifies secretRotation.type: None disables rotation on
# both the CSIDriver object and the DaemonSet args (specs.md FR-001, Edge Cases).
test_rotation_disabled() {
	echo "Disabling secret rotation (secretRotation.type: None)"
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=merge -p \
		'{"spec":{"driverConfig":{"driverType":"SecretsStore","secretsStore":{"secretRotation":{"type":"None"}}}}}' || return 1
	wait_for_node_daemonset_rollout || return 1

	local REPUBLISH=$(get_requires_republish)
	if [ "${REPUBLISH}" != "false" ]; then
		echo "Expected requiresRepublish=false with secretRotation.type=None, got '${REPUBLISH}'"
		return 1
	fi
	local ENABLED=$(get_rotation_arg enable-secret-rotation)
	if [ "${ENABLED}" != "false" ]; then
		echo "Expected --enable-secret-rotation=false with secretRotation.type=None, got '${ENABLED}'"
		return 1
	fi
	echo "test_rotation_disabled PASSED"
	return 0
}

# test_rotation_toggle_back_to_custom verifies that after disabling rotation,
# switching back to Custom re-enables it correctly (specs.md User Story 1,
# Acceptance Scenario 3).
test_rotation_toggle_back_to_custom() {
	local INTERVAL_SECONDS=300
	echo "Re-enabling rotation with Custom interval after having disabled it"
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=merge -p \
		"{\"spec\":{\"driverConfig\":{\"driverType\":\"SecretsStore\",\"secretsStore\":{\"secretRotation\":{\"type\":\"Custom\",\"custom\":{\"rotationPollIntervalSeconds\":${INTERVAL_SECONDS}}}}}}}" || return 1
	wait_for_node_daemonset_rollout || return 1

	local REPUBLISH=$(get_requires_republish)
	if [ "${REPUBLISH}" != "true" ]; then
		echo "Expected requiresRepublish=true after toggling back to Custom, got '${REPUBLISH}'"
		return 1
	fi
	local ENABLED=$(get_rotation_arg enable-secret-rotation)
	if [ "${ENABLED}" != "true" ]; then
		echo "Expected --enable-secret-rotation=true after toggling back to Custom, got '${ENABLED}'"
		return 1
	fi
	echo "test_rotation_toggle_back_to_custom PASSED"
	return 0
}

# test_rotation_cleanup removes the driverConfig set by the rotation tests above,
# restoring the singleton ClusterCSIDriver to its pre-test state for subsequent runs.
test_rotation_cleanup() {
	echo "Restoring ClusterCSIDriver to no driverConfig (cleanup)"
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=json -p '[{"op":"remove","path":"/spec/driverConfig"}]' 2>/dev/null
	wait_for_node_daemonset_rollout
	return 0
}

# --- SSCSI-254: Workload Identity Federation (WIF) token audience e2e scenarios ---
#
# IMPORTANT: tokenRequests.type is a one-way, CEL-enforced transition on the
# ClusterCSIDriver singleton -- once set to "Managed" it can NEVER revert to
# "Unmanaged" for the lifetime of this e2e run. Any scenario that depends on
# "Unmanaged" behavior (preserving pre-existing/externally-configured audiences,
# or an Unmanaged->Managed transition) MUST run and complete BEFORE the functions
# below. Do not insert new "Unmanaged"-dependent test calls after this block in
# the execution order at the bottom of this script.

get_token_requests_audiences() {
	oc get csidriver ${PROVISIONER_NAME} -o jsonpath='{.spec.tokenRequests[*].audience}'
}

get_token_request_expiration() {
	local AUDIENCE=$1
	oc get csidriver ${PROVISIONER_NAME} -o jsonpath="{.spec.tokenRequests[?(@.audience==\"${AUDIENCE}\")].expirationSeconds}"
}

# wait_for_csidriver_audiences polls until the CSIDriver's tokenRequests audiences
# match the expected space-separated list, tolerating the brief delete+recreate
# window from ApplyCSIDriver's spec-hash-based reconciliation.
wait_for_csidriver_audiences() {
	local EXPECTED="$1"
	local ATTEMPTS=0
	local CURRENT=""
	while [ ${ATTEMPTS} -lt 12 ]; do
		CURRENT=$(get_token_requests_audiences)
		if [ "${CURRENT}" == "${EXPECTED}" ]; then
			return 0
		fi
		sleep 5
		ATTEMPTS=$((ATTEMPTS + 1))
	done
	echo "Timed out waiting for tokenRequests audiences to become '${EXPECTED}', last saw '${CURRENT}'"
	return 1
}

# test_wif_managed_single_audience verifies a single Managed audience with a
# custom expirationSeconds propagates to CSIDriver.spec.tokenRequests
# (specs.md FR-003, SC-003).
test_wif_managed_single_audience() {
	local AWS_AUDIENCE="sts.amazonaws.com"
	echo "Configuring Managed tokenRequests with a single audience"
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=merge -p \
		"{\"spec\":{\"driverConfig\":{\"driverType\":\"SecretsStore\",\"secretsStore\":{\"tokenRequests\":{\"type\":\"Managed\",\"managed\":{\"audiences\":[{\"audience\":\"${AWS_AUDIENCE}\",\"expirationSeconds\":3600}]}}}}}}" || return 1

	wait_for_csidriver_audiences "${AWS_AUDIENCE}" || return 1
	local EXP=$(get_token_request_expiration ${AWS_AUDIENCE})
	if [ "${EXP}" != "3600" ]; then
		echo "Expected expirationSeconds=3600 for ${AWS_AUDIENCE}, got '${EXP}'"
		return 1
	fi
	echo "test_wif_managed_single_audience PASSED"
	return 0
}

# test_wif_managed_multiple_audiences verifies multiple simultaneous audiences
# (e.g. AWS + Azure) are all propagated together (specs.md FR-004/FR-011, SC-004).
test_wif_managed_multiple_audiences() {
	local AWS_AUDIENCE="sts.amazonaws.com"
	local AZURE_AUDIENCE="api://AzureADTokenExchange"
	echo "Configuring Managed tokenRequests with multiple audiences (AWS + Azure)"
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=merge -p \
		"{\"spec\":{\"driverConfig\":{\"driverType\":\"SecretsStore\",\"secretsStore\":{\"tokenRequests\":{\"type\":\"Managed\",\"managed\":{\"audiences\":[{\"audience\":\"${AWS_AUDIENCE}\",\"expirationSeconds\":3600},{\"audience\":\"${AZURE_AUDIENCE}\"}]}}}}}}" || return 1

	local ATTEMPTS=0
	local CURRENT=""
	while [ ${ATTEMPTS} -lt 12 ]; do
		CURRENT=$(get_token_requests_audiences)
		if [[ "${CURRENT}" == *"${AWS_AUDIENCE}"* ]] && [[ "${CURRENT}" == *"${AZURE_AUDIENCE}"* ]]; then
			echo "test_wif_managed_multiple_audiences PASSED"
			return 0
		fi
		sleep 5
		ATTEMPTS=$((ATTEMPTS + 1))
	done
	echo "Expected both '${AWS_AUDIENCE}' and '${AZURE_AUDIENCE}' present, last saw '${CURRENT}'"
	return 1
}

# test_wif_managed_clear_audiences verifies an explicit empty audience list
# clears all managed tokenRequests (specs.md FR-008, SC-007).
test_wif_managed_clear_audiences() {
	echo "Clearing Managed tokenRequests audiences (empty list)"
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=merge -p \
		'{"spec":{"driverConfig":{"driverType":"SecretsStore","secretsStore":{"tokenRequests":{"type":"Managed","managed":{"audiences":[]}}}}}}' || return 1

	wait_for_csidriver_audiences "" || return 1
	echo "test_wif_managed_clear_audiences PASSED"
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

test_teardown
if [ $? -ne 0 ]; then
	echo "test_teardown FAILED"
	exit 1
fi

# SSCSI-254: rotation scenarios run after the pod-mount test tears down, since
# each mutates the singleton ClusterCSIDriver and triggers a node DaemonSet
# rollout that could otherwise disrupt the concurrently-running pod-mount test.
test_rotation_defaults
if [ $? -ne 0 ]; then
	echo "test_rotation_defaults FAILED"
	exit 1
fi

test_rotation_custom_interval
if [ $? -ne 0 ]; then
	echo "test_rotation_custom_interval FAILED"
	test_rotation_cleanup
	exit 1
fi

test_rotation_disabled
if [ $? -ne 0 ]; then
	echo "test_rotation_disabled FAILED"
	test_rotation_cleanup
	exit 1
fi

test_rotation_toggle_back_to_custom
if [ $? -ne 0 ]; then
	echo "test_rotation_toggle_back_to_custom FAILED"
	test_rotation_cleanup
	exit 1
fi

test_rotation_cleanup

# NOTE (SSCSI-254): this is the LAST safe point to insert any e2e scenario that
# depends on tokenRequests.type == "Unmanaged" (e.g. preservation-on-upgrade
# scenarios). The WIF tests below permanently transition tokenRequests to
# "Managed" for the rest of this run (one-way CEL-enforced transition) -- any
# "Unmanaged"-dependent scenario added after this point would be unable to run.
test_wif_managed_single_audience
if [ $? -ne 0 ]; then
	echo "test_wif_managed_single_audience FAILED"
	exit 1
fi

test_wif_managed_multiple_audiences
if [ $? -ne 0 ]; then
	echo "test_wif_managed_multiple_audiences FAILED"
	exit 1
fi

test_wif_managed_clear_audiences
if [ $? -ne 0 ]; then
	echo "test_wif_managed_clear_audiences FAILED"
	exit 1
fi

echo "All tests PASSED"
exit 0
