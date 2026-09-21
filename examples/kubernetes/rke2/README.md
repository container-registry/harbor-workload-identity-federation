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

The installer restarts `rke2-agent` on worker nodes and `rke2-server` on server nodes, picking whichever unit is installed. A node that has both units restarts both, since each runs a kubelet of its own and the one left alone would keep the flags it started with. Override it with `--set kubelet.serviceName=...` if your nodes name it differently.

## If You Already Set `kubelet-arg` Yourself

Read this one before installing. RKE2 reads `/etc/rancher/rke2/config.yaml` first and then `config.yaml.d/*.yaml` in alphabetical order, and for a repeated key the last file wins outright. It does not merge the two lists. So the installer's `99-credential-provider-harbor.yaml` sorts last, the credential provider flags do apply, and **any `kubelet-arg` entries of your own are replaced by them.**

If you have none, there is nothing to do.

If you have some, the thing that does not work is putting them in a drop-in that sorts after `99-`. Under the same rule, that file replaces the installer's list and takes the two provider flags out with it, silently. Two ways round it, and they are not equally good:

- **`kubelet-arg+:` in the later file.** It appends to the earlier value rather than replacing it, so the installer keeps owning the two provider entries. Prefer this one.
- **Both sets of entries listed together under `kubelet-arg:` in the later file.** This works, and it takes the provider flags out of the installer's hands. `99-credential-provider-harbor.yaml` is rewritten with the current `credentialProvider.binDir` on every run, but your later file still wins, so a reinstall with a different path leaves the cluster on the old one with no error. If you go this way, keep the copies in step.

The third option is to keep the installer out of the kubelet arguments entirely:

```bash
--set kubelet.configure=false
```

and merge the two entries from [`config.yaml`](config.yaml) into your own list by hand. The installer itself writes plain `kubelet-arg:` rather than `kubelet-arg+:`, because a key an older parser does not recognize fails silently, and a visible replacement is the better of the two failures.

Check what the supervisor actually got after the restart:

```bash
journalctl -u rke2-agent | grep -i kubelet-arg   # rke2-server on a server node
```

## Check It Took

```bash
./scripts/verify-node-install.sh
```

The flags do not appear on any command line here: the supervisor passes them to the embedded kubelet from the config file. The verify script knows that and reads the RKE2 config drop-ins. To check by hand:

```bash
grep -r image-credential-provider /etc/rancher/rke2/
```
