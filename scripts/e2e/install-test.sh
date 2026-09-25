#!/usr/bin/env bash
#
# Installs the chart into whatever cluster kubectl points at and checks the
# nodes came out configured. Distro-agnostic: the cluster and the image are
# somebody else's job, this asserts what the chart and the installer did.
#
# Usage: install-test.sh <profile> [extra helm --set arguments...]

set -euo pipefail

# shellcheck source=scripts/e2e/lib/common.sh
source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/lib/common.sh"

PROFILE=${1:?profile to install with, for example k3d}
shift || true

need kubectl helm

CHART_DIR="${REPO_ROOT}/deploy/helm/credential-provider-harbor"

cleanup() {
  local rc=$?
  if [ "${rc}" -ne 0 ]; then
    dump_state
    [ -z "${E2E_CLUSTER_SCRIPT:-}" ] || "${E2E_CLUSTER_SCRIPT}" dump-node || true
  fi
  if [ -n "${E2E_KEEP:-}" ]; then
    log "E2E_KEEP set, leaving the release installed"
    return "${rc}"
  fi
  log "uninstalling"
  helm uninstall "${E2E_RELEASE}" -n "${E2E_NAMESPACE}" --wait --timeout 2m >/dev/null 2>&1 || true
  return "${rc}"
}
trap cleanup EXIT

log "installing profile=${PROFILE} from ${CHART_DIR}"
helm upgrade --install "${E2E_RELEASE}" "${CHART_DIR}" \
  --namespace "${E2E_NAMESPACE}" \
  --set profile="${PROFILE}" \
  --set registry.host="${E2E_REGISTRY_HOST}" \
  --set registry.audience="${E2E_REGISTRY_HOST}" \
  --set image.repository="${E2E_IMAGE_REPO}" \
  --set image.tag="${E2E_IMAGE_TAG}" \
  --set image.pullPolicy=Never \
  "$@" \
  --wait --timeout 5m

# The readiness probe greps the marker for this install's ID, so a rolled-out
# DaemonSet already means every node finished installing.
log "waiting for the rollout"
kubectl rollout status "daemonset/${E2E_RELEASE}" -n "${E2E_NAMESPACE}" --timeout=5m

# A distro whose nodes have no init system cannot have its kubelet restarted
# from inside the cluster. The cluster script does it instead, before anything
# is verified.
if [ -n "${E2E_CLUSTER_SCRIPT:-}" ]; then
  "${E2E_CLUSTER_SCRIPT}" restart-nodes
fi

# A kubelet restart takes the node NotReady for a few seconds, and everything
# below needs a pod on it.
log "waiting for the nodes"
wait_for_nodes 5m

log "installer logs"
kubectl logs -n "${E2E_NAMESPACE}" -l "app.kubernetes.io/name=credential-provider-harbor" --tail=40 --prefix || true

# The real assertion. verify-node-install.sh reads the live kubelet arguments
# on every node and exits nonzero if any node is not set up, which is the
# failure this whole component exists to avoid.
log "verifying the nodes"
NAMESPACE="${E2E_NAMESPACE}" "${REPO_ROOT}/scripts/verify-node-install.sh"

# The RBAC that lets kubelet ask for a token with the registry audience. The
# chart creates it; without it the pull fails later with no clue why.
log "checking the node audience RBAC"
kubectl auth can-i request-serviceaccounts-token-audience "${E2E_REGISTRY_HOST}" \
  --as-group=system:nodes --as="system:node:probe" >/dev/null \
  || fail "kubelets cannot request a token for ${E2E_REGISTRY_HOST}"
ok "node audience RBAC grants ${E2E_REGISTRY_HOST}"

ok "profile=${PROFILE} installed and verified"
