#!/usr/bin/env bash
#
# k3s on this machine for the e2e install test. A real k3s server, so the
# installer writes /etc/rancher/k3s/config.yaml.d and restarts k3s.service
# rather than dropping a systemd unit file in.
#
# Usage: k3s.sh up|down|load|profile|install-args|restart-nodes

set -euo pipefail

# shellcheck source=scripts/e2e/lib/common.sh
source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)/lib/common.sh"
# shellcheck source=scripts/e2e/versions.env
source "${E2E_ROOT}/versions.env"

profile() { echo k3s; }

up() {
  need curl kubectl sudo
  host_install_guard k3s

  local version=""
  [ "${E2E_K8S_CHANNEL}" = pinned ] && version="${E2E_PINNED_K3S_VERSION}"

  log "installing k3s (${E2E_K8S_CHANNEL})"
  # An empty INSTALL_K3S_VERSION is what the install script reads as "latest".
  curl -sfL https://get.k3s.io \
    | sudo INSTALL_K3S_VERSION="${version}" sh -s - --write-kubeconfig-mode 644

  retry 30 5 test -r /etc/rancher/k3s/k3s.yaml
  cp /etc/rancher/k3s/k3s.yaml "${KUBECONFIG}"
  chmod 600 "${KUBECONFIG}"
  kubectl wait --for=condition=Ready nodes --all --timeout=5m
}

load() {
  log "importing ${E2E_IMAGE}"
  import_to_containerd k3s ctr images import -
}

down() {
  [ -x /usr/local/bin/k3s-uninstall.sh ] || return 0
  sudo /usr/local/bin/k3s-uninstall.sh || true
}

"${1:?up, down, load, profile, install-args or restart-nodes}"
