# Talos Linux Example

This example shows how to pull Harbor images on [Talos Linux](https://www.talos.dev/)
with `credential-provider-harbor` and service account tokens, with no
`imagePullSecrets`.

Talos is different from the [`kubernetes`](../kubernetes/) example: the Helm
DaemonSet installer does **not** work here. Talos has no systemd and an
immutable root filesystem, so the provider binary cannot be copied onto a node
at runtime. Instead, the binary ships in a **system extension** baked into the
Talos installer image, and the kubelet is wired through `machine.kubelet.credentialProviderConfig`.

> **A node reboot is required.** Installing the extension is a `talosctl upgrade`,
> which reboots the node. Plan for it on the workers that pull Harbor images.

## Prerequisites

- Kubernetes **1.34+** (the `KubeletServiceAccountTokenForCredentialProviders`
  and `ServiceAccountNodeAudienceRestriction` feature gates are on by default).
- `talosctl` for your cluster and the ability to `talosctl upgrade` workers.
- Docker with `buildx`, to build the extension and a custom installer image.
- A container registry the nodes can reach, to host the installer image
  (`<image-registry>` below — distinct from the Harbor host you pull from).

Placeholders used below: `harbor.example.com` (the Harbor host, used here as the
**audience** too), `<cluster-service-account-issuer>`, `<project>`/`<image>`.
See the [root README](../../README.md) for the full Harbor Federated IDP
reference and per-provider JWT claim tables.

> **About the audience.** The audience (`aud`) is just an agreed identifier
> string — it does **not** have to be a domain. The only hard rule is that the
> **same value** appears in all three places: the kubelet
> `tokenAttributes.serviceAccountTokenAudience`, the node-audience RBAC
> `resources:` entry, and what the Harbor Federated IDP expects. This example
> uses the Harbor host `harbor.example.com` as that value because it makes the
> token's intended recipient obvious; any consistent string works.

## Harbor Setup

Find the cluster's service account issuer:

```bash
kubectl get --raw /.well-known/openid-configuration | jq -r .issuer
```

Create a Harbor Federated IDP for that issuer. When the issuer is not publicly
reachable (a common case for on-prem/private clusters), use **offline JWKS
validation**: fetch the cluster's public keys and paste them into the IDP as
inline JWKS, so Harbor never has to call the API server.

```bash
# JWKS to paste into the Harbor IDP (inline / offline validation)
kubectl get --raw "$(kubectl get --raw /.well-known/openid-configuration | jq -r .jwks_uri)"
```

```text
Issuer:   <cluster-service-account-issuer>
Audience: harbor.example.com
JWKS:     <inline keys from the command above>
```

Create a federated **robot account with pull permission** and a claim rule that
pins the workload identity on `sub`:

```text
iss == <cluster-service-account-issuer>
aud == harbor.example.com
sub == system:serviceaccount:<namespace>:<service-account>
```

For the sample workload in this directory:

```text
sub == system:serviceaccount:demo:harbor-puller
```

Use the same audience here as everywhere else — see the **About the audience**
note above for the three places it must match.

## Why a system extension

Talos hardcodes the credential provider bin directory to
`/usr/local/lib/kubelet/credentialproviders` and sets the kubelet flags itself
when `machine.kubelet.credentialProviderConfig` is present. That directory is a
read-only overlay populated only by system extensions; `machine.kubelet.extraArgs`
cannot redirect it. So the binary must be delivered as an extension.

## Delivering the extension

Two routes produce a Talos installer image that carries the provider binary:

- **Image Factory (recommended when available).** If the
  `credential-provider-harbor` extension is published to a
  [Talos Image Factory](https://factory.talos.dev/) (the hosted factory or a
  self-hosted one), define a schematic that references it and pull the installer
  it generates — no local build. Skip to [step 4](#4-upgrade-the-worker-nodes)
  using the factory installer reference,
  `factory.talos.dev/installer/<schematic-id>:<talos-version>`.
- **Build your own (custom / air-gapped).** Build the extension and installer
  locally with `imager` (steps 1-3 below). Use this when the extension is not in
  a factory catalog or you need a private/offline build.

The hosted Image Factory only serves extensions it can resolve, so a one-off
custom binary uses the build-your-own route.

## 1. Build the provider binary

Build for the node architecture (`amd64` shown) and place it in the extension
rootfs:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" \
  -o examples/talos/credential-provider-extension/rootfs/usr/local/lib/kubelet/credentialproviders/credential-provider-harbor \
  ./cmd/credential-provider-harbor
chmod 0755 examples/talos/credential-provider-extension/rootfs/usr/local/lib/kubelet/credentialproviders/credential-provider-harbor
```

You can also use a release binary instead of building from source.

## 2. Build the extension image

```bash
cd examples/talos/credential-provider-extension
docker buildx build --platform linux/amd64 \
  -t <image-registry>/credential-provider-harbor-extension:v0.1.0 --push .
```

## 3. Build a custom Talos installer

Use the Talos `imager` matching your **running** Talos version and node
architecture. It bakes the extension into an installer image:

```bash
mkdir -p _out
docker run --rm -t -v "$PWD/_out:/out" \
  ghcr.io/siderolabs/imager:v1.12.6 installer \
  --arch amd64 \
  --system-extension-image <image-registry>/credential-provider-harbor-extension:v0.1.0
```

Load, tag, and push the resulting installer so the node can pull it:

```bash
docker load -i _out/installer-amd64.tar          # loads ghcr.io/siderolabs/installer-base:<version>
docker tag <loaded-image-id> <image-registry>/talos-installer-harbor:v1.12.6
docker push <image-registry>/talos-installer-harbor:v1.12.6
```

## 4. Upgrade the worker nodes

This reboots each node; Talos cordons and drains automatically.

```bash
talosctl --nodes <worker-ip> upgrade \
  --image <image-registry>/talos-installer-harbor:v1.12.6 --wait
```

Confirm the extension and binary are present:

```bash
talosctl --nodes <worker-ip> get extensions
talosctl --nodes <worker-ip> ls /usr/local/lib/kubelet/credentialproviders
```

## 5. Wire the kubelet and apply RBAC

Apply the credential provider config (no reboot) and the node-audience RBAC:

```bash
talosctl --nodes <worker-ip> patch mc --mode auto \
  --patch @examples/talos/talos-machine-config-patch.yaml

kubectl apply -f examples/talos/node-audience-rbac.yaml
```

Verify the live kubelet command line includes both flags:

```bash
talosctl --nodes <worker-ip> ps | grep -o '/usr/local/bin/kubelet.*' \
  | tr ' ' '\n' | grep image-credential-provider
```

Expected:

```text
--image-credential-provider-bin-dir=/usr/local/lib/kubelet/credentialproviders
--image-credential-provider-config=/etc/kubernetes/kubelet-credentialproviderconfig.yaml
```

## 6. Test a secretless pull

The sample workload creates the `demo` namespace, a dedicated `harbor-puller`
ServiceAccount, and a Deployment with **no** `imagePullSecrets`. Edit the image
reference first.

```bash
kubectl apply -f examples/talos/workload-example.yaml
kubectl -n demo rollout status deploy/harbor-pull-demo
```

A successful rollout means the kubelet pulled the image using only the
ServiceAccount token.

## Troubleshooting

- **`no basic auth credentials` / `401 Unauthorized`** — check the live kubelet
  flags first (step 5). If they are missing, the config patch did not apply.
- **`audience not found in pod spec volume`** — the node-audience RBAC is missing
  or its `resources:` entry does not equal the audience.
- **Kubelet does not invoke the provider** — confirm the image matches a
  `matchImages` entry and the extension binary is present (step 4). Inspect with:

  ```bash
  talosctl --nodes <worker-ip> logs kubelet | grep -i credential
  ```

- **Claim rule mismatch** — the token `sub` is
  `system:serviceaccount:<namespace>:<service-account>`; make sure the
  workload's ServiceAccount matches the Harbor claim rule.

## Files

| File | Purpose |
|------|---------|
| [`credential-provider-extension/`](credential-provider-extension/) | Talos system extension that ships the provider binary (`manifest.yaml`, `Dockerfile`, `rootfs/`). |
| [`talos-machine-config-patch.yaml`](talos-machine-config-patch.yaml) | `machine.kubelet.credentialProviderConfig` patch that wires the kubelet. |
| [`node-audience-rbac.yaml`](node-audience-rbac.yaml) | RBAC for kubelets to request service account tokens with the Harbor audience. |
| [`workload-example.yaml`](workload-example.yaml) | Namespace + dedicated ServiceAccount + Deployment that pulls from Harbor without `imagePullSecrets`. |
