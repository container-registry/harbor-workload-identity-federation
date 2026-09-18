# k3s

k3s has a profile, so this is a one-liner. kubelet is embedded in the k3s process, and its arguments come from `/etc/rancher/k3s/config.yaml.d/`, which the installer writes for you.

## Install

```bash
helm upgrade --install credential-provider-harbor \
  oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system \
  --set profile=k3s \
  --set registry.host=harbor.example.com \
  --set registry.audience=harbor.example.com

kubectl rollout status daemonset/credential-provider-harbor -n kube-system
```

## What Gets Written

| Path on the node | Contents |
|------------------|----------|
| `/var/lib/rancher/credentialprovider/bin/credential-provider-harbor` | The binary |
| `/var/lib/rancher/credentialprovider/config.yaml` | The kubelet `CredentialProviderConfig` |
| `/etc/rancher/k3s/config.yaml.d/99-credential-provider-harbor.yaml` | The two `image-credential-provider-*` settings, as in [`../k3s-config.yaml`](../k3s-config.yaml) |

The installer then restarts the k3s service. It detects whether the node runs `k3s` or `k3s-agent`, so a mixed server and agent cluster needs no extra values.

## Audience

The API server must be willing to mint tokens for your Harbor audience. Add it to the k3s server arguments:

```yaml
# /etc/rancher/k3s/config.yaml
kube-apiserver-arg:
  - "api-audiences=https://kubernetes.default.svc.cluster.local,harbor.example.com"
```

Then apply the node audience RBAC, which the chart creates by default, or by hand from [`../rbac-audience.yaml`](../rbac-audience.yaml).

## Check It Took

```bash
./scripts/verify-node-install.sh
```

Do not expect to see the flags in `ps` output. They come from `/etc/rancher/k3s/config.yaml.d/`, and k3s passes them to its embedded kubelet internally rather than on its own command line. Check the drop-in and the k3s log instead:

```bash
cat /etc/rancher/k3s/config.yaml.d/99-credential-provider-harbor.yaml
journalctl -u k3s -u k3s-agent | grep -i credential-provider
```

Server nodes run `k3s.service` and agent nodes run `k3s-agent.service`, so the log check names both units; naming only `k3s` returns nothing on an agent. The drop-in `cat` is the half that works the same everywhere.
