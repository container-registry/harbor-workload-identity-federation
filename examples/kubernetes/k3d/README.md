# k3d

k3d runs k3s in Docker. The chart's `k3d` profile is the same as [`k3s`](../k3s/); this page covers the parts specific to running it on a laptop.

## Cluster

[`../k3d-config.yaml`](../k3d-config.yaml) creates a cluster with the Harbor audience allowed on the API server, and with mount points for the provider files.

Drop the `volumes` block unless you have a reason to keep it, and let the chart install the binary. If you do keep it, build the binary first and point the mount at it, because Docker silently creates an empty directory for a bind-mount source that does not exist, and the provider then fails with nothing useful in the logs:

```bash
task build-all   # writes bin/credential-provider-harbor-linux-amd64 and -linux-arm64
```

Mount the artifact that matches the k3d node's architecture, which is your machine's: `linux-arm64` on an Apple Silicon or other arm64 laptop, `linux-amd64` on an x86_64 one. A node given the wrong one cannot execute it, and the pull fails with `exec format error`.

```bash
k3d cluster create --config examples/kubernetes/k3d-config.yaml
k3d kubeconfig merge credential-provider-test --kubeconfig-switch-context
```

The `api-audiences` argument in that config is the part that matters. Without your Harbor audience in that list, the API server refuses to issue the token and the provider never gets a chance to run.

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

## Test

```bash
kubectl apply -f examples/kubernetes/rbac-audience.yaml
kubectl apply -f examples/kubernetes/pod-example.yaml
kubectl get pod httpd -w
```

## Harbor Has To Reach the Issuer

A k3d cluster on your machine is not reachable from a hosted Harbor, so online JWKS validation will not work. Configure the Federated IDP with inline JWKS instead:

```bash
kubectl get --raw "$(kubectl get --raw /.well-known/openid-configuration | jq -r .jwks_uri)"
```

Paste that into the IDP. The [Talos example](../../talos/) walks through the same offline setup in more detail.

## Cleanup

```bash
k3d cluster delete credential-provider-test
```
