# RKE2

One `helm install` on the node side, plus an API server setting on the server nodes.

RKE2 runs kubelet inside the `rke2-agent` process rather than as its own systemd unit, so there is no `kubelet.service` to drop a file into. The `rke2` profile writes `/etc/rancher/rke2/config.yaml.d/99-credential-provider-harbor.yaml` instead, which is where RKE2 reads `kubelet-arg` from, and restarts the supervisor so it takes effect. [`config.yaml`](config.yaml) shows the file it writes.

## The API Server Has To Accept the Audience

kubelet asks the API server for a token whose audience is `registry.audience`. The API server only mints audiences listed in `--api-audiences`, so if yours is not there the request is refused before the provider is called at all, and no amount of correct node setup fixes it.

On RKE2 this is a server-node setting, and the chart does not touch it:

```yaml
# /etc/rancher/rke2/config.yaml.d/99-harbor-audience.yaml, on every server node
kube-apiserver-arg:
  - "api-audiences=https://kubernetes.default.svc.cluster.local,harbor.example.com"
```

Restart `rke2-server` after adding it. Keep the default cluster audience in the list; dropping it breaks in-cluster service account tokens.

## Install

```bash
helm upgrade --install credential-provider-harbor \
  oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system \
  -f examples/kubernetes/rke2/values.yaml \
  --set registry.host=harbor.example.com \
  --set registry.audience=harbor.example.com

kubectl rollout status daemonset/credential-provider-harbor -n kube-system
```

The installer restarts `rke2-agent` on worker nodes and `rke2-server` on server nodes, picking whichever unit is installed. Override it with `--set kubelet.serviceName=...` if your nodes name it differently.

## A Node That Manages `config.yaml` by Hand

If you keep all RKE2 settings in a single `/etc/rancher/rke2/config.yaml` rather than in `config.yaml.d`, the drop-in still applies: RKE2 reads both. What it does not do is merge two `kubelet-arg` lists the way you might expect, so a `kubelet-arg` list in the main file can shadow the drop-in. Check `journalctl -u rke2-agent` after the restart, and if the flags are missing, merge the two entries from [`config.yaml`](config.yaml) into your own file and install with `--set kubelet.configure=false`.

## Check It Took

```bash
./scripts/verify-node-install.sh
```

The flags do not appear on any command line here: the supervisor passes them to the embedded kubelet from the config file. The verify script knows that and reads the RKE2 config drop-ins. To check by hand:

```bash
grep -r image-credential-provider /etc/rancher/rke2/
```
