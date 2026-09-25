#!/usr/bin/env bash
#
# A single-node kubeadm cluster on this machine. It is the only distro here
# that runs kubelet as its own systemd unit on a normal filesystem, which is
# what the generic profile is written for.
#
# Usage: kubeadm.sh up|down|load|profile|install-args|restart-nodes|dump-node

set -euo pipefail

# shellcheck source=scripts/e2e/lib/common.sh
source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)/lib/common.sh"
# shellcheck source=scripts/e2e/versions.env
source "${E2E_ROOT}/versions.env"

FLANNEL_MANIFEST="https://github.com/flannel-io/flannel/releases/latest/download/kube-flannel.yml"
POD_CIDR="10.244.0.0/16"

# Set by up() once the package version is known, and used for kubeadm init.
K8S_VERSION=""

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

# Flannel installs its own plugin and nothing else, and its conflist delegates
# to bridge and host-local and then chains portmap. Without those three, every
# pod off the host network stops at sandbox creation.
install_cni_plugins() {
  local url tgz
  url="https://github.com/containernetworking/plugins/releases/download/${E2E_CNI_PLUGINS_VERSION}/cni-plugins-linux-amd64-${E2E_CNI_PLUGINS_VERSION}.tgz"
  tgz="$(mktemp)"

  log "installing CNI plugins ${E2E_CNI_PLUGINS_VERSION}"
  curl -sfL "${url}" -o "${tgz}"
  curl -sfL "${url}.sha256" | sed "s|cni-plugins-linux-amd64-${E2E_CNI_PLUGINS_VERSION}.tgz|${tgz}|" \
    | sha256sum -c -
  sudo mkdir -p /opt/cni/bin
  sudo tar -C /opt/cni/bin -xzf "${tgz}"
  rm -f "${tgz}"
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

  # One exact version, taken from the repository that was just added. Asking
  # apt for "kubelet kubeadm kubectl" gets the highest version any configured
  # source offers, and this machine offers its own: the pinned channel logged
  # v1.34 and stood up a v1.37 node.
  local pkg
  pkg="$(apt-cache madison kubeadm | awk -v repo="stable:/${minor}/deb" '$0 ~ repo {print $3; exit}')"
  [ -n "${pkg}" ] || fail "no kubeadm package for ${minor} in the repository just added"

  log "installing kubelet, kubeadm and kubectl ${pkg}"
  # --allow-downgrades because the pinned channel is a downgrade here: the
  # runner ships a newer kubectl than the minor under test, which is the same
  # thing that let apt pick its own version before.
  sudo apt-get install -y -qq --allow-downgrades \
    "kubelet=${pkg}" "kubeadm=${pkg}" "kubectl=${pkg}"
  # Nothing here upgrades them, but an unattended upgrade mid-run would swap
  # the kubelet under the node the test is about to inspect.
  sudo apt-mark hold kubelet kubeadm kubectl >/dev/null

  K8S_VERSION="v${pkg%%-*}"
  local got
  got="$(kubeadm version -o short)"
  [ "${got}" = "${K8S_VERSION}" ] \
    || fail "asked for kubeadm ${K8S_VERSION} and got ${got}"

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

  install_cni_plugins

  # Named rather than left to kubeadm's default, so the control plane matches
  # the kubelet that was installed instead of whatever is newest.
  sudo kubeadm init --kubernetes-version "${K8S_VERSION}" --pod-network-cidr "${POD_CIDR}"
  sudo cp /etc/kubernetes/admin.conf "${KUBECONFIG}"
  sudo chown "$(id -u):$(id -g)" "${KUBECONFIG}"
  chmod 600 "${KUBECONFIG}"

  # One node, so it has to take the workload the control plane is tainted
  # against.
  kubectl taint nodes --all node-role.kubernetes.io/control-plane- || true
  kubectl apply -f "${FLANNEL_MANIFEST}"
  wait_for_nodes 10m
}

# The node is this machine, so the files the installer wrote and the command
# line kubelet actually ended up with can be read directly. Printed on failure
# because "kubelet is running WITHOUT the credential provider flags" says
# nothing about which of those two went wrong.
dump-node() {
  log "kubelet drop-in"
  sudo cat /etc/systemd/system/kubelet.service.d/99-credential-provider-harbor.conf 2>&1 || true
  log "kubelet unit as systemd merged it"
  sudo systemctl cat kubelet 2>&1 || true
  log "kubelet command line"
  local pid
  pid="$(pgrep -x kubelet | head -1)" || true
  if [ -n "${pid}" ]; then
    sudo cat "/proc/${pid}/cmdline" | tr '\0' ' '
    echo
  else
    echo "no kubelet process"
  fi
  log "kubelet environment files"
  sudo cat /etc/default/kubelet /var/lib/kubelet/kubeadm-flags.env 2>&1 || true
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

"${1:?up, down, load, profile, install-args, restart-nodes or dump-node}"
