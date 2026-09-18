# RKE2

RKE2 runs kubelet inside the `rke2-agent` process rather than as its own systemd unit. There is no `kubelet.service` to drop a file into, so the chart cannot wire kubelet here. It can still place the binary and the config; you add two lines to the RKE2 config.

This is the same shape as k3s, but k3s has a profile and RKE2 does not yet. Until it does, use `profile=custom`.


## The API Server Has To Accept the Audience

kubelet asks the API server for a token whose audience is `registry.audience`. The API server only mints audiences listed in `--api-audiences`, so if yours is not there the request is refused before the provider is called at all, and no amount of correct node setup fixes it.

On RKE2 this is a server-node setting:

```yaml
# /etc/rancher/rke2/config.yaml.d/99-harbor-audience.yaml, on every server node
kube-apiserver-arg:
  - "api-audiences=https://kubernetes.default.svc.cluster.local,harbor.example.com"
```

Restart `rke2-server` after adding it. Keep the default cluster audience in the list; dropping it breaks in-cluster service account tokens.

## Order Matters

The config drop-in has to be on the node before the agent restarts, otherwise the restart the installer performs is wasted and you need a second one.

**1. On every node**, drop in the kubelet arguments:

```bash
sudo mkdir -p /etc/rancher/rke2/config.yaml.d
sudo cp examples/kubernetes/rke2/config.yaml \
  /etc/rancher/rke2/config.yaml.d/99-credential-provider-harbor.yaml
```

If you manage RKE2 config with a single `/etc/rancher/rke2/config.yaml`, merge the `kubelet-arg` entries into it instead. RKE2 merges `config.yaml.d` entries for list keys, but a `kubelet-arg` list in the main file and in a drop-in do not always combine the way you expect; check `journalctl -u rke2-agent` after the restart.

**2. Install the chart:**

```bash
helm upgrade --install credential-provider-harbor \
  oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system \
  -f examples/kubernetes/rke2/values.yaml \
  --set registry.host=harbor.example.com \
  --set registry.audience=harbor.example.com

kubectl rollout status daemonset/credential-provider-harbor -n kube-system
```

On server nodes, override the service name:

```bash
--set kubelet.serviceName=rke2-server
```

A cluster with both needs two releases with different names and complementary selectors, because one restarts `rke2-server` and the other `rke2-agent`:

```bash
# server nodes
helm upgrade --install credential-provider-harbor-server ... \
  --set kubelet.serviceName=rke2-server \
  --set nodeSelector."node-role\.kubernetes\.io/control-plane"=true

# worker nodes
helm upgrade --install credential-provider-harbor-agent ... \
  --set kubelet.serviceName=rke2-agent \
  --set 'affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].key=node-role.kubernetes.io/control-plane' \
  --set 'affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].operator=DoesNotExist'
```

A `nodeSelector` cannot express "not a control plane node", which is why the worker release uses `affinity` instead.

## Check It Took

```bash
./scripts/verify-node-install.sh
```

The flags should appear on the `kubelet` arguments inside the rke2 process tree:

```bash
ps -ef | grep -o 'image-credential-provider[^ ]*'
```

## Contributing a Profile

An `rke2` profile would remove the manual step: it needs a `configureRKE2` branch in the installer that writes `/etc/rancher/rke2/config.yaml.d/99-credential-provider-harbor.yaml`, the same way `configureK3s` already writes the k3s drop-in, plus the path defaults. See [CONTRIBUTING.md](../../../CONTRIBUTING.md).
