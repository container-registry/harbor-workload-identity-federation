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

# The debug pods sitting on a node, oldest first. kubectl debug names its pod
# node-debugger-<node>-<suffix> and leaves it behind when it exits, so these
# accumulate across calls.
#
# -F, not a pattern: a node name like "worker.example.com" read as a regex
# would let its dots match any character and pick up another node's debug
# pods. A pod name cannot contain "/", so the fixed string "pod/node-..." can
# only ever match at the start of a line from `kubectl get -o name`.
debug_pods_for() {
  local node=$1
  kubectl get pods -n "${NAMESPACE}" -o name \
    --sort-by=.metadata.creationTimestamp 2>/dev/null \
    | grep -F -e "pod/node-debugger-${node}-" || true
}

# A few runs across a few nodes would otherwise litter the namespace with
# completed privileged pods. Clean up ours after each call.
cleanup_debug_pods() {
  local node=$1 pods
  pods=$(debug_pods_for "${node}")
  [ -n "${pods}" ] || return 0
  # shellcheck disable=SC2086  # deliberate word splitting over the pod list
  kubectl delete -n "${NAMESPACE}" ${pods} --wait=false >/dev/null 2>&1 || true
}

# The log of the debug pod this attempt created, once it has finished. The
# pods that were already there are excluded by name: cleanup does not wait for
# a delete, so an earlier attempt's pod can still be listed, and reading it
# would answer this attempt with a previous one's view of the node.
debug_pod_log() {
  local node=$1 already=$2 pod
  pod=$(debug_pods_for "${node}" \
    | grep -F -x -v -f <(printf '%s\n' "${already}") | tail -1) || true
  [ -n "${pod}" ] || return 0
  kubectl wait --for=jsonpath='{.status.phase}'=Succeeded "${pod}" \
    -n "${NAMESPACE}" --timeout=60s >/dev/null 2>&1 || true
  kubectl logs -n "${NAMESPACE}" "${pod}" 2>/dev/null || true
}

# Runs a shell snippet on a node through a debug pod, chrooted into the host.
#
# Every snippet below prints something on every path, so empty output means the
# attach never saw the container rather than that the node answered "nothing".
# Two things cause that. The attach races the container, which for a program
# that prints and exits it can lose outright, so the pod's log is asked for
# whatever the attach came back with. And a node that has just restarted its
# kubelet cannot start the pod at all, which is exactly when this gets run, so
# the whole thing is retried. Without either, a healthy node reads as having no
# kubelet.
on_node() {
  local node=$1 script=$2 rc out attempt already
  shift 2
  for attempt in 1 2 3; do
    rc=0
    already=$(debug_pods_for "${node}")
    out=$(kubectl debug "node/${node}" \
      --image="${DEBUG_IMAGE}" \
      --profile=sysadmin \
      --namespace="${NAMESPACE}" \
      -q --attach=true \
      -- chroot /host /bin/sh -c "${script}" verify-node-install "$@" 2>/dev/null) || rc=$?
    if [ -z "${out}" ]; then
      out=$(debug_pod_log "${node}" "${already}")
      # The node answered after all, so the attach's exit status was about the
      # attach and not about the node.
      [ -z "${out}" ] || rc=0
    fi
    cleanup_debug_pods "${node}"
    # Empty is the only retryable answer, and it is retryable whatever the
    # exit status: a kubelet that is still restarting fails both routes.
    [ -z "${out}" ] || break
    [ "${attempt}" -eq 3 ] || sleep 5
  done
  printf '%s' "${out}"
  return "${rc}"
}

# The node-side programs live in scripts/lib/, one file each: a program that
# is read, checked (shellcheck, sh -n) and reviewed on its own rather than as
# a quoted string in the middle of this script. They are read here and handed
# to the node through the debug pod.
LIB_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/lib"

node_program() {
  local path="${LIB_DIR}/$1"
  if [ ! -r "${path}" ]; then
    # Both lines go to stderr: this function is read through a command
    # substitution, which would otherwise swallow the explanation and leave
    # the run ending on nothing.
    red "cannot read ${path}" >&2
    echo "  Run this from a checkout: the node-side programs live in scripts/lib/." >&2
    exit 1
  fi
  cat "${path}"
}

# The words the node prints to say "nothing is running kubelet here" and "the
# scan read every file it knows about". Both are defined here and passed to
# the node, so the printing end and the reading end cannot drift apart.
NO_KUBELET_MARKER="NO_KUBELET_PROCESS"
CONFIG_SCAN_DONE_MARKER="CONFIG_SCAN_DONE"

KUBELET_CMDLINE_PROGRAM="$(node_program node-kubelet-cmdline.sh)"
CONFIG_SCAN_PROGRAM="$(node_program node-kubelet-config-scan.sh)"

# Runs the config scan on a node and prints what it found, sentinel removed.
# k3s, RKE2 and MicroK8s keep these settings in a file rather than on a
# command line, and every spelling of a setting comes back from the scan as
# one quoted "key=value" line. Fails when the scan did not run to completion,
# which is the only way to tell a node whose files hold nothing from a debug
# pod that never attached.
scan_node_kubelet_config() {
  local node=$1 out
  out=$(on_node "${node}" "${CONFIG_SCAN_PROGRAM}" "${CONFIG_SCAN_DONE_MARKER}") || out=""
  case "${out}" in
    *"${CONFIG_SCAN_DONE_MARKER}"*) ;;
    *) return 1 ;;
  esac
  printf '%s' "${out%"${CONFIG_SCAN_DONE_MARKER}"}"
}

# Pulls one credential-provider setting out of whichever source we got it
# from. Both sources arrive as a key and a value separated by "=" or ":": the
# kubelet command line as it is written there, and the config files as the
# node normalized them.
#
#   --image-credential-provider-bin-dir=/path      kubelet command line
#   image-credential-provider-bin-dir="/path"      from the node-side scan
#
# One extractor for both, because a regex per source is what made the k3s case
# report a working node as broken: the installer writes that value quoted, and
# an unquoted-only pattern stopped at the opening quote and yielded nothing.
# The quoted form is still tried first, so a value that reaches here quoted,
# from a hand-written config or a quoted command line, reads the same way. The
# unquoted form stops at whitespace or at a quote, so a trailing quote never
# ends up in the path.
#
# The key has to be followed by its separator, which is what keeps a longer
# flag that starts with the same characters from answering for it.
#
# Commented-out lines are dropped first. These files ship with commented
# examples, and counting one as an active setting reports a node as configured
# while kubelet is given nothing.
#
# The last match wins, not the first. The scan reads config.yaml before
# config.yaml.d/, and on k3s and RKE2 the drop-in replaces the key, so the
# first match is whatever the node was set to before the installer ran. A
# repeated flag on a kubelet command line works the same way: the last one is
# the one kubelet uses. An empty last match is an answer too: a later file
# dropped the flag, and the node is not configured.
extract_setting() {
  local key=$1 text=$2
  printf '%s\n' "${text}" | sed '/^[[:space:]]*#/d' | sed -n "
    s/.*${key}[=:][[:space:]]*\"\([^\"]*\)\".*/\1/p
    s/.*${key}[=:][[:space:]]*\([^\"[:space:]][^\"[:space:]]*\).*/\1/p
  " | tail -1 || true
}

report() {
  local ok=$1 good=$2 bad=$3
  if [ "${ok}" = "yes" ]; then green "  ${good}"; return 0; fi
  red "  ${bad}"; return 1
}

check_node() {
  local node=$1 failed=0 cmdline files ok

  echo "=== ${node} ==="

  if ! cmdline=$(on_node "${node}" "${KUBELET_CMDLINE_PROGRAM}" "${NO_KUBELET_MARKER}"); then
    red "  could not reach the node (is kubectl debug allowed here?)"
    return 1
  fi

  if [ -z "${cmdline}" ]; then
    red "  the debug pod produced no output, three times"
    echo "     That is a kubectl debug problem, not a diagnosis of the node."
    echo "     If kubelet was just restarted, give it a moment and run again."
    return 1
  fi

  case "${cmdline}" in
    *"${NO_KUBELET_MARKER}"*)
      red "  no kubelet process found"
      echo "     Looked for kubelet, kubelite, and the rke2 and k3s supervisors."
      return 1
      ;;
  esac

  local bindir configpath
  bindir=$(extract_setting image-credential-provider-bin-dir "${cmdline}")
  configpath=$(extract_setting image-credential-provider-config "${cmdline}")

  local source="the kubelet command line"
  if [ -z "${bindir}" ] || [ -z "${configpath}" ]; then
    # k3s, RKE2 and MicroK8s pass these to their embedded kubelet from a file,
    # so they never show up in /proc. Absent from the command line is not yet a
    # failure on those.
    local fromconfig
    if ! fromconfig=$(scan_node_kubelet_config "${node}"); then
      red "  could not read the kubelet argument files on the node"
      echo "     The scan did not run to completion, so a node that is set up"
      echo "     correctly would be reported as broken here."
      return 1
    fi
    bindir=$(extract_setting image-credential-provider-bin-dir "${fromconfig}")
    configpath=$(extract_setting image-credential-provider-config "${fromconfig}")
    source="a k3s, RKE2 or MicroK8s arguments file"
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
