#!/usr/bin/env bash
#
# Shared helpers for the end-to-end install tests. Sourced, not run.
#
# shellcheck disable=SC2034  # the variables here are for the scripts that source this

set -euo pipefail

E2E_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd -- "${E2E_ROOT}/../.." && pwd)"

# The image the DaemonSet runs. Built from this checkout and loaded into the
# cluster, so the test covers the code in the branch rather than a release.
E2E_IMAGE_REPO="${E2E_IMAGE_REPO:-localhost/credential-provider-harbor-deployer}"
E2E_IMAGE_TAG="${E2E_IMAGE_TAG:-e2e}"
E2E_IMAGE="${E2E_IMAGE_REPO}:${E2E_IMAGE_TAG}"

# Harbor is not part of this test. The install writes a provider config that
# names this host; whether a pull against it succeeds is the manual test.
E2E_REGISTRY_HOST="${E2E_REGISTRY_HOST:-harbor.example.com}"

# Every cluster script writes its kubeconfig here, so a run never edits the
# machine's own ~/.kube/config or leaves a context behind after teardown.
E2E_KUBECONFIG="${E2E_KUBECONFIG:-${TMPDIR:-/tmp}/cph-e2e-kubeconfig}"
export KUBECONFIG="${E2E_KUBECONFIG}"

E2E_NAMESPACE="${E2E_NAMESPACE:-kube-system}"
E2E_RELEASE="${E2E_RELEASE:-credential-provider-harbor}"
E2E_CLUSTER_NAME="${E2E_CLUSTER_NAME:-cph-e2e}"

# pinned is the lowest Kubernetes this component supports, latest is whatever
# the distro tool installs by default. See versions.env.
E2E_K8S_CHANNEL="${E2E_K8S_CHANNEL:-latest}"

log()  { printf '\033[36m==>\033[0m %s\n' "$*"; }
ok()   { printf '\033[32m ok\033[0m %s\n' "$*"; }
fail() { printf '\033[31mFAIL\033[0m %s\n' "$*" >&2; return 1; }

need() {
  local c
  for c in "$@"; do
    command -v "${c}" >/dev/null 2>&1 || { echo "missing required command: ${c}" >&2; exit 1; }
  done
}

# Runs a command until it succeeds or the deadline passes. Used for the waits
# that have no kubectl equivalent, such as an API server that is still coming up.
retry() {
  local tries=$1 delay=$2; shift 2
  local n=1
  until "$@"; do
    if [ "${n}" -ge "${tries}" ]; then
      fail "gave up after ${tries} attempts: $*" || return 1
    fi
    n=$((n + 1))
    sleep "${delay}"
  done
}

# Waits for the cluster to have a Ready node. The registration wait comes first
# because a distro can serve a kubeconfig before its node exists, and
# `kubectl wait --all` against nothing matches nothing and returns at once.
wait_for_nodes() {
  local timeout="${1:-5m}"
  retry 60 5 node_registered
  kubectl wait --for=condition=Ready nodes --all --timeout="${timeout}"
}

node_registered() {
  [ -n "$(kubectl get nodes -o name 2>/dev/null)" ]
}

# k3s, RKE2, MicroK8s and kubeadm install onto the machine that runs them and
# restart its kubelet. Only a throwaway VM should agree to that.
host_install_guard() {
  [ -n "${E2E_ALLOW_HOST_INSTALL:-}" ] || [ "${CI:-}" = true ] || {
    echo "refusing to install $1 on this machine." >&2
    echo "Set E2E_ALLOW_HOST_INSTALL=1 if it is disposable." >&2
    exit 1
  }
}

# Hands a locally built image to a containerd the distro owns. Piped rather
# than written to a file, because the node runtime and this shell do not share
# a writable directory on every distro.
import_to_containerd() {
  docker save "${E2E_IMAGE}" | sudo "$@"
}

# What a failed run has to show for itself. Printed from the trap rather than
# from a CI step, so a run on a laptop says as much as a run in Actions.
dump_state() {
  log "cluster state"
  kubectl get nodes -o wide || true
  kubectl get pods -A -o wide || true
  kubectl describe "daemonset/${E2E_RELEASE}" -n "${E2E_NAMESPACE}" || true
  # Whatever stopped a pod short of Running is in its events, not the
  # DaemonSet's. A sandbox that never came up says so only here. Listed first
  # and described one at a time, because describe takes no field selector.
  kubectl get pods -A --no-headers \
    --field-selector=status.phase!=Running,status.phase!=Succeeded \
    -o 'custom-columns=NS:.metadata.namespace,NAME:.metadata.name' 2>/dev/null \
    | while read -r ns name; do
        kubectl describe pod "${name}" -n "${ns}" || true
      done
  kubectl logs -n "${E2E_NAMESPACE}" \
    -l app.kubernetes.io/name=credential-provider-harbor --tail=200 --prefix || true
}

# Two things a cluster script may override. install-args prints extra helm
# --set flags the distro needs; restart-nodes brings the node back when the
# installer could not do it itself.
install-args() { :; }
restart-nodes() { :; }
