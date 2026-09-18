# kubeadm and Other systemd Nodes

The default case. Works on any node where kubelet runs under systemd and its unit expands `$KUBELET_EXTRA_ARGS`, which is what `kubeadm init` generates.

## What Gets Installed

| Path on the node | Written by |
|------------------|------------|
| `/usr/local/bin/credential-providers/credential-provider-harbor` | The binary, copied from the deployer image |
| `/etc/kubernetes/credential-providers/config.yaml` | The kubelet `CredentialProviderConfig` |
| `/etc/systemd/system/kubelet.service.d/99-credential-provider-harbor.conf` | A drop-in setting `KUBELET_EXTRA_ARGS` to the two provider flags |

Then `systemctl daemon-reload && systemctl restart kubelet`, one node at a time.

## Install

```bash
helm upgrade --install credential-provider-harbor \
  oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system \
  -f examples/kubernetes/kubeadm/values.yaml \
  --set registry.host=harbor.example.com \
  --set registry.audience=harbor.example.com

kubectl rollout status daemonset/credential-provider-harbor -n kube-system
```

## Check It Took

```bash
./scripts/verify-node-install.sh            # every node
./scripts/verify-node-install.sh node-1     # one node
```

The two flags have to show up on the live kubelet process, not just in the drop-in file:

```text
--image-credential-provider-bin-dir=/usr/local/bin/credential-providers
--image-credential-provider-config=/etc/kubernetes/credential-providers/config.yaml
```

If the drop-in exists but the flags are missing from the process, the node's kubelet unit does not reference `$KUBELET_EXTRA_ARGS`. That is a distribution difference, not a bug here: see the [AKS](../aks/), [RKE2](../rke2/), or [MicroK8s](../microk8s/) notes for what to do instead.

## Then Pull Something

The chart already created the node audience RBAC (`nodeAudienceRbac.create` defaults to true), so there is nothing to apply for that. [`rbac-audience.yaml`](../rbac-audience.yaml) is the standalone version, for when you install the binary without the chart.

Point [`pod-example.yaml`](../pod-example.yaml) at an image that exists in your Harbor, then:

```bash
kubectl apply -f examples/kubernetes/pod-example.yaml
kubectl get pod httpd -w
```

## Custom Paths

If `/usr/local` is read-only or noexec on your node image, switch to `profile=custom` and point at a directory that is neither:

```bash
--set profile=custom \
--set credentialProvider.binDir=/opt/credential-providers \
--set credentialProvider.configPath=/etc/kubernetes/credential-providers/config.yaml
```
