#!/usr/bin/env bash
#
# RKE2 on this machine for the e2e install test. RKE2 runs kubelet inside the
# supervisor, so this is the only way to exercise detectRKE2Services and the
# config.yaml.d drop-in against a real one.
#
# Usage: rke2.sh up|down|load|profile|install-args|restart-nodes

set -euo pipefail

# shellcheck source=scripts/e2e/lib/common.sh
source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)/lib/common.sh"
# shellcheck source=scripts/e2e/versions.env
source "${E2E_ROOT}/versions.env"

profile() { echo rke2; }

up() {
  need curl kubectl sudo
  host_install_guard rke2

  local version=""
  [ "${E2E_K8S_CHANNEL}" = pinned ] && version="${E2E_PINNED_RKE2_VERSION}"

  log "installing rke2 (${E2E_K8S_CHANNEL})"
  curl -sfL https://get.rke2.io | sudo INSTALL_RKE2_VERSION="${version}" sh -

  # config.yaml, not a drop-in: the installer owns config.yaml.d/99-*, and a
  # base file here is also what a node with its own settings looks like.
  sudo mkdir -p /etc/rancher/rke2
  printf 'write-kubeconfig-mode: "0644"\n' | sudo tee /etc/rancher/rke2/config.yaml >/dev/null

  sudo systemctl enable --now rke2-server.service
  retry 60 10 test -r /etc/rancher/rke2/rke2.yaml
  cp /etc/rancher/rke2/rke2.yaml "${KUBECONFIG}"
  chmod 600 "${KUBECONFIG}"
  kubectl wait --for=condition=Ready nodes --all --timeout=10m
}

load() {
  log "importing ${E2E_IMAGE}"
  # RKE2 ships its own containerd on a socket of its own, and its ctr is not
  # on PATH.
  import_to_containerd /var/lib/rancher/rke2/bin/ctr \
    --address /run/k3s/containerd/containerd.sock --namespace k8s.io images import -
}

down() {
  [ -x /usr/local/bin/rke2-uninstall.sh ] || return 0
  sudo /usr/local/bin/rke2-uninstall.sh || true
}

"${1:?up, down, load, profile, install-args or restart-nodes}"
