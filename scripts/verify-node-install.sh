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
  # -F, not a pattern: a node name like "worker.example.com" read as a regex
  # would let its dots match any character and delete another node's debug
  # pods. A pod name cannot contain "/", so the fixed string "pod/node-..."
  # can only ever match at the start of a line from `kubectl get -o name`.
  pods=$(kubectl get pods -n "${NAMESPACE}" -o name 2>/dev/null \
    | grep -F -e "pod/node-debugger-${node}-" || true)
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

# k3s, RKE2 and MicroK8s keep the settings in a file rather than on a command
# line, so an empty command line is not yet a failure on those.
#
# Those files spell a setting six different ways, and MicroK8s may even put a
# flag and its value on two separate lines. Collecting the raw lines and
# picking them apart here cannot work for that last one: by the time the lines
# arrive, nothing says which of them belonged together. So the node normalizes
# instead, and every setting it recognizes comes back as one "key=value" line
# whatever it looked like in the file:
#
#   image-credential-provider-bin-dir: "/path"    k3s drop-in, as installed
#   image-credential-provider-bin-dir: /path      k3s drop-in, hand written
#   - "image-credential-provider-bin-dir=/path"   RKE2 kubelet-arg list item
#   --image-credential-provider-bin-dir=/path     MicroK8s snap arguments
#   --image-credential-provider-bin-dir /path     same, value as a second field
#   --image-credential-provider-bin-dir           same, value on the next line
#
# A flag left without a value takes the next line as its value, but only if
# that line can be one: an empty line, another flag or a comment leaves the
# flag unset rather than being swallowed as its value. That is the reading
# side of setMicroK8sKubeletArgs in the installer, which writes that file.
#
# CONFIG_SCAN_DONE is printed last. Without it, the node that was scanned and
# had nothing and the debug pod that never attached both come back as an empty
# string, and the second one would be reported as a broken node.
# shellcheck disable=SC2016  # this expands on the node, not here.
CONFIG_DROPIN_SNIPPET='
emit() {
  if [ -z "$2" ]; then return 0; fi
  case "$1" in
    *image-credential-provider*) echo "$1=$2" ;;
  esac
}

# Sets TRIMMED to $1 without leading or trailing spaces. Tabs became spaces on
# the way in, so spaces are the whole of it.
trim() {
  TRIMMED=$1
  while :; do
    case "$TRIMMED" in
      " "*) TRIMMED=${TRIMMED# } ;;
      *" ") TRIMMED=${TRIMMED% } ;;
      *) break ;;
    esac
  done
}

# Sets VALUE to what a flag is actually set to: a quoted value verbatim, an
# unquoted one only up to the first space, so that a trailing comment or a
# following field never ends up inside a path.
value_of() {
  VALUE=$1
  case "$VALUE" in
    \"*) VALUE=${VALUE#\"}; VALUE=${VALUE%%\"*} ;;
    *) VALUE=${VALUE%% *} ;;
  esac
}

for f in /etc/rancher/k3s/config.yaml /etc/rancher/k3s/config.yaml.d/*.yaml \
         /etc/rancher/rke2/config.yaml /etc/rancher/rke2/config.yaml.d/*.yaml \
         /var/snap/microk8s/current/args/kubelet; do
  [ -f "$f" ] || continue
  tr -d "\r" < "$f" | tr "\t" " " | {
    pending=""
    while IFS= read -r line || [ -n "$line" ]; do
      trim "$line"; line=$TRIMMED

      # A flag from the line before claims this line, unless this line is one
      # of the three things that cannot be a value.
      if [ -n "$pending" ]; then
        case "$line" in
          ""|-*|"#"*) pending="" ;;
          *) value_of "$line"; emit "$pending" "$VALUE"; pending=""; continue ;;
        esac
      fi

      case "$line" in
        ""|"#"*) continue ;;
      esac

      # A YAML list item carries the whole "key=value" pair, quoted or not.
      case "$line" in
        --*) ;;
        -*)
          line=${line#-}
          trim "$line"; line=$TRIMMED
          case "$line" in
            \"*\") line=${line#\"}; line=${line%\"} ;;
          esac
          ;;
      esac

      case "$line" in
        --*)
          line=${line#--}
          case "$line" in
            *=*) key=${line%%=*}; value_of "${line#*=}" ;;
            *" "*) key=${line%% *}; trim "${line#* }"; value_of "$TRIMMED" ;;
            *) pending=$line; continue ;;
          esac
          ;;
        *:*) key=${line%%:*}; trim "${line#*:}"; value_of "$TRIMMED" ;;
        *=*) key=${line%%=*}; value_of "${line#*=}" ;;
        *) continue ;;
      esac
      emit "$key" "$VALUE"
    done
  }
done
echo CONFIG_SCAN_DONE
'

# Pulls one credential-provider setting out of whichever source we got it
# from. Both sources arrive as a key and a value separated by "=" or ":": the
# kubelet command line as it is written there, and the config files as the
# node normalized them.
#
#   --image-credential-provider-bin-dir=/path      kubelet command line
#   image-credential-provider-bin-dir=/path        from CONFIG_DROPIN_SNIPPET
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
extract_setting() {
  local key=$1 text=$2
  printf '%s\n' "${text}" | sed '/^[[:space:]]*#/d' | sed -n "
    s/.*${key}[=:][[:space:]]*\"\([^\"]*\)\".*/\1/p
    s/.*${key}[=:][[:space:]]*\([^\"[:space:]][^\"[:space:]]*\).*/\1/p
  " | head -1 || true
}

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
  bindir=$(extract_setting image-credential-provider-bin-dir "${cmdline}")
  configpath=$(extract_setting image-credential-provider-config "${cmdline}")

  local source="the kubelet command line"
  if [ -z "${bindir}" ] || [ -z "${configpath}" ]; then
    # k3s, RKE2 and MicroK8s pass these to their embedded kubelet from a file,
    # so they never show up in /proc. Absent from the command line is not yet a
    # failure on those.
    local fromconfig
    fromconfig=$(on_node "${node}" "${CONFIG_DROPIN_SNIPPET}") || fromconfig=""
    case "${fromconfig}" in
      *CONFIG_SCAN_DONE*) ;;
      *)
        red "  could not read the kubelet argument files on the node"
        echo "     The scan did not run to completion, so a node that is set up"
        echo "     correctly would be reported as broken here."
        return 1
        ;;
    esac
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
