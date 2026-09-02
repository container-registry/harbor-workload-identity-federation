# Talos Linux Example

This example shows how to pull Harbor images on [Talos Linux](https://www.talos.dev/)
with the kubelet credential provider and service account tokens, with no
`imagePullSecrets`.

Talos is different from the [`kubernetes`](../kubernetes/) example: the Helm
DaemonSet installer does **not** work here. Talos has no systemd and an
immutable root filesystem, so the provider binary cannot be copied onto a node
at runtime. Instead, the binary ships as the official Talos system extension
[`siderolabs/harbor-credential-provider`](https://github.com/siderolabs/extensions/tree/main/container-runtime/harbor-credential-provider),
and the kubelet is wired through `machine.kubelet.credentialProviderConfig`.

> [!WARNING]
> **A node reboot is required.** Installing the extension is a `talosctl upgrade`,
> which reboots the node. Plan for it on the workers that pull Harbor images.

## Prerequisites

- Talos **v1.11+**. Talos v1.11 ships Kubernetes 1.34, where the
  `KubeletServiceAccountTokenForCredentialProviders` and
  `ServiceAccountNodeAudienceRestriction` feature gates are on by default.
- `talosctl` for your cluster and the ability to `talosctl upgrade` workers.
- **Talos v1.11 to v1.13 only:** Docker, to build a custom installer image
  with `imager`, and a container registry the nodes can reach to host it
  (`<image-registry>` below, distinct from the Harbor host you pull from).

Placeholders used below: `harbor.example.com` (the Harbor host, used here as the
**audience** too), `<cluster-service-account-issuer>`, `<project>`/`<image>`.
See the [root README](../../README.md) for the full Harbor Federated IDP
reference and per-provider JWT claim tables.

> [!NOTE]
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

## The extension

Talos hardcodes the credential provider bin directory to
`/usr/local/lib/kubelet/credentialproviders` and sets the kubelet flags itself
when `machine.kubelet.credentialProviderConfig` is present. That directory is a
read-only overlay populated only by system extensions; `machine.kubelet.extraArgs`
cannot redirect it. So the binary must be delivered as an extension.

Sidero builds and publishes the extension from a tagged release of this
repository (`cmd/credential-provider-harbor`):

| | |
|---|---|
| Extension name | `siderolabs/harbor-credential-provider` |
| Image | `ghcr.io/siderolabs/harbor-credential-provider:v0.0.1` (linux/amd64, linux/arm64) |
| Installs | `/usr/local/lib/kubelet/credentialproviders/harbor-credential-provider` |
| Talos compatibility | `>= v1.11.0` |
| Image Factory catalog | Talos **v1.14.0-rc.1 and later** |
| Source | [siderolabs/extensions/container-runtime/harbor-credential-provider](https://github.com/siderolabs/extensions/tree/main/container-runtime/harbor-credential-provider) |

> [!IMPORTANT]
> **Naming.** On Talos the binary and therefore the provider `name` in the
> kubelet config is **`harbor-credential-provider`**, not
> `credential-provider-harbor` as in the other examples of this repository. The
> kubelet resolves the provider binary by `name`, so a mismatch means the
> provider is never invoked.

## Delivering the extension

Pick the route for your Talos version:

- **Image Factory (Talos v1.14+).** The extension is in the factory catalog, so
  a schematic is enough; no local build. See [Option A](#option-a-image-factory-talos-v114).
- **imager (Talos v1.11 to v1.13).** The factory catalog for these releases
  does not contain the extension and rejects the schematic with
  `official extension "siderolabs/harbor-credential-provider" is not available for Talos version v1.13.9`.
  The extension image itself is compatible with these releases, so bake it
  into a custom installer with `imager`. See [Option B](#option-b-imager-talos-v111-to-v113).

Check the catalog for any Talos version with:

```bash
curl -s https://factory.talos.dev/version/<talos-version>/extensions/official | jq -r '.[].name' | grep harbor
```

### Option A: Image Factory (Talos v1.14+)

Register the schematic in [`schematic.yaml`](schematic.yaml):

```bash
curl -X POST --data-binary @examples/talos/schematic.yaml https://factory.talos.dev/schematics
```

```json
{"id":"41acf9f8f17d138cb7ca2439b4c415c55555cdb608c995379451976e4125797b"}
```

Schematic IDs are content-addressed, so this exact file always yields the ID
above. If you add other extensions or customizations you get a different ID.

Upgrade the worker nodes with the factory installer (bare metal shown; other
platforms use `<platform>-installer` per the Talos boot-assets docs). This
reboots each node; Talos cordons and drains automatically.

```bash
talosctl --nodes <worker-ip> upgrade \
  --image factory.talos.dev/metal-installer/41acf9f8f17d138cb7ca2439b4c415c55555cdb608c995379451976e4125797b:<talos-version> \
  --wait
```

Continue with [Verify the extension](#verify-the-extension).

### Option B: imager (Talos v1.11 to v1.13)

Use the `imager` matching your **running** Talos version and node architecture.
Pin the extension by digest; the one below is the `v0.0.1` digest from the
`ghcr.io/siderolabs/extensions:v1.14.0-rc.2` catalog (refresh it with
`crane export ghcr.io/siderolabs/extensions:<talos-version> | tar x -O image-digests | grep harbor`).

```bash
mkdir -p _out
docker run --rm -t -v "$PWD/_out:/out" \
  ghcr.io/siderolabs/imager:v1.13.9 installer \
  --arch amd64 \
  --system-extension-image ghcr.io/siderolabs/harbor-credential-provider:v0.0.1@sha256:653be5a30e33792bec0d323bbedee0016c773edc59f8e19ec0ad5eee832a9295
```

The imager log lists the discovered extension (`harbor-credential-provider v0.0.1`)
and writes `_out/installer-amd64.tar`. Load, tag, and push it so the node can
pull it:

```bash
docker load -i _out/installer-amd64.tar          # Loaded image: ghcr.io/siderolabs/installer-base:v1.13.9
docker tag ghcr.io/siderolabs/installer-base:v1.13.9 <image-registry>/talos-installer-harbor:v1.13.9
docker push <image-registry>/talos-installer-harbor:v1.13.9
```

Upgrade the worker nodes. This reboots each node; Talos cordons and drains
automatically.

```bash
talosctl --nodes <worker-ip> upgrade \
  --image <image-registry>/talos-installer-harbor:v1.13.9 --wait
```

### Building the extension from an unreleased version

The official extension pins a release tag of this repository
(`HARBOR_WIF_VERSION` in `container-runtime/vars.yaml` of
[siderolabs/extensions](https://github.com/siderolabs/extensions)). To ship an
unreleased build, fork that repository, bump the version and checksums, and
build with its tooling:

```bash
make harbor-credential-provider PLATFORM=linux/amd64 PUSH=true REGISTRY=<image-registry> USERNAME=<namespace>
```

Then pass the resulting image to `imager --system-extension-image` as in
Option B. Do not hand-roll a `FROM scratch` extension image; the upstream build
runs the extensions validator and produces the SBOM Talos expects.

## Verify the extension

After the upgrade, confirm the extension and binary are present:

```bash
talosctl --nodes <worker-ip> get extensions
talosctl --nodes <worker-ip> ls /usr/local/lib/kubelet/credentialproviders
```

Expected: an extension named `harbor-credential-provider` and a file with the
same name in the bin directory.

## Wire the kubelet and apply RBAC

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

## Test a secretless pull

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
  flags first. If they are missing, the config patch did not apply.
- **`audience not found in pod spec volume`** — the node-audience RBAC is missing
  or its `resources:` entry does not equal the audience.
- **Kubelet does not invoke the provider** — confirm the image matches a
  `matchImages` entry, the extension is listed by `talosctl get extensions`,
  and the provider `name` in the config is `harbor-credential-provider`.
  Inspect with:

  ```bash
  talosctl --nodes <worker-ip> logs kubelet | grep -i credential
  ```

- **Factory rejects the schematic** (`official extension ... is not available
  for Talos version ...`) — the running Talos release predates the extension's
  catalog entry. Use [Option B](#option-b-imager-talos-v111-to-v113).
- **Claim rule mismatch** — the token `sub` is
  `system:serviceaccount:<namespace>:<service-account>`; make sure the
  workload's ServiceAccount matches the Harbor claim rule.

## Files

| File | Purpose |
|------|---------|
| [`schematic.yaml`](schematic.yaml) | Image Factory schematic that includes the official extension (Talos v1.14+). |
| [`talos-machine-config-patch.yaml`](talos-machine-config-patch.yaml) | `machine.kubelet.credentialProviderConfig` patch that wires the kubelet to `harbor-credential-provider`. |
| [`node-audience-rbac.yaml`](node-audience-rbac.yaml) | RBAC for kubelets to request service account tokens with the Harbor audience. |
| [`workload-example.yaml`](workload-example.yaml) | Namespace + dedicated ServiceAccount + Deployment that pulls from Harbor without `imagePullSecrets`. |
