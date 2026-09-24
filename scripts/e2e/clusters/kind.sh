#!/usr/bin/env bash
#
# kind cluster for the e2e install test: nodes are containers running systemd,
# so the kind profile's systemd drop-in is what gets exercised.
#
# Usage: kind.sh up|down|load|profile|install-args|restart-nodes

set -euo pipefail

# shellcheck source=scripts/e2e/lib/common.sh
source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)/lib/common.sh"
# shellcheck source=scripts/e2e/versions.env
source "${E2E_ROOT}/versions.env"

profile() { echo kind; }

# kind's kubelet unit does not expand $KUBELET_EXTRA_ARGS, so the drop-in the
# profile writes by default lands on disk and changes nothing. The README
# calls this out as a wrinkle to check for; on current node images it is
# every time. See issue #33.
install-args() { echo "--set kubelet.forceExecStartOverride=true"; }

up() {
  need kind kubectl
  local args=(--name "${E2E_CLUSTER_NAME}" --wait 5m)
  [ "${E2E_K8S_CHANNEL}" = pinned ] && args+=(--image "${E2E_PINNED_KIND_NODE_IMAGE}")
  log "creating kind cluster ${E2E_CLUSTER_NAME} (${E2E_K8S_CHANNEL})"
  kind create cluster "${args[@]}"
  kubectl config use-context "kind-${E2E_CLUSTER_NAME}"
}

load() {
  log "loading ${E2E_IMAGE}"
  kind load docker-image "${E2E_IMAGE}" --name "${E2E_CLUSTER_NAME}"
}

down() {
  kind delete cluster --name "${E2E_CLUSTER_NAME}" || true
}

"${1:?up, down, load, profile, install-args or restart-nodes}"
