# k3s

k3s has a profile, so this is a one-liner. kubelet is embedded in the k3s process, and its arguments come from `/etc/rancher/k3s/config.yaml.d/`, which the installer writes for you.

## Install

```bash
helm upgrade --install harbor-credential-provider \
  oci://8gears.container-registry.com/8gcr/harbor-credential-provider \
  --namespace kube-system \
  --set profile=k3s \
  --set registry.host=harbor.example.com \
  --set registry.audience=harbor.example.com

kubectl rollout status daemonset/harbor-credential-provider -n kube-system
```

## What Gets Written

| Path on the node | Contents |
|------------------|----------|
| `/var/lib/rancher/credentialprovider/bin/harbor-credential-provider` | The binary |
| `/var/lib/rancher/credentialprovider/config.yaml` | The kubelet `CredentialProviderConfig` |
| `/etc/rancher/k3s/config.yaml.d/99-harbor-credential-provider.yaml` | The two `image-credential-provider-*` settings, as in [`../k3s-config.yaml`](../k3s-config.yaml) |

The installer then restarts the k3s service. It detects whether the node runs `k3s` or `k3s-agent`, so a mixed server and agent cluster needs no extra values.

## Harbor Side

Federated robot accounts are off until an admin enables the feature gate, and the Trusted Issuer needs its `audience` field. The commands are under [Harbor Setup](../README.md#harbor-setup), which uses the offline JWKS variant for k3s.

A Harbor on plain HTTP also needs `/etc/rancher/k3s/registries.yaml` on every node, or the pull fails. Restart the unit after writing it:

```yaml
mirrors:
  "<host:port>":
    endpoint:
      - "http://<host:port>"
```

A Harbor on TLS needs no such file.

## Audience

The node has to be allowed to ask for a token with your Harbor audience. That permission is the node audience RBAC, which the chart creates by default, or apply it by hand from [`../rbac-audience.yaml`](../rbac-audience.yaml).

Nothing goes in `kube-apiserver-arg` for this. `--api-audiences` lists the audiences the API server accepts on tokens presented to it, and the same list becomes the audience of every token requested without one. It plays no part in kubelet's request for the registry audience. Putting the registry there makes Harbor tokens valid cluster credentials and stops keyless Flux pulls; see [The Three Things That Have To Line Up](../README.md#the-three-things-that-have-to-line-up).

## Check It Took

```bash
./scripts/verify-node-install.sh
```

Do not expect to see the flags in `ps` output. They come from `/etc/rancher/k3s/config.yaml.d/`, and k3s passes them to its embedded kubelet internally rather than on its own command line. Check the drop-in and the k3s log instead:

```bash
cat /etc/rancher/k3s/config.yaml.d/99-harbor-credential-provider.yaml
journalctl -u k3s -u k3s-agent | grep -i credential-provider
```

Server nodes run `k3s.service` and agent nodes run `k3s-agent.service`, so the log check names both units; naming only `k3s` returns nothing on an agent. The drop-in `cat` is the half that works the same everywhere.

## Uninstall

```bash
helm uninstall harbor-credential-provider -n kube-system
```

The node half is a k3s config drop-in, not a systemd one.

Entry out, restart, then delete. Kubelet reads the config at startup, so it keeps exec'ing the provider until a restart; delete the binary first and pulls from Harbor fail against a missing file.

```bash
# 1. Remove the drop-in.
sudo rm -f /etc/rancher/k3s/config.yaml.d/99-harbor-credential-provider.yaml

# 2. Take the entry out of the providers list.
sudo "${EDITOR:-vi}" /var/lib/rancher/credentialprovider/config.yaml

# 3. Restart the unit this node runs, then delete.
sudo systemctl restart k3s        # k3s-agent on an agent node
sudo rm -f /var/lib/rancher/credentialprovider/bin/harbor-credential-provider
sudo rm -f /var/lib/harbor-credential-provider/install-marker
```

The provider directory outlives the binary. Once nothing else uses it, remove it too:

```bash
sudo rm -rf /var/lib/rancher/credentialprovider
```

Delete `config.yaml` outright where the provider is its only entry.

Reference: [Uninstalling](../../../deploy/helm/harbor-credential-provider/README.md#uninstalling). A node upgraded from the old `credential-provider-harbor` naming has legacy-named leftovers as well; [Nodes upgraded from the old name](../../../deploy/helm/harbor-credential-provider/README.md#nodes-upgraded-from-the-old-name) lists them.
