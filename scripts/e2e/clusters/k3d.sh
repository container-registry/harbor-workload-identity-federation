#!/usr/bin/env bash
#
# k3d cluster for the e2e install test: k3s in a container, so the installer
# writes to /etc/rancher/k3s/config.yaml.d inside the node container.
#
# Usage: k3d.sh up|down|load|profile|install-args|restart-nodes

set -euo pipefail

# shellcheck source=scripts/e2e/lib/common.sh
source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)/lib/common.sh"
# shellcheck source=scripts/e2e/versions.env
source "${E2E_ROOT}/versions.env"

profile() { echo k3d; }

# A k3d node runs k3s as its own PID 1 with no init system behind it, so the
# installer has nothing to ask for a restart and its nsenter systemctl call
# fails with exit 127. Write the files and restart the node from out here
# instead. See issue #32.
install-args() { echo "--set kubelet.restart=false"; }

restart-nodes() {
  log "restarting the cluster so k3s rereads its config"
  k3d cluster stop "${E2E_CLUSTER_NAME}"
  k3d cluster start "${E2E_CLUSTER_NAME}" --wait
  kubectl wait --for=condition=Ready nodes --all --timeout=5m
}

up() {
  need k3d kubectl
  local args=(--wait --timeout 5m --agents 1)
  [ "${E2E_K8S_CHANNEL}" = pinned ] && args+=(--image "${E2E_PINNED_K3S_IMAGE}")
  log "creating k3d cluster ${E2E_CLUSTER_NAME} (${E2E_K8S_CHANNEL})"
  k3d cluster create "${E2E_CLUSTER_NAME}" "${args[@]}"
  kubectl config use-context "k3d-${E2E_CLUSTER_NAME}"
  kubectl wait --for=condition=Ready nodes --all --timeout=5m
}

load() {
  log "importing ${E2E_IMAGE}"
  k3d image import "${E2E_IMAGE}" --cluster "${E2E_CLUSTER_NAME}"
}

down() {
  k3d cluster delete "${E2E_CLUSTER_NAME}" || true
}

"${1:?up, down, load, profile, install-args or restart-nodes}"
