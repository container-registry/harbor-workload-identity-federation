#!/usr/bin/env bash
#
# A single-node kubeadm cluster on this machine. It is the only distro here
# that runs kubelet as its own systemd unit on a normal filesystem, which is
# what the generic profile is written for.
#
# Usage: kubeadm.sh up|down|load|profile|install-args|restart-nodes

set -euo pipefail

# shellcheck source=scripts/e2e/lib/common.sh
source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)/lib/common.sh"
# shellcheck source=scripts/e2e/versions.env
source "${E2E_ROOT}/versions.env"

FLANNEL_MANIFEST="https://github.com/flannel-io/flannel/releases/latest/download/kube-flannel.yml"
POD_CIDR="10.244.0.0/16"

profile() { echo generic; }

# The package repository is per minor version, so latest has to be resolved to
# one before anything can be installed.
k8s_minor() {
  if [ "${E2E_K8S_CHANNEL}" = pinned ]; then
    echo "${E2E_PINNED_K8S_MINOR}"
    return
  fi
  curl -sfL https://dl.k8s.io/release/stable.txt | cut -d. -f1,2
}

up() {
  need curl sudo
  host_install_guard kubeadm

  local minor
  minor="$(k8s_minor)"
  [ -n "${minor}" ] || fail "could not work out which Kubernetes minor to install"

  log "installing kubeadm ${minor} (${E2E_K8S_CHANNEL})"
  sudo apt-get update -qq
  # Not containerd: the machine already has one, and the distro package
  # conflicts with it. Replacing it would take Docker with it, and the image
  # under test is in Docker.
  sudo apt-get install -y -qq apt-transport-https ca-certificates curl gpg

  sudo mkdir -p /etc/apt/keyrings
  curl -fsSL "https://pkgs.k8s.io/core:/stable:/${minor}/deb/Release.key" \
    | sudo gpg --dearmor --yes -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg
  echo "deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/${minor}/deb/ /" \
    | sudo tee /etc/apt/sources.list.d/kubernetes.list >/dev/null
  sudo apt-get update -qq
  sudo apt-get install -y -qq kubelet kubeadm kubectl

  # kubelet and containerd have to agree on the cgroup driver, and the shipped
  # containerd config says cgroupfs while kubeadm defaults to systemd.
  sudo mkdir -p /etc/containerd
  sudo containerd config default | sudo tee /etc/containerd/config.toml >/dev/null
  sudo sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml
  sudo systemctl restart containerd
  # Docker talks to that containerd, so it has to come back after it.
  sudo systemctl restart docker

  sudo swapoff -a
  sudo modprobe br_netfilter
  printf 'net.bridge.bridge-nf-call-iptables=1\nnet.ipv4.ip_forward=1\n' \
    | sudo tee /etc/sysctl.d/99-cph-e2e.conf >/dev/null
  sudo sysctl --system >/dev/null

  sudo kubeadm init --pod-network-cidr "${POD_CIDR}"
  sudo cp /etc/kubernetes/admin.conf "${KUBECONFIG}"
  sudo chown "$(id -u):$(id -g)" "${KUBECONFIG}"
  chmod 600 "${KUBECONFIG}"

  # One node, so it has to take the workload the control plane is tainted
  # against.
  kubectl taint nodes --all node-role.kubernetes.io/control-plane- || true
  kubectl apply -f "${FLANNEL_MANIFEST}"
  kubectl wait --for=condition=Ready nodes --all --timeout=10m
}

load() {
  log "importing ${E2E_IMAGE}"
  import_to_containerd ctr --namespace k8s.io images import -
}

down() {
  command -v kubeadm >/dev/null 2>&1 || return 0
  sudo kubeadm reset -f || true
  sudo rm -rf /etc/cni/net.d
}

"${1:?up, down, load, profile, install-args or restart-nodes}"
