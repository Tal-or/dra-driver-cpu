#!/bin/sh

# Copyright The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

usage() {
	cat <<EOF
Configure an OpenShift cluster for DRA CPU management and deploy the driver.

When NODE_GROUP is set the script first runs the openshift-config tool to
discover reserved CPUs from KubeletConfig and annotate the matching
PerformanceProfile for DRA, then deploys the driver. When NODE_GROUP is not
set, RESERVED_CPUS must be provided directly.

Required environment variables:
  KUBECONFIG            Path to the kubeconfig file.
  IMAGE_REPOSITORY      Driver image repository (e.g. quay.io/org/dra-driver-cpu).

One of the following is required:
  NODE_GROUP            MachineConfigPool name for reserved-CPU discovery.
  RESERVED_CPUS         Reserved CPU set for the driver (e.g. 0-3).

Optional environment variables:
  IMAGE_TAG               Driver image tag (default: latest).
  CPU_DEVICE_MODE         CPU device mode (default: grouped).
  CPU_GROUP_BY            CPU grouping (default: numanode).
  NAMESPACE               Namespace for the driver (default: openshift-dra-driver-cpu).
  RELEASE_NAME            Helm release name (default: dra-driver-cpu).
  CHART                   Helm chart path (default: deployment/helm/dra-driver-cpu).
  VALUES_FILE             Helm values file (default: <chart>/values-openshift.yaml).
  TIMEOUT                 Helm and rollout timeout (default: 2m).
  ENABLE_DRA_ANNOTATION   Set to "true" to annotate the PerformanceProfile for DRA (default: false).
  DRY_RUN                 Set to "true" to skip the PerformanceProfile annotation write.

Additional arguments are forwarded to 'helm upgrade --install', for example:
  $0 --set image.pullPolicy=Always
EOF
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
	usage
	exit 0
fi

set -eu

# ── Repository root ─────────────────────────────────────────────────────────
REPO_ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)

# ── Required variables ──────────────────────────────────────────────────────
: "${KUBECONFIG:?KUBECONFIG is required}"
: "${IMAGE_REPOSITORY:?IMAGE_REPOSITORY is required}"

# ── Optional variables with defaults ────────────────────────────────────────
IMAGE_TAG=${IMAGE_TAG:-latest}
NAMESPACE=${NAMESPACE:-openshift-dra-driver-cpu}
RELEASE_NAME=${RELEASE_NAME:-dra-driver-cpu}
TIMEOUT=${TIMEOUT:-2m}
CPU_DEVICE_MODE=${CPU_DEVICE_MODE:-grouped}
CPU_GROUP_BY=${CPU_GROUP_BY:-numanode}
CHART=${CHART:-${REPO_ROOT}/deployment/helm/dra-driver-cpu}
VALUES_FILE=${VALUES_FILE:-${CHART}/values-openshift.yaml}
DRY_RUN=${DRY_RUN:-false}
ENABLE_DRA_ANNOTATION=${ENABLE_DRA_ANNOTATION:-false}

# ── Validation ──────────────────────────────────────────────────────────────
if [ ! -f "${KUBECONFIG}" ]; then
	echo "error: KUBECONFIG file not found: ${KUBECONFIG}" >&2
	exit 1
fi

for _cmd in helm oc; do
	if ! command -v "${_cmd}" >/dev/null 2>&1; then
		echo "error: ${_cmd} command not found" >&2
		exit 1
	fi
done

if ! oc whoami >/dev/null 2>&1; then
	echo "error: unable to access OpenShift with KUBECONFIG; verify the file, current context, credentials, and cluster access" >&2
	exit 1
fi

if [ ! -d "${CHART}" ]; then
	echo "error: Helm chart directory not found: ${CHART}" >&2
	exit 1
fi

if [ ! -f "${VALUES_FILE}" ]; then
	echo "error: OpenShift values file not found: ${VALUES_FILE}" >&2
	exit 1
fi

# ── Step 1 (optional): Discover reserved CPUs via openshift-config ──────────
if [ -n "${NODE_GROUP:-}" ]; then
	CONFIG_FLAGS="--kubeconfig=${KUBECONFIG} --node-group=${NODE_GROUP}"
	if [ "${ENABLE_DRA_ANNOTATION}" = "true" ]; then
		CONFIG_FLAGS="${CONFIG_FLAGS} --enable-dra-annotation"
	fi
	if [ "${DRY_RUN}" = "true" ]; then
		CONFIG_FLAGS="${CONFIG_FLAGS} --dry-run"
	fi

	echo "Discovering reserved CPUs for node group '${NODE_GROUP}'..."
	# shellcheck disable=SC2086
	if ! reserved=$(go -C "${REPO_ROOT}/tools/openshiftconfig" run ./cmd/openshift-config ${CONFIG_FLAGS}); then
		echo "error: openshift-config failed" >&2
		exit 1
	fi
	if [ -z "${reserved}" ]; then
		echo "error: openshift-config returned empty reserved CPUs" >&2
		exit 1
	fi
	RESERVED_CPUS="${reserved}"
	echo "Reserved CPUs: ${RESERVED_CPUS}"
fi

: "${RESERVED_CPUS:?Either NODE_GROUP or RESERVED_CPUS is required}"

# ── Step 2: Deploy the DRA CPU driver ───────────────────────────────────────
if ! oc get namespace "${NAMESPACE}" >/dev/null 2>&1; then
	echo "Creating namespace ${NAMESPACE}"
	oc create namespace "${NAMESPACE}"
fi

oc label namespace "${NAMESPACE}" \
	pod-security.kubernetes.io/enforce=privileged \
	pod-security.kubernetes.io/audit=privileged \
	pod-security.kubernetes.io/warn=privileged \
	--overwrite >/dev/null

echo "Deploying ${RELEASE_NAME} to namespace ${NAMESPACE} (image: ${IMAGE_REPOSITORY}:${IMAGE_TAG})"
helm upgrade --install "${RELEASE_NAME}" "${CHART}" \
	--namespace "${NAMESPACE}" \
	--create-namespace \
	--values "${VALUES_FILE}" \
	--skip-schema-validation \
	--set "image.repository=${IMAGE_REPOSITORY}" \
	--set "image.tag=${IMAGE_TAG}" \
	--set-literal "driverConfig.reservedCPUs=${RESERVED_CPUS}" \
	--set "driverConfig.cpuDeviceMode=${CPU_DEVICE_MODE}" \
	--set "driverConfig.groupBy=${CPU_GROUP_BY}" \
	--set "podLabels.app=dracpu" \
	--set "fullnameOverride=dracpu" \
	--wait \
	--timeout "${TIMEOUT}" \
	"$@"

echo "OpenShift deployment completed successfully"
