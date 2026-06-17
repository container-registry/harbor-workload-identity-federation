# Talos system extension image carrying the credential-provider-harbor binary.
# The extension rootfs is overlaid onto the (read-only) Talos rootfs, so the
# binary lands at /usr/local/lib/kubelet/credentialproviders inside the kubelet
# mount namespace -- the directory Talos hardcodes for credential providers.
#
# Build for the node architecture (amd64 shown here):
#   docker buildx build --platform linux/amd64 \
#     -t <registry>/credential-provider-harbor-extension:v0.1.0 --push .
FROM scratch
COPY manifest.yaml /
COPY rootfs/ /rootfs/
