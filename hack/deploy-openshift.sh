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

set -eu

usage() {
	cat <<EOF
Deploy the DRA CPU driver on an OpenShift cluster.

Environment variables:
  RELEASE_NAME  Helm release name (default: dra-driver-cpu)
  NAMESPACE     Namespace for the driver (default: openshift-dra-driver-cpu)
  CHART         Helm chart path (default: deployment/helm/dra-driver-cpu)
  IMAGE_REPOSITORY  Driver image repository (default: registry.k8s.io/dra-driver-cpu/dra-driver-cpu)
  IMAGE_TAG         Driver image tag (default: v0.2.0)
  TIMEOUT       Helm and rollout timeout (default: 5m)
  KUBECONFIG    Required path to the kubeconfig file

Additional arguments can be passed to 'helm upgrade --install', for example:
  $0 --set image.pullPolicy=Always
EOF
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
	usage
	exit 0
fi

SCRIPT_ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)

# image variables
IMAGE_REPOSITORY=${IMAGE_REPOSITORY:-registry.k8s.io/dra-driver-cpu/dra-driver-cpu}
IMAGE_TAG=${IMAGE_TAG:-v0.2.0}

# helm deployment variables
CHART=${CHART:-${SCRIPT_ROOT}/deployment/helm/dra-driver-cpu}
VALUES_FILE=${VALUES_FILE:-${CHART}/values-openshift.yaml}

# cluster variables
KUBECONFIG=${KUBECONFIG:-~/.kube/config}
NAMESPACE=${NAMESPACE:-openshift-dra-driver-cpu}

# additional variables
TIMEOUT=${TIMEOUT:-2m}
RELEASE_NAME=${RELEASE_NAME:-dra-driver-cpu}

if [ ! -f "${KUBECONFIG}" ]; then
	echo "error: KUBECONFIG file not found: ${KUBECONFIG}" >&2
	exit 1
fi

for command in helm oc; do
	if ! command -v "${command}" >/dev/null 2>&1; then
		echo "error: ${command} command not found" >&2
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

if ! oc get namespace "${NAMESPACE}" >/dev/null 2>&1; then
	echo "Creating namespace ${NAMESPACE}"
	oc create namespace "${NAMESPACE}"
fi

# The driver requires hostNetwork and hostPath volumes, which are not allowed
# by the restricted Pod Security profile. This is a dedicated namespace for a
# node-level driver, so use the privileged profile for all Pod Security levels.
oc label namespace "${NAMESPACE}" \
	pod-security.kubernetes.io/enforce=privileged \
	pod-security.kubernetes.io/audit=privileged \
	pod-security.kubernetes.io/warn=privileged \
	--overwrite >/dev/null

echo "Deploying ${RELEASE_NAME} to namespace ${NAMESPACE}"
helm upgrade --install "${RELEASE_NAME}" "${CHART}" \
	--namespace "${NAMESPACE}" \
	--create-namespace \
	--values "${VALUES_FILE}" \
	--skip-schema-validation \
	--set "image.repository=${IMAGE_REPOSITORY}" \
	--set "image.tag=${IMAGE_TAG}" \
	--wait \
	--timeout "${TIMEOUT}" \
	"$@"

echo "OpenShift deployment completed successfully"
