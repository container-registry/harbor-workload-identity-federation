#!/bin/sh
#
# Add the credential provider flags to an AKS node's kubelet and restart it.
#
# AKS builds the kubelet command line from KUBELET_FLAGS in
# /etc/default/kubelet. Run this on the node, as root, after the chart has
# placed the binary and the config.
#
#   kubectl debug node/<node> -it --image=busybox -- chroot /host sh
#   # then paste this script, or curl it in
#
# It is idempotent: running it twice changes nothing the second time.

set -eu

BIN_DIR="${BIN_DIR:-/usr/local/bin/credential-providers}"
BINARY_NAME="${BINARY_NAME:-credential-provider-harbor}"
CONFIG_PATH="${CONFIG_PATH:-/etc/kubernetes/credential-providers/config.yaml}"
DEFAULTS="${DEFAULTS:-/etc/default/kubelet}"

test -x "$BIN_DIR/$BINARY_NAME" \
  || { echo "ERROR: $BIN_DIR/$BINARY_NAME is missing. Install the chart first."; exit 1; }
test -f "$CONFIG_PATH" \
  || { echo "ERROR: $CONFIG_PATH is missing. Install the chart first."; exit 1; }
test -f "$DEFAULTS" \
  || { echo "ERROR: $DEFAULTS not found. This does not look like an AKS node."; exit 1; }

# The value of the single active KUBELET_FLAGS assignment, with any inline
# comment removed and the surrounding quotes stripped. Everything that decides
# whether the flags are present reads this, so a flag sitting in a comment or
# in a commented-out line never counts as "already done".
kubelet_flags_value() {
  awk '/^[[:space:]]*KUBELET_FLAGS=/ {
         eq = index($0, "=")
         value = substr($0, eq + 1)
         sub(/[[:space:]]+#.*$/, "", value)
         sub(/^[[:space:]]+/, "", value)
         sub(/[[:space:]]+$/, "", value)
         gsub(/^"|"$/, "", value)
         print value
       }' "$1"
}

assignments=$(grep -c '^[[:space:]]*KUBELET_FLAGS=' "$DEFAULTS" || true)
if [ "$assignments" -eq 0 ]; then
  echo "ERROR: no KUBELET_FLAGS assignment in $DEFAULTS."
  echo "       This node does not build its kubelet command line the way AKS does."
  echo "       Add the two flags by hand:"
  echo "         --image-credential-provider-bin-dir=$BIN_DIR"
  echo "         --image-credential-provider-config=$CONFIG_PATH"
  exit 1
fi
if [ "$assignments" -gt 1 ]; then
  # systemd's EnvironmentFile lets the last assignment win. Patching one of
  # several would leave kubelet running without the flags while this script
  # reported success, so refuse rather than guess which one is effective.
  echo "ERROR: $DEFAULTS has $assignments active KUBELET_FLAGS assignments:"
  grep -n '^[[:space:]]*KUBELET_FLAGS=' "$DEFAULTS"
  echo "       The last one wins. Collapse them into a single assignment,"
  echo "       then run this again."
  exit 1
fi

active=$(kubelet_flags_value "$DEFAULTS")

has_bindir=$(printf '%s' "$active" | grep -c -- '--image-credential-provider-bin-dir=' || true)
has_config=$(printf '%s' "$active" | grep -c -- '--image-credential-provider-config=' || true)
if [ "$has_bindir" -gt 0 ] && [ "$has_config" -gt 0 ]; then
  echo "Already configured: $DEFAULTS"
  exit 0
fi
if [ "$has_bindir" -gt 0 ] || [ "$has_config" -gt 0 ]; then
  echo "ERROR: $DEFAULTS has one of the two flags but not both:"
  echo "  $active"
  echo "       Fix it by hand rather than letting this script append a duplicate."
  exit 1
fi

cp "$DEFAULTS" "$DEFAULTS.before-credential-provider-harbor"

# AKS images have written this both quoted and unquoted over the years, so
# handle both rather than silently matching neither. An inline comment is
# lifted off before the quotes come off and put back after the new closing
# quote, otherwise the appended flags land inside the comment and kubelet
# never sees them. The result is always quoted: an unquoted value with spaces
# is fine for systemd's EnvironmentFile parser but breaks anything that
# sources the file as shell. There is exactly one assignment to patch; the
# duplicate check above refused the file otherwise.
awk -v bindir="$BIN_DIR" -v config="$CONFIG_PATH" '
  /^[[:space:]]*KUBELET_FLAGS=/ {
    flags = "--image-credential-provider-bin-dir=" bindir " --image-credential-provider-config=" config
    line = $0
    sub(/[[:space:]]+$/, "", line)
    eq = index(line, "=")
    key = substr(line, 1, eq - 1)
    value = substr(line, eq + 1)
    comment = ""
    if (match(value, /[[:space:]]+#.*$/)) {
      comment = substr(value, RSTART, RLENGTH)
      value = substr(value, 1, RSTART - 1)
    }
    gsub(/^"|"$/, "", value)
    print key "=\"" value " " flags "\"" comment
    next
  }
  { print }
' "$DEFAULTS" > "$DEFAULTS.new"

mv "$DEFAULTS.new" "$DEFAULTS"

# Read the assignment back the same way the pre-check read it: both flags have
# to be in the parsed value, not merely somewhere on the line.
patched=$(kubelet_flags_value "$DEFAULTS")
if ! printf '%s' "$patched" | grep -q -- '--image-credential-provider-bin-dir=' \
  || ! printf '%s' "$patched" | grep -q -- '--image-credential-provider-config='; then
  echo "ERROR: could not patch KUBELET_FLAGS in $DEFAULTS. Restoring the backup; edit it by hand."
  cp "$DEFAULTS.before-credential-provider-harbor" "$DEFAULTS"
  exit 1
fi

echo "Patched $DEFAULTS. Backup at $DEFAULTS.before-credential-provider-harbor"
systemctl restart kubelet
echo "Restarted kubelet"
