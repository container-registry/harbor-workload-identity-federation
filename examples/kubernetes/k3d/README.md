# k3d

k3d runs k3s in Docker. The chart's `k3d` profile writes the same files as [`k3s`](../k3s/). The difference is the restart: a k3d node is a container running k3s as PID 1 with no init system, so nothing inside it can restart k3s, and k3s reads its `config.yaml.d` drop-in only when it starts. The `k3d` profile therefore writes the files and leaves the restart to you, from the machine running k3d. This page covers that and the other parts specific to running it on a laptop.

## Cluster

[`../k3d-config.yaml`](../k3d-config.yaml) creates a cluster with mount points for the provider files.

Drop the `volumes` block unless you have a reason to keep it, and let the chart install the binary. If you do keep it, build the binary first and point the mount at it, because Docker silently creates an empty directory for a bind-mount source that does not exist, and the provider then fails with nothing useful in the logs:

```bash
task build-all   # writes bin/harbor-credential-provider-linux-amd64 and -linux-arm64
```

Mount the artifact that matches the k3d node's architecture, which is your machine's: `linux-arm64` on an Apple Silicon or other arm64 laptop, `linux-amd64` on an x86_64 one. A node given the wrong one cannot execute it, and the pull fails with `exec format error`.

```bash
k3d cluster create --config examples/kubernetes/k3d-config.yaml
k3d kubeconfig merge credential-provider-test --kubeconfig-switch-context
```

The cluster needs nothing else at creation time for the audience. What lets kubelet ask for a Harbor token is the node audience RBAC, which the chart creates.

## Install

```bash
helm upgrade --install harbor-credential-provider \
  oci://8gears.container-registry.com/8gcr/harbor-credential-provider \
  --namespace kube-system \
  --set profile=k3d \
  --set kubelet.restart=false \
  --set registry.host=harbor.example.com \
  --set registry.audience=harbor.example.com

kubectl rollout status daemonset/harbor-credential-provider -n kube-system
```

`kubelet.restart=false` is already the `k3d` default. It is spelled out because chart 0.4.0 and earlier default it to `true` for `k3d` as well, and there the installer tries `systemctl` on a node that has none and crash-loops ([#32](https://github.com/container-registry/harbor-workload-identity-federation/issues/32)).

The rollout completes once every node has the files. The installer log names the step left:

```bash
kubectl logs -n kube-system -l app.kubernetes.io/name=harbor-credential-provider | grep 'restart disabled'
```

Restart the cluster so k3s starts against the new config, then wait for the nodes:

```bash
k3d cluster stop credential-provider-test && k3d cluster start credential-provider-test
kubectl wait --for=condition=Ready nodes --all --timeout=5m
```

`docker restart k3d-credential-provider-test-server-0` does the same for one node. Restarting again is only needed after an upgrade that changes the files, and the installer log says so each time.

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

The cluster is disposable, so deleting it is the removal. The nodes were containers, so nothing is left behind:

```bash
k3d cluster delete credential-provider-test
```

Keeping the cluster: the k3s steps apply unchanged, and [Uninstalling](../../../deploy/helm/harbor-credential-provider/README.md#uninstalling) has the paths and the order.
