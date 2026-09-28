# Installing credential-provider-harbor

The Helm chart is the recommended path. It runs a privileged DaemonSet on every node, copies the credential provider binary onto the host, writes or merges the kubelet credential provider config, creates the node audience RBAC, configures kubelet where the selected profile supports it, and restarts kubelet by default.

The distribution pages in [`examples/kubernetes/`](../examples/kubernetes/) are the per-node detail: where each distribution keeps its kubelet arguments, and what else it takes there.

## Before You Start

```bash
export REGISTRY_ADDRESS=8gears.container-registry.com
export PROJECT_NAME=8gcr
export INSTALLER_IMAGE="${REGISTRY_ADDRESS}/${PROJECT_NAME}/credential-provider-harbor-deployer"

# Harbor registry that workloads pull from.
export HARBOR_REGISTRY=harbor.example.com

# Audience Harbor expects in service account tokens. Use the exact value
# configured in Harbor.
export HARBOR_AUDIENCE=https://harbor.example.com
```

## Install

From the published chart:

```bash
helm upgrade --install credential-provider-harbor \
  "oci://${REGISTRY_ADDRESS}/${PROJECT_NAME}/credential-provider-harbor" \
  --namespace kube-system \
  --create-namespace \
  --set image.repository="${INSTALLER_IMAGE}" \
  --set registry.host="${HARBOR_REGISTRY}" \
  --set registry.audience="${HARBOR_AUDIENCE}"
```

From a local checkout, swap the chart reference for `deploy/helm/credential-provider-harbor/`.

A pod turns ready once its node has finished this revision's install, kubelet restart included, so the rollout says when the cluster is done:

```bash
kubectl rollout status daemonset/credential-provider-harbor -n kube-system
```

On a cluster where every node going NotReady at once is unacceptable, read [First Install](../deploy/helm/credential-provider-harbor/README.md#first-install) before running that. `maxUnavailable` bounds upgrades, not the first install.

## Profiles

The profile decides where the binary and the config go, and how kubelet is told about them.

| Profile | Use case | Host config path |
|---------|----------|------------------|
| `generic` | kubeadm and other systemd nodes | `/etc/kubernetes/credential-providers/config.yaml` |
| `eks` or `aws` | Amazon EKS AL2023 nodes | `/etc/eks/image-credential-provider/config.json` |
| `k3s` or `k3d` | k3s and k3d nodes | `/var/lib/rancher/credentialprovider/config.yaml` |
| `rke2` | RKE2 server and agent nodes | `/var/lib/rancher/credentialprovider/config.yaml` |
| `kind` | kind node containers | `/var/lib/kubelet/credential-provider-config.yaml` |
| `gke` | GKE Standard, best effort | generic kubelet paths |
| `aks` | AKS nodes | generic kubelet paths, wired through `/etc/default/kubelet` |
| `microk8s` | MicroK8s nodes | `/var/snap/microk8s/common/credentialprovider/config.yaml` |
| `custom` | anything else | whatever you set |

Pass it with `--set profile=<name>`, added to the install command above. A few need more than the name:

```bash
# Custom host paths. profile=custom sets no defaults, so both are required.
--set profile=custom \
--set credentialProvider.binDir=/opt/kubelet/credential-providers \
--set credentialProvider.configPath=/etc/kubernetes/credential-providers/config.yaml

# Write the files but leave kubelet alone, for a staged rollout.
--set kubelet.restart=false
```

`eks` keeps the ECR credential provider entry the AMI already has in its config rather than replacing it. On GKE, Container-Optimized OS nodes need `profile=custom` instead of `profile=gke`, because `/usr` is `noexec` there and the binary cannot go in the usual place; [`examples/kubernetes/gke/`](../examples/kubernetes/gke/) covers both node images.

GKE Autopilot is not supported. It does not allow the privileged host access the installer needs.

Talos does not use the chart at all. It installs the provider through the official `siderolabs/harbor-credential-provider` system extension; see [`examples/talos/`](../examples/talos/).

## Values

The [chart README](../deploy/helm/credential-provider-harbor/README.md) covers the values you are likely to change, and [`values.yaml`](../deploy/helm/credential-provider-harbor/values.yaml) is the full list.

`values.schema.json` catches the usual mistakes at `helm install` time rather than leaving you to find them on a node: no `registry.host`, an unknown profile, `profile=custom` with no paths set, a relative host path, a bad cache duration, a marker file on tmpfs, a path with characters the kubelet argument files cannot hold, and a `securityContext` that takes away privileges the installer needs.

## Installing By Hand

Useful on a cluster the chart will not install into, and on Kubernetes 1.33, where you have to enable the `ServiceAccountNodeAudienceRestriction` and `KubeletServiceAccountTokenForCredentialProviders` feature gates yourself. The chart sets `kubeVersion: ">=1.34.0-0"` and refuses to install below 1.34.

Put the binary on every node:

```bash
curl -Lo credential-provider-harbor \
  https://github.com/container-registry/harbor-workload-identity-federation/releases/latest/download/credential-provider-harbor-linux-amd64

chmod +x credential-provider-harbor

sudo mkdir -p /usr/local/bin/credential-providers
sudo mv credential-provider-harbor /usr/local/bin/credential-providers/
```

Pick the architecture that matches the node. One built for the wrong one fails the pull with `exec format error` and nothing more useful.

Write the credential provider config:

```yaml
# /etc/kubernetes/credential-providers/config.yaml
kind: CredentialProviderConfig
apiVersion: kubelet.config.k8s.io/v1
providers:
- name: credential-provider-harbor
  apiVersion: credentialprovider.kubelet.k8s.io/v1
  tokenAttributes:
    requireServiceAccount: true
    serviceAccountTokenAudience: "<your-registry-domain>"
    cacheType: Token
  matchImages:
  - "<your-registry-domain>"
  defaultCacheDuration: "1h"
  args:
  - "--username=jwt"
```

Point kubelet at both:

```text
--image-credential-provider-bin-dir=/usr/local/bin/credential-providers
--image-credential-provider-config=/etc/kubernetes/credential-providers/config.yaml
```

Apply the node audience RBAC, which the chart would otherwise create:

```bash
kubectl apply -f examples/kubernetes/rbac-audience.yaml
```

## Checking a Node

The files are not what decides. kubelet has to be running with the two flags:

```bash
./scripts/verify-node-install.sh
```

It reads the live kubelet command line on every node, falls back to the argument files on the distributions that keep them there, and exits nonzero if any node is not set up.

## When a Pull Fails

| Error | Cause | What to do |
|-------|-------|------------|
| `no basic auth credentials` | kubelet is running without the two flags, so the provider was never called | `./scripts/verify-node-install.sh` |
| `audience not found in pod spec volume` | The node audience RBAC is missing | Apply [`rbac-audience.yaml`](../examples/kubernetes/rbac-audience.yaml) |
| `no robots matched your token` | No federated robot account has claim rules the token satisfies | See [Token Reference](token-reference.md) |
| `401 Unauthorized` | The JWKS or the audience in the Trusted Issuer does not match the cluster | Re-export the JWKS and check the audience on both sides |
| `exec format error` | The binary on the node is built for another architecture | Install the one matching the node |

Recreating a cluster generates new service account signing keys, so a Trusted Issuer configured with inline JWKS has to be updated with the new ones.

## Uninstalling

`helm uninstall` removes the DaemonSet, the ServiceAccount and the RBAC. It does not clean the nodes. The binary, the config, the kubelet drop-in and the marker file stay where the installer put them, and kubelet goes on calling the provider. [Uninstalling](../deploy/helm/credential-provider-harbor/README.md#uninstalling) lists what to delete to put a node back.
