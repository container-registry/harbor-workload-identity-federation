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

# The active assignment only. A commented-out line, or a different provider's
# bin-dir flag somewhere else in the file, must not look like "already done".
active=$(grep '^[[:space:]]*KUBELET_FLAGS=' "$DEFAULTS" || true)
if [ -z "$active" ]; then
  echo "ERROR: no KUBELET_FLAGS assignment in $DEFAULTS."
  echo "       This node does not build its kubelet command line the way AKS does."
  echo "       Add the two flags by hand:"
  echo "         --image-credential-provider-bin-dir=$BIN_DIR"
  echo "         --image-credential-provider-config=$CONFIG_PATH"
  exit 1
fi

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
# handle both rather than silently matching neither. The result is always
# quoted: an unquoted value with spaces is fine for systemd's EnvironmentFile
# parser but breaks anything that sources the file as shell.
awk -v bindir="$BIN_DIR" -v config="$CONFIG_PATH" '
  /^[[:space:]]*KUBELET_FLAGS=/ && !done {
    flags = "--image-credential-provider-bin-dir=" bindir " --image-credential-provider-config=" config
    line = $0
    sub(/[[:space:]]+$/, "", line)
    eq = index(line, "=")
    key = substr(line, 1, eq - 1)
    value = substr(line, eq + 1)
    gsub(/^"|"$/, "", value)
    print key "=\"" value " " flags "\""
    done = 1
    next
  }
  { print }
' "$DEFAULTS" > "$DEFAULTS.new"

mv "$DEFAULTS.new" "$DEFAULTS"

grep '^[[:space:]]*KUBELET_FLAGS=' "$DEFAULTS" | grep -q -- '--image-credential-provider-bin-dir=' || {
  echo "ERROR: could not patch KUBELET_FLAGS in $DEFAULTS. Restoring the backup; edit it by hand."
  cp "$DEFAULTS.before-credential-provider-harbor" "$DEFAULTS"
  exit 1
}

echo "Patched $DEFAULTS. Backup at $DEFAULTS.before-credential-provider-harbor"
systemctl restart kubelet
echo "Restarted kubelet"
