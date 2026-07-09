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
export DS_NAME="secrets-store-csi-driver-node"
export DS_CONTAINER_NAME="csi-driver"
export ROTATION_WAIT_TIMEOUT=180 # seconds; time allowed for the operator to reconcile a ClusterCSIDriver change into the DaemonSet

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

# --- Secret rotation configuration tests (US1/US3, SC-001/SC-002) ---
#
# These tests drive rotation behavior by patching the cluster-scoped
# ClusterCSIDriver (${PROVISIONER_NAME}) and observing the resulting
# "--enable-secret-rotation=" / "--rotation-poll-interval=" args on the
# csi-driver container of the ${DS_NAME} DaemonSet, which is the operator's
# own reconciliation surface for this feature (see
# pkg/operator/rotation.go:WithSecretRotationDaemonSetHook). Mutating the
# e2e-provider's returned secret VALUE to observe an actual refresh is out of
# scope: this repo's e2e-provider has no such control today.

# test_driver_config_save captures the ClusterCSIDriver's current
# spec.driverConfig into the global ORIGINAL_DRIVER_CONFIG so it can be
# restored byte-for-byte afterwards, regardless of what was configured
# (or not) before the test ran.
test_driver_config_save() {
	echo "Saving original ClusterCSIDriver ${PROVISIONER_NAME} driverConfig"
	ORIGINAL_DRIVER_CONFIG=$(oc get clustercsidriver ${PROVISIONER_NAME} -o jsonpath='{.spec.driverConfig}')
	return $?
}

# test_driver_config_restore restores spec.driverConfig to the value captured
# by test_driver_config_save, using a JSON patch "replace" (not a merge
# patch) so the field is fully reset rather than recursively merged.
test_driver_config_restore() {
	echo "Restoring original ClusterCSIDriver ${PROVISIONER_NAME} driverConfig"
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=json -p "[{\"op\":\"replace\",\"path\":\"/spec/driverConfig\",\"value\":${ORIGINAL_DRIVER_CONFIG}}]" || return 1
	test_wait_ds_arg "--enable-secret-rotation="
	return $?
}

# test_get_ds_container_args prints the current args of the csi-driver
# container on the ${DS_NAME} DaemonSet.
test_get_ds_container_args() {
	oc get daemonset ${DS_NAME} -n ${E2E_PROVIDER_NAMESPACE} -o jsonpath="{.spec.template.spec.containers[?(@.name=='${DS_CONTAINER_NAME}')].args}"
}

# test_wait_ds_arg polls (up to ROTATION_WAIT_TIMEOUT seconds) until the
# csi-driver container's args contain the given substring, then waits for the
# DaemonSet rollout triggered by that spec change to finish. Polling (rather
# than a single check right after patching) is required because the operator
# reconciles the ClusterCSIDriver change on its own sync loop, not
# synchronously with `oc patch`.
test_wait_ds_arg() {
	local EXPECTED_ARG=$1
	local ELAPSED=0
	local INTERVAL=5
	local DS_ARGS=""
	echo "Waiting (up to ${ROTATION_WAIT_TIMEOUT}s) for ${DS_NAME} container ${DS_CONTAINER_NAME} args to contain: ${EXPECTED_ARG}"
	while [ ${ELAPSED} -lt ${ROTATION_WAIT_TIMEOUT} ]; do
		DS_ARGS=$(test_get_ds_container_args)
		if echo "${DS_ARGS}" | grep -q -- "${EXPECTED_ARG}"; then
			oc rollout status daemonset/${DS_NAME} -n ${E2E_PROVIDER_NAMESPACE} --timeout=${ROTATION_WAIT_TIMEOUT}s
			return $?
		fi
		sleep ${INTERVAL}
		ELAPSED=$((ELAPSED + INTERVAL))
	done
	echo "Timed out waiting for arg \"${EXPECTED_ARG}\"; last observed args: ${DS_ARGS}"
	return 1
}

# test_rotation_toggle covers US1/SC-001: disabling rotation on a live driver
# stops the operator from advertising rotation as enabled, and re-enabling it
# resumes advertising rotation as enabled again -- without restarting any
# workload pod using the driver.
test_rotation_toggle() {
	echo "Running test_rotation_toggle"
	test_driver_config_save || return 1

	echo "Disabling secret rotation via ClusterCSIDriver ${PROVISIONER_NAME}"
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=merge -p '{"spec":{"driverConfig":{"driverType":"SecretsStore","secretsStore":{"secretRotation":{"type":"None"}}}}}' || {
		test_driver_config_restore
		return 1
	}
	test_wait_ds_arg "--enable-secret-rotation=false" || {
		test_driver_config_restore
		return 1
	}
	echo "Confirmed driver DaemonSet reconciled with rotation disabled"

	echo "Re-enabling secret rotation via ClusterCSIDriver ${PROVISIONER_NAME}"
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=merge -p '{"spec":{"driverConfig":{"driverType":"SecretsStore","secretsStore":{"secretRotation":{"type":"Custom","custom":{"minimumRefreshAge":120}}}}}}' || {
		test_driver_config_restore
		return 1
	}
	test_wait_ds_arg "--enable-secret-rotation=true" || {
		test_driver_config_restore
		return 1
	}
	echo "Confirmed driver DaemonSet reconciled with rotation re-enabled"

	test_driver_config_restore || return 1
	echo "test_rotation_toggle PASSED"
	return 0
}

# test_rotation_custom_interval covers US3/SC-002: setting a custom rotation
# interval is reflected in the driver's --rotation-poll-interval arg.
test_rotation_custom_interval() {
	echo "Running test_rotation_custom_interval"
	local CUSTOM_INTERVAL_SECONDS=30
	local EXPECTED_ARG="--rotation-poll-interval=${CUSTOM_INTERVAL_SECONDS}s"

	test_driver_config_save || return 1

	echo "Setting custom rotation interval (${CUSTOM_INTERVAL_SECONDS}s) via ClusterCSIDriver ${PROVISIONER_NAME}"
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=merge -p "{\"spec\":{\"driverConfig\":{\"driverType\":\"SecretsStore\",\"secretsStore\":{\"secretRotation\":{\"type\":\"Custom\",\"custom\":{\"minimumRefreshAge\":${CUSTOM_INTERVAL_SECONDS}}}}}}}" || {
		test_driver_config_restore
		return 1
	}
	test_wait_ds_arg "${EXPECTED_ARG}" || {
		test_driver_config_restore
		return 1
	}
	echo "Confirmed driver DaemonSet reconciled with custom rotation interval ${CUSTOM_INTERVAL_SECONDS}s"

	test_driver_config_restore || return 1
	echo "test_rotation_custom_interval PASSED"
	return 0
}

# --- Workload Identity Federation (WIF) token audience tests (US2/US4, SC-003/SC-004) ---
#
# Per this task's Non-goals, these tests verify the storage.k8s.io CSIDriver
# object's spec.tokenRequests field and that a workload can still mount a
# secret via the driver while tokenRequests is configured -- NOT a full
# cloud-provider round-trip authentication (AWS STS / Azure AD), which is
# outside this operator's scope (repo-assessment.md §10.3) and would require
# test infrastructure this repo's e2e-provider does not provide.

# test_wait_csidriver_audiences polls (up to ROTATION_WAIT_TIMEOUT seconds)
# until CSIDriver ${PROVISIONER_NAME}'s spec.tokenRequests audiences exactly
# match the given space-separated list (order-independent; pass "" to wait
# for an empty list).
test_wait_csidriver_audiences() {
	local EXPECTED_SORTED
	EXPECTED_SORTED=$(echo "$1" | tr ' ' '\n' | sort)
	local ELAPSED=0
	local INTERVAL=5
	local ACTUAL=""
	echo "Waiting (up to ${ROTATION_WAIT_TIMEOUT}s) for CSIDriver ${PROVISIONER_NAME} tokenRequests audiences to be: [$1]"
	while [ ${ELAPSED} -lt ${ROTATION_WAIT_TIMEOUT} ]; do
		ACTUAL=$(oc get csidriver ${PROVISIONER_NAME} -o jsonpath='{.spec.tokenRequests[*].audience}')
		if [ "$(echo "${ACTUAL}" | tr ' ' '\n' | sort)" = "${EXPECTED_SORTED}" ]; then
			echo "Confirmed CSIDriver tokenRequests audiences: [${ACTUAL}]"
			return 0
		fi
		sleep ${INTERVAL}
		ELAPSED=$((ELAPSED + INTERVAL))
	done
	echo "Timed out waiting for tokenRequests audiences [$1]; last observed: [${ACTUAL}]"
	return 1
}

# test_wif_clear_audiences explicitly clears all operator-managed token
# audiences by submitting an empty managed audiences list (FR-007). This is
# the ONLY valid cleanup path once tokenRequests.type has been set to
# "Managed": FR-006 makes that discriminator irreversible back to
# "Unmanaged", so a plain driverConfig restore (as test_driver_config_restore
# does for rotation) cannot, by design, un-set Managed status -- it can only
# return the audience list to empty, which is functionally equivalent to "no
# audiences configured".
test_wif_clear_audiences() {
	echo "Clearing operator-managed token audiences via ClusterCSIDriver ${PROVISIONER_NAME} (FR-007)"
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=merge -p '{"spec":{"driverConfig":{"driverType":"SecretsStore","secretsStore":{"tokenRequests":{"type":"Managed","managed":{"audiences":[]}}}}}}' || return 1
	test_wait_csidriver_audiences ""
	return $?
}

# test_wif_mount_check confirms a workload can still successfully mount a
# secret via the driver while tokenRequests is configured -- i.e. that
# configuring WIF audiences causes no disruption to the driver's core
# mount path (reuses the same pod fixture as test_pod_with_secret).
test_wif_mount_check() {
	local TEST_POD_NAME=$1
	test_pod_create ${TEST_POD_NAME} || return 1
	test_pod_wait ${TEST_POD_NAME} || return 1
	test_pod_log_check ${TEST_POD_NAME} || return 1
	test_pod_delete ${TEST_POD_NAME} || return 1
	return 0
}

# test_wif_single_audience covers US2/SC-003: configuring a single managed
# token audience is reflected on the CSIDriver object, and a workload can
# still mount a secret via the driver while it is configured.
test_wif_single_audience() {
	echo "Running test_wif_single_audience"
	test_driver_config_save || return 1

	echo "Configuring a single managed token audience via ClusterCSIDriver ${PROVISIONER_NAME}"
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=merge -p '{"spec":{"driverConfig":{"driverType":"SecretsStore","secretsStore":{"tokenRequests":{"type":"Managed","managed":{"audiences":[{"audience":"openshift-wif-e2e"}]}}}}}}' || {
		test_wif_clear_audiences
		test_driver_config_restore
		return 1
	}
	test_wait_csidriver_audiences "openshift-wif-e2e" || {
		test_wif_clear_audiences
		test_driver_config_restore
		return 1
	}

	echo "Confirming a workload can still mount a secret via the driver with a single tokenRequests audience configured"
	test_wif_mount_check test-pod-wif-single || {
		test_wif_clear_audiences
		test_driver_config_restore
		return 1
	}

	test_wif_clear_audiences || return 1
	test_driver_config_restore || return 1
	echo "test_wif_single_audience PASSED"
	return 0
}

# test_wif_multi_audience covers US4/SC-004: configuring multiple managed
# token audiences (e.g. AWS + Azure) is reflected on the CSIDriver object
# with both audiences present simultaneously, and a workload can still mount
# a secret via the driver while they are configured.
test_wif_multi_audience() {
	echo "Running test_wif_multi_audience"
	test_driver_config_save || return 1

	echo "Configuring multiple managed token audiences (AWS + Azure) via ClusterCSIDriver ${PROVISIONER_NAME}"
	oc patch clustercsidriver ${PROVISIONER_NAME} --type=merge -p '{"spec":{"driverConfig":{"driverType":"SecretsStore","secretsStore":{"tokenRequests":{"type":"Managed","managed":{"audiences":[{"audience":"sts.amazonaws.com","expirationSeconds":3600},{"audience":"api://AzureADTokenExchange"}]}}}}}}' || {
		test_wif_clear_audiences
		test_driver_config_restore
		return 1
	}
	test_wait_csidriver_audiences "sts.amazonaws.com api://AzureADTokenExchange" || {
		test_wif_clear_audiences
		test_driver_config_restore
		return 1
	}

	echo "Confirming a workload can still mount a secret via the driver with multiple tokenRequests audiences configured"
	test_wif_mount_check test-pod-wif-multi || {
		test_wif_clear_audiences
		test_driver_config_restore
		return 1
	}

	test_wif_clear_audiences || return 1
	test_driver_config_restore || return 1
	echo "test_wif_multi_audience PASSED"
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

test_rotation_toggle
if [ $? -ne 0 ]; then
	echo "test_rotation_toggle FAILED"
	test_pods_dump
	test_teardown
	exit 1
fi

test_rotation_custom_interval
if [ $? -ne 0 ]; then
	echo "test_rotation_custom_interval FAILED"
	test_pods_dump
	test_teardown
	exit 1
fi

test_wif_single_audience
if [ $? -ne 0 ]; then
	echo "test_wif_single_audience FAILED"
	test_pods_dump
	test_teardown
	exit 1
fi

test_wif_multi_audience
if [ $? -ne 0 ]; then
	echo "test_wif_multi_audience FAILED"
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
