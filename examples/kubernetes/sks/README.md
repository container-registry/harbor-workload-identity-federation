# Exoscale SKS

One `helm install`. The `sks` profile rewrites the one line SKS uses to start kubelet, because nothing else on an SKS node reaches kubelet's command line.

## Why SKS Needs Its Own Profile

An SKS node starts kubelet from a drop-in Exoscale writes, `/etc/systemd/system/kubelet.service.d/sks.conf`:

```ini
[Service]
ExecStart=
ExecStart=/usr/local/bin/kubelet \
	--config=/var/lib/kubelet/config.yaml \
	--cloud-provider="external" \
	--bootstrap-kubeconfig="/etc/kubernetes/kubelet/bootstrap-kubeconfig" \
	--kubeconfig="/etc/kubernetes/kubelet/kubeconfig" \
	--node-labels="node.exoscale.net/nodepool-id=<nodepool-id>" \
	-v=1
```

Every flag is spelled out and no variable is expanded, so the `KUBELET_EXTRA_ARGS` drop-in the `generic` profile writes is read and has no effect, and so would an `EnvironmentFile`. `/var/lib/kubelet/config.yaml` is a `KubeletConfiguration`, but it has no field for `--image-credential-provider-bin-dir` or `--image-credential-provider-config`: on kubelet 1.37 both are still command-line flags only.

So the `sks` profile reads the last `ExecStart` in `sks.conf` and writes `/etc/systemd/system/kubelet.service.d/zz-harbor-credential-provider.conf`, which resets `ExecStart` and repeats the SKS command line with the two flags appended. It never edits `sks.conf`.

The file name matters. systemd applies a unit's drop-ins in file name order, and `99-harbor-credential-provider.conf`, the name the `generic` profile uses, sorts before `sks.conf`: `sks.conf` would reset its `ExecStart` and kubelet would start without the flags. The installer refuses a `kubelet.systemdDropInPath` whose file name does not sort after `sks.conf`.

The command line is copied on every install rather than once, so a node whose `sks.conf` changed is brought back in line the next time the installer runs on it. If `sks.conf` already passes both flags with the profile's paths, the installer writes nothing and does not restart kubelet.

## Install

```bash
helm upgrade --install harbor-credential-provider \
  oci://8gears.container-registry.com/8gcr/harbor-credential-provider \
  --namespace kube-system \
  -f examples/kubernetes/sks/values.yaml \
  --set registry.host=harbor.example.com \
  --set registry.audience=harbor.example.com

kubectl rollout status daemonset/harbor-credential-provider -n kube-system
```

The Harbor side is the same as on any other cluster: see [Harbor Setup](../README.md#harbor-setup).

## Confirm

```bash
./scripts/verify-node-install.sh
```

The two flags have to show up on the live kubelet process:

```text
--image-credential-provider-bin-dir=/usr/local/bin/credential-providers
--image-credential-provider-config=/etc/kubernetes/credential-providers/config.yaml
```

On the node, `systemctl cat kubelet` lists `zz-harbor-credential-provider.conf` after `sks.conf`.

## Node Pool Scale-Out and Replacement

A node the pool adds or replaces comes up with a stock `sks.conf` and no drop-in of ours. The DaemonSet runs on it as it joins, writes the drop-in and restarts kubelet. A pod scheduled in the window before that retries the pull.

## If the Install Fails

The installer stops with an error, and changes nothing on the node's kubelet, when `sks.conf` is missing, has no `ExecStart` in `[Service]`, or holds a command line it cannot parse safely: an unterminated quote, or more than one command. It does not fall back to the `generic` drop-in, which would report success on a node where kubelet never sees the flags. If SKS moves its drop-in, point `kubelet.sksDropInPath` at the new one.

## Uninstall

```bash
helm uninstall harbor-credential-provider -n kube-system
```

Replacing the nodes in the pool is the clean way out: replacements come up with a stock kubelet.

To clean a node in place, the only kubelet configuration to undo is the drop-in. Removing it brings back the `ExecStart` from `sks.conf`.

Entry out, restart, then delete. Kubelet reads the config at startup, so it keeps exec'ing the provider until a restart; delete the binary first and pulls from Harbor fail against a missing file.

```bash
# 1. Take the entry out of the providers list.
sudo "${EDITOR:-vi}" /etc/kubernetes/credential-providers/config.yaml

# 2. Remove the drop-in, which puts the stock SKS ExecStart back.
sudo rm -f /etc/systemd/system/kubelet.service.d/zz-harbor-credential-provider.conf
sudo systemctl daemon-reload

# 3. Restart, then delete.
sudo systemctl restart kubelet
sudo rm -f /usr/local/bin/credential-providers/harbor-credential-provider
sudo rm -f /var/lib/harbor-credential-provider/install-marker
```

Delete `config.yaml` outright where the provider is its only entry.

Reference: [Uninstalling](../../../deploy/helm/harbor-credential-provider/README.md#uninstalling).
