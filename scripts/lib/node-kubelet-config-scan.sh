#!/bin/sh
#
# Node side of scripts/verify-node-install.sh: reads the files k3s, RKE2 and
# MicroK8s keep their kubelet arguments in, and prints every credential
# provider setting it finds as one quoted "key=value" line, whatever shape the
# file wrote it in. The caller then has a single shape to parse instead of the
# seven below, and the normalization lives here rather than in the caller.
#
# Runs inside a debug pod chrooted into the host, so it stays POSIX sh and
# uses nothing beyond what a busybox image has. It takes the sentinel to print
# when it has finished as $1, so the caller owns both ends of that word.
#
# The shapes it reads, all of which the installer or a hand-written config can
# produce:
#
#   image-credential-provider-bin-dir: "/path"    k3s drop-in, as installed
#   image-credential-provider-bin-dir: /path      k3s drop-in, hand written
#   - "image-credential-provider-bin-dir=/path"   RKE2 kubelet-arg list item
#   kubelet-arg: "image-...-bin-dir=/path"        RKE2 kubelet-arg scalar
#   --image-credential-provider-bin-dir=/path     MicroK8s snap arguments
#   --image-credential-provider-bin-dir /path     same, value as a second field
#   --image-credential-provider-bin-dir           same, value on the next line
#
# A flag left without a value takes the next line as its value, but only if
# that line can be one: an empty line, another flag or a comment leaves the
# flag unset rather than being swallowed as its value. That is the reading
# side of setMicroK8sKubeletArgs in the installer, which writes that file.
#
# The sentinel is printed last. A node with none of these files is the normal
# case, and finding nothing in them is a real answer. Without the sentinel,
# that node and a debug pod that never attached both come back as an empty
# string, and the caller would report a working node as broken.

sentinel=${1:?the sentinel to print when the scan has finished}

# Prints one normalized line for a setting worth reporting. The value is
# printed quoted: the caller's unquoted pattern stops at the first space, and
# a path with a space in it would otherwise come back truncated.
emit() {
  emit_key=${1#--}
  emit_value=$2
  [ -n "$emit_value" ] || return 0

  case "$emit_key" in
    *image-credential-provider*)
      echo "$emit_key=\"$emit_value\""
      return 0
      ;;
  esac

  # A scalar kubelet-arg carries the whole flag and its value:
  #
  #   kubelet-arg: "image-credential-provider-bin-dir=/path"
  #
  # The key of that line is kubelet-arg, and the setting is in the value.
  case "$emit_value" in
    *image-credential-provider*=*)
      emit_key=${emit_value%%=*}
      emit_key=${emit_key#--}
      emit_value=${emit_value#*=}
      case "$emit_key" in
        *image-credential-provider*)
          [ -n "$emit_value" ] && echo "$emit_key=\"$emit_value\""
          ;;
      esac
      ;;
  esac
  return 0
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

      # Both separators occur inside real values: a path may contain a colon,
      # a flag's value may contain an equals sign, and a scalar kubelet-arg's
      # value is itself a "flag=value" pair. So the separator that comes
      # first is the one that separates this line's key from its value,
      # rather than one of them always winning.
      case "$line" in
        --*)
          line=${line#--}
          before_eq=${line%%=*}
          before_space=${line%% *}
          if [ "${#before_eq}" -lt "${#before_space}" ]; then
            key=$before_eq; value_of "${line#*=}"
          elif [ "${#before_space}" -lt "${#line}" ]; then
            key=$before_space; trim "${line#* }"; value_of "$TRIMMED"
          else
            # A flag with no value on this line. The next line may be it.
            pending=$line
            continue
          fi
          ;;
        *)
          before_eq=${line%%=*}
          before_colon=${line%%:*}
          if [ "${#before_eq}" -lt "${#before_colon}" ]; then
            key=$before_eq; value_of "${line#*=}"
          elif [ "${#before_colon}" -lt "${#line}" ]; then
            key=$before_colon; trim "${line#*:}"; value_of "$TRIMMED"
          else
            continue
          fi
          ;;
      esac
      emit "$key" "$VALUE"
    done
  }
done
echo "$sentinel"
