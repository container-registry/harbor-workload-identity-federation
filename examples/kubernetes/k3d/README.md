# k3d

k3d runs k3s in Docker. The chart's `k3d` profile is the same as [`k3s`](../k3s/); this page covers the parts specific to running it on a laptop.

## Cluster

[`../k3d-config.yaml`](../k3d-config.yaml) creates a cluster with mount points for the provider files.

Drop the `volumes` block unless you have a reason to keep it, and let the chart install the binary. If you do keep it, build the binary first and point the mount at it, because Docker silently creates an empty directory for a bind-mount source that does not exist, and the provider then fails with nothing useful in the logs:

```bash
task build-all   # writes bin/credential-provider-harbor-linux-amd64 and -linux-arm64
```

Mount the artifact that matches the k3d node's architecture, which is your machine's: `linux-arm64` on an Apple Silicon or other arm64 laptop, `linux-amd64` on an x86_64 one. A node given the wrong one cannot execute it, and the pull fails with `exec format error`.

```bash
k3d cluster create --config examples/kubernetes/k3d-config.yaml
k3d kubeconfig merge credential-provider-test --kubeconfig-switch-context
```

The cluster needs nothing else at creation time for the audience. What lets kubelet ask for a Harbor token is the node audience RBAC, which the chart creates.

## Install

```bash
helm upgrade --install credential-provider-harbor \
  oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system \
  --set profile=k3d \
  --set registry.host=harbor.example.com \
  --set registry.audience=harbor.example.com

kubectl rollout status daemonset/credential-provider-harbor -n kube-system
```

## Restart the Cluster

A k3d node is a container running k3s as PID 1, with no init system behind it. The installer writes the binary, the config and the k3s config drop-in, then says it cannot restart anything from in there:

```text
[WARN] This node has no systemctl, so kubelet cannot be restarted from inside
the cluster. The credential provider binary, config and drop-in are all
written. Restart the cluster so k3s rereads its config: k3d cluster stop <name>
&& k3d cluster start <name>.
```

k3s reads `config.yaml.d` at startup and nowhere else, so the flags take effect on the next start:

```bash
k3d cluster stop credential-provider-test
k3d cluster start credential-provider-test --wait
```

The install is not finished until you have done that. Skipping it leaves the node with the files on disk and a k3s that has never read them, which fails a pull with `no basic auth credentials` and looks like a broken provider.

## Test

```bash
kubectl apply -f examples/kubernetes/rbac-audience.yaml
kubectl apply -f examples/kubernetes/pod-example.yaml
kubectl get pod httpd -w
```

## Harbor Has To Reach the Issuer

A k3d cluster on your machine is not reachable from a hosted Harbor, so online JWKS validation will not work. Configure the Trusted Issuer with inline JWKS instead:

```bash
kubectl get --raw "$(kubectl get --raw /.well-known/openid-configuration | jq -r .jwks_uri)"
```

Paste that into the Trusted Issuer. The [Talos example](../../talos/) walks through the same offline setup in more detail.

## Cleanup

```bash
k3d cluster delete credential-provider-test
```
