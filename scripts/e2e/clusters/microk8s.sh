#!/usr/bin/env bash
#
# MicroK8s on this machine for the e2e install test. kubelet runs inside
# kubelite under snap confinement, so this covers the snap arguments file and
# the writable paths the microk8s profile has to use.
#
# Usage: microk8s.sh up|down|load|profile|install-args|restart-nodes|dump-node

set -euo pipefail

# shellcheck source=scripts/e2e/lib/common.sh
source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)/lib/common.sh"
# shellcheck source=scripts/e2e/versions.env
source "${E2E_ROOT}/versions.env"

profile() { echo microk8s; }

up() {
  need snap kubectl sudo
  host_install_guard microk8s

  local args=(microk8s --classic)
  [ "${E2E_K8S_CHANNEL}" = pinned ] && args+=(--channel "${E2E_PINNED_MICROK8S_CHANNEL}")

  log "installing microk8s (${E2E_K8S_CHANNEL})"
  sudo snap install "${args[@]}"
  sudo microk8s status --wait-ready --timeout 300

  # shellcheck disable=SC2024  # the redirect is meant to run as us: the
  # kubeconfig has to end up owned by whoever runs the test, not by root.
  sudo microk8s config > "${KUBECONFIG}"
  chmod 600 "${KUBECONFIG}"
  wait_for_nodes 5m
}

load() {
  log "importing ${E2E_IMAGE}"
  # The snap reads its own tree, so the tarball goes there rather than through
  # a pipe or /tmp.
  local tar=/var/snap/microk8s/common/cph-e2e-image.tar
  docker save "${E2E_IMAGE}" | sudo tee "${tar}" >/dev/null
  sudo microk8s ctr images import "${tar}"
  sudo rm -f "${tar}"
}

down() {
  command -v microk8s >/dev/null 2>&1 || return 0
  sudo snap remove microk8s --purge || true
}

"${1:?up, down, load, profile, install-args, restart-nodes or dump-node}"
