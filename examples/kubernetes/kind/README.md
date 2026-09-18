# kind

For local testing. kind nodes are containers running systemd, so the chart's `kind` profile works, with one wrinkle worth knowing before you start debugging the wrong thing.

## Install

```bash
helm upgrade --install credential-provider-harbor \
  oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system \
  --set profile=kind \
  --set registry.host=harbor.example.com \
  --set registry.audience=harbor.example.com

kubectl rollout status daemonset/credential-provider-harbor -n kube-system
```

Files land at `/var/lib/kubelet/credential-provider/` and `/var/lib/kubelet/credential-provider-config.yaml` inside the node container.

## The Wrinkle

Installing the binary and the config is not enough. kubelet has to actually be running with the two flags. On kind that does not always follow from writing the drop-in, because the node's kubelet unit may not expand `$KUBELET_EXTRA_ARGS`.

Check the live process, not the file:

```bash
docker exec <kind-node> sh -c 'tr "\0" " " < /proc/$(pidof kubelet)/cmdline; printf "\n"'
```

You want:

```text
--image-credential-provider-bin-dir=/var/lib/kubelet/credential-provider
--image-credential-provider-config=/var/lib/kubelet/credential-provider-config.yaml
```

If they are missing, reinstall with the kind-only override:

```bash
helm upgrade --install credential-provider-harbor \
  oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system \
  --set profile=kind \
  --set registry.host=harbor.example.com \
  --set registry.audience=harbor.example.com \
  --set kubelet.forceExecStartOverride=true
```

That writes the equivalent of [`../kind-kubelet-systemd-dropin.conf`](../kind-kubelet-systemd-dropin.conf): it clears `ExecStart` and re-declares it with the flags spelled out. It is off by default because rewriting `ExecStart` is heavier than setting an environment variable, and it breaks if kind changes the kubelet command line. Use it only when the flags are actually missing.

## Then Pull Something

```bash
kubectl apply -f examples/kubernetes/rbac-audience.yaml
kubectl apply -f examples/kubernetes/pod-example.yaml
```

`no basic auth credentials` on the pod almost always means kubelet never called the provider. Check the live command line before looking anywhere else.

## Audience

The API server has to be willing to issue tokens for your Harbor audience. On kind, pass it at cluster creation:

```yaml
# kind-config.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
kubeadmConfigPatches:
  - |
    kind: ClusterConfiguration
    apiServer:
      extraArgs:
        api-audiences: "https://kubernetes.default.svc.cluster.local,harbor.example.com"
```

For Harbor to validate tokens from a local kind cluster, the cluster issuer has to be reachable from Harbor, or the Federated IDP has to be configured with inline JWKS. The [Talos example](../../talos/) has the offline JWKS recipe, which applies here too.
