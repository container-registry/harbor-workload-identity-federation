#!/usr/bin/env bash
#
# Check whether nodes are actually set up to pull from Harbor with
# credential-provider-harbor.
#
# The common failure is not that the install failed. It is that the binary and
# the config are in place, and kubelet was never told about them, so pulls keep
# failing with "no basic auth credentials". This checks the live kubelet
# arguments rather than the files, because that is the thing that decides.
#
# Usage:
#   scripts/verify-node-install.sh            # every node
#   scripts/verify-node-install.sh node-1     # one node
#
# Needs: kubectl with permission to create debug pods on the nodes.
#
# Environment:
#   NAMESPACE      namespace of the chart release (default kube-system)
#   DEBUG_IMAGE    image for the debug pod (default busybox:1.36)
#   BINARY_NAME    credentialProvider.binaryName, if you changed it
#                  (default credential-provider-harbor)

set -euo pipefail

NAMESPACE="${NAMESPACE:-kube-system}"
DEBUG_IMAGE="${DEBUG_IMAGE:-busybox:1.36}"
BINARY_NAME="${BINARY_NAME:-credential-provider-harbor}"

red() { printf '\033[31m%s\033[0m\n' "$1"; }
green() { printf '\033[32m%s\033[0m\n' "$1"; }
yellow() { printf '\033[33m%s\033[0m\n' "$1"; }

# kubectl debug names its pod node-debugger-<node>-<suffix> and leaves it
# behind when it exits, so a few runs across a few nodes litter the namespace
# with completed privileged pods. Clean up ours after each call.
cleanup_debug_pods() {
  local node=$1 pods
  pods=$(kubectl get pods -n "${NAMESPACE}" -o name 2>/dev/null \
    | grep "^pod/node-debugger-${node}-" || true)
  [ -n "${pods}" ] || return 0
  # shellcheck disable=SC2086  # deliberate word splitting over the pod list
  kubectl delete -n "${NAMESPACE}" ${pods} --wait=false >/dev/null 2>&1 || true
}

# Runs a shell snippet on a node through a debug pod, chrooted into the host.
on_node() {
  local node=$1 script=$2 rc=0 out
  out=$(kubectl debug "node/${node}" \
    --image="${DEBUG_IMAGE}" \
    --profile=sysadmin \
    --namespace="${NAMESPACE}" \
    -q --attach=true \
    -- chroot /host /bin/sh -c "${script}" 2>/dev/null) || rc=$?
  cleanup_debug_pods "${node}"
  printf '%s' "${out}"
  return "${rc}"
}

# kubelet is a separate process on most distributions, and embedded in the
# rke2 and k3s supervisors or in microk8s kubelite on others. Matching on the
# literal string "kubelet" with pgrep would also match this very snippet, whose
# own command line contains it, so the shell's own PID is excluded explicitly.
# shellcheck disable=SC2016  # this expands on the node, not here.
KUBELET_CMDLINE_SNIPPET='
self=$$
for pid in $(ls /proc 2>/dev/null | grep "^[0-9]*$"); do
  [ "$pid" = "$self" ] && continue
  [ -r "/proc/$pid/cmdline" ] || continue
  cmd=$(tr "\0" " " < "/proc/$pid/cmdline")
  case "$cmd" in
    */bin/sh\ -c*|*chroot\ /host*) continue ;;
  esac
  case "$cmd" in
    */kubelet\ *|*/kubelet|*/kubelite\ *|*/rke2\ server*|*/rke2\ agent*|*/k3s\ server*|*/k3s\ agent*)
      echo "$cmd"
      exit 0
      ;;
  esac
done
echo NO_KUBELET_PROCESS
'

# k3s and RKE2 keep the settings here rather than on a command line.
# shellcheck disable=SC2016  # this expands on the node, not here.
CONFIG_DROPIN_SNIPPET='
for f in /etc/rancher/k3s/config.yaml /etc/rancher/k3s/config.yaml.d/*.yaml \
         /etc/rancher/rke2/config.yaml /etc/rancher/rke2/config.yaml.d/*.yaml; do
  [ -f "$f" ] || continue
  grep -h "image-credential-provider" "$f" 2>/dev/null
done
'

report() {
  local ok=$1 good=$2 bad=$3
  if [ "${ok}" = "yes" ]; then green "  ${good}"; return 0; fi
  red "  ${bad}"; return 1
}

check_node() {
  local node=$1 failed=0 cmdline files ok

  echo "=== ${node} ==="

  if ! cmdline=$(on_node "${node}" "${KUBELET_CMDLINE_SNIPPET}"); then
    red "  could not reach the node (is kubectl debug allowed here?)"
    return 1
  fi

  case "${cmdline}" in
    *NO_KUBELET_PROCESS*|'')
      red "  no kubelet process found"
      echo "     Looked for kubelet, kubelite, and the rke2 and k3s supervisors."
      return 1
      ;;
  esac

  local bindir configpath
  bindir=$(printf '%s' "${cmdline}" | grep -o '\-\-image-credential-provider-bin-dir=[^ ]*' | cut -d= -f2- || true)
  configpath=$(printf '%s' "${cmdline}" | grep -o '\-\-image-credential-provider-config=[^ ]*' | cut -d= -f2- || true)

  local source="the kubelet command line"
  if [ -z "${bindir}" ] || [ -z "${configpath}" ]; then
    # k3s and RKE2 pass these to their embedded kubelet from a config file, so
    # they never show up in /proc. Absent from the command line is not yet a
    # failure on those.
    local fromconfig
    fromconfig=$(on_node "${node}" "${CONFIG_DROPIN_SNIPPET}") || fromconfig=""
    bindir=$(printf '%s' "${fromconfig}" | grep -o 'image-credential-provider-bin-dir[=:][^\"]*' | sed 's/.*[=:][[:space:]]*//;s/\"//g' | head -1 || true)
    configpath=$(printf '%s' "${fromconfig}" | grep -o 'image-credential-provider-config[=:][^\"]*' | sed 's/.*[=:][[:space:]]*//;s/\"//g' | head -1 || true)
    source="a k3s or RKE2 config drop-in"
  fi

  if [ -z "${bindir}" ] || [ -z "${configpath}" ]; then
    red "  kubelet is running WITHOUT the credential provider flags"
    echo "     This is the usual cause of 'no basic auth credentials'."
    echo "     The files may be installed; kubelet was never pointed at them."
    echo "     See examples/kubernetes/<your distribution>/README.md."
    return 1
  fi

  green "  kubelet flags present, from ${source}"
  echo "     bin-dir: ${bindir}"
  echo "     config:  ${configpath}"

  # No `|| true` here: an unreachable node must not read as three green checks.
  if ! files=$(on_node "${node}" "
    if [ -x '${bindir}/${BINARY_NAME}' ]; then echo BINARY_OK; else echo BINARY_MISSING; fi
    if [ -f '${configpath}' ]; then echo CONFIG_OK; else echo CONFIG_MISSING; fi
    if grep -q credential-provider-harbor '${configpath}' 2>/dev/null; then echo ENTRY_OK; else echo ENTRY_MISSING; fi
  "); then
    red "  could not read the node filesystem"
    return 1
  fi

  case "${files}" in *BINARY_OK*) ok=yes ;; *) ok=no ;; esac
  report "${ok}" "binary present" "binary missing at ${bindir}/${BINARY_NAME}" || failed=1
  case "${files}" in *CONFIG_OK*) ok=yes ;; *) ok=no ;; esac
  report "${ok}" "config present" "config missing at ${configpath}" || failed=1
  case "${files}" in *ENTRY_OK*) ok=yes ;; *) ok=no ;; esac
  report "${ok}" "provider entry present" "config has no credential-provider-harbor entry" || failed=1

  return "${failed}"
}

main() {
  local rc=0 node nodes=()

  if [ $# -gt 0 ]; then
    nodes=("$@")
  else
    local listed
    if ! listed=$(kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}'); then
      red "could not list nodes"
      return 1
    fi
    while IFS= read -r node; do
      [ -n "${node}" ] && nodes+=("${node}")
    done <<<"${listed}"
  fi

  if [ "${#nodes[@]}" -eq 0 ]; then
    red "no nodes to check"
    return 1
  fi

  for node in "${nodes[@]}"; do
    check_node "${node}" || rc=1
    echo
  done

  if [ "${rc}" -ne 0 ]; then
    yellow "Some nodes are not ready to pull from Harbor. See the output above."
    echo "Installer logs:"
    echo "  kubectl logs -n ${NAMESPACE} -l app.kubernetes.io/name=credential-provider-harbor"
  else
    green "All ${#nodes[@]} node(s) checked are set up."
  fi
  return "${rc}"
}

main "$@"
