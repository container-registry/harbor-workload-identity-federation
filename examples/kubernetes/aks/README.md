# Azure Kubernetes Service

One `helm install`. The `aks` profile handles the part that used to be a manual step on every node.

## Why AKS Needs Its Own Profile

The `generic` profile writes a systemd drop-in that sets `KUBELET_EXTRA_ARGS`. That works on kubeadm nodes because the kubeadm unit expands it. The AKS kubelet unit does not: it builds its command line from `$KUBELET_FLAGS`, which comes from `/etc/default/kubelet`. A drop-in setting `KUBELET_EXTRA_ARGS` on an AKS node is written, is valid, and has no effect.

AKS also has no supported API for adding arbitrary kubelet flags. [Custom node configuration](https://learn.microsoft.com/en-us/azure/aks/custom-node-configuration) covers a fixed list of settings, and the two image credential provider flags are not on it.

So the `aks` profile edits `/etc/default/kubelet` directly. It rewrites the active `KUBELET_FLAGS` assignment to carry the two flags, keeps every other flag in place, and backs the file up first. Running it again with different paths replaces its own earlier values rather than appending a second copy.

## Audience

kubelet asks the API server for a token whose audience is `registry.audience`, and on AKS as anywhere else that request is authorized by the node audience RBAC the chart creates. AKS does not expose `--api-audiences`, and does not need to: that flag governs the tokens the API server accepts, not the ones it issues. Any agreed string works as the audience, as long as the Harbor Federated IDP is configured with the same one.

Harbor also has to reach the cluster's signing keys to validate those tokens, so enable the [OIDC issuer](https://learn.microsoft.com/en-us/azure/aks/use-oidc-issuer) on the cluster and point the Federated IDP at that URL.

## Install

```bash
helm upgrade --install credential-provider-harbor \
  oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system \
  -f examples/kubernetes/aks/values.yaml \
  --set registry.host=harbor.example.com \
  --set registry.audience=harbor.example.com

kubectl rollout status daemonset/credential-provider-harbor -n kube-system
```

## Confirm

```bash
./scripts/verify-node-install.sh
```

## Node Image Upgrades and Scale-Out

A new AKS node comes up with a stock `/etc/default/kubelet`. Because the DaemonSet runs on every node, it patches that file and restarts kubelet as the node joins, which is what the old per-node script could not do. The window between the node going Ready and the installer finishing is real but short; a pod that lands in it retries the pull.

Node image upgrades behave the same way: the upgraded node is a new node, and the DaemonSet treats it as one.

## If the Install Fails

The installer stops with an error when `/etc/default/kubelet` has no active `KUBELET_FLAGS` assignment, rather than reporting success on a node it did not change. A node image that does not use `KUBELET_FLAGS` is not one this profile can wire, so set `kubelet.configure=false` and add the flags however that image expects them.
