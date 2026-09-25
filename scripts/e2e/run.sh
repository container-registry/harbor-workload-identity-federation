#!/usr/bin/env bash
#
# One end-to-end install test: stand a cluster up, put this checkout's image
# in it, install the chart, check the nodes, tear the cluster down.
#
# Usage: run.sh <distro> [pinned|latest]
#
#   scripts/e2e/run.sh k3d pinned
#   E2E_KEEP=1 scripts/e2e/run.sh kind     # leave the cluster up to poke at
#
# Distros are the files in clusters/. The cloud ones are not here: EKS, GKE,
# AKS and OpenShift need an account, so they are tested by hand against a
# release. See README.md.

set -euo pipefail

# shellcheck source=scripts/e2e/lib/common.sh
source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/lib/common.sh"

DISTRO=${1:?distro, one of $(cd "${E2E_ROOT}/clusters" && printf '%s ' *.sh)}
export E2E_K8S_CHANNEL="${2:-${E2E_K8S_CHANNEL}}"

CLUSTER="${E2E_ROOT}/clusters/${DISTRO}.sh"
[ -x "${CLUSTER}" ] || { echo "no cluster script for ${DISTRO}: ${CLUSTER}" >&2; exit 1; }

case "${E2E_K8S_CHANNEL}" in
  pinned|latest) ;;
  *) echo "channel must be pinned or latest, got ${E2E_K8S_CHANNEL}" >&2; exit 1 ;;
esac

need docker

teardown() {
  local rc=$?
  if [ -n "${E2E_KEEP:-}" ]; then
    log "E2E_KEEP set, leaving the cluster up"
  else
    log "tearing down"
    "${CLUSTER}" down || true
  fi
  return "${rc}"
}

log "building ${E2E_IMAGE}"
# Retried because the base image comes from Docker Hub, which resets the
# connection often enough to lose a distro's whole run to something that has
# nothing to do with the chart.
retry 3 15 docker build --build-arg VERSION="e2e" -t "${E2E_IMAGE}" "${REPO_ROOT}"

# Armed before the cluster exists: a distro that fails halfway through its
# install has still left something behind to clean up.
trap teardown EXIT
"${CLUSTER}" up

"${CLUSTER}" load

# Split rather than quoted: the driver prints whole --set flags, and a failure
# here has to stop the run rather than install with half of them.
INSTALL_ARGS="$("${CLUSTER}" install-args)"
# shellcheck disable=SC2206  # deliberate word splitting over those flags
EXTRA_ARGS=(${INSTALL_ARGS})

E2E_CLUSTER_SCRIPT="${CLUSTER}" \
  "${E2E_ROOT}/install-test.sh" "$("${CLUSTER}" profile)" "${EXTRA_ARGS[@]}"

ok "${DISTRO} (${E2E_K8S_CHANNEL}) passed"
