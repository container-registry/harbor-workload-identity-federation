# RKE2

One `helm install`, on server and worker nodes alike.

RKE2 runs kubelet inside the `rke2-agent` process rather than as its own systemd unit, so there is no `kubelet.service` to drop a file into. The `rke2` profile writes `/etc/rancher/rke2/config.yaml.d/99-credential-provider-harbor.yaml` instead, which is where RKE2 reads `kubelet-arg` from, and restarts the supervisor so it takes effect. [`config.yaml`](config.yaml) shows the file it writes.

## Audience

kubelet asks the API server for a token whose audience is `registry.audience`. What decides whether it gets one is the node audience RBAC, which the chart creates. There is no server-node setting to add, and in particular the audience does not go in `kube-apiserver-arg` as `--api-audiences`: that flag lists what the API server accepts on tokens presented to it, not what it issues.

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

The installer restarts the supervisor the node runs, asking systemd which of `rke2-server` and `rke2-agent` is active or enabled. It cannot go by the unit files: the tarball ships both and `install.sh` moves both into place, so `rke2-server.service` sits on a plain worker too, and restarting everything installed would start a control plane there. Override the choice with `--set kubelet.serviceName=...` if your nodes name the unit differently. A node where systemd runs neither fails the install with that instruction rather than guessing.

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
