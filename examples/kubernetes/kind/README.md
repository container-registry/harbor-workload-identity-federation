# kind

For local testing. kind nodes are containers running systemd, so the chart's `kind` profile works, with one wrinkle worth knowing before you start debugging the wrong thing.

## Install

```bash
helm upgrade --install harbor-credential-provider \
  oci://8gears.container-registry.com/8gcr/harbor-credential-provider \
  --namespace kube-system \
  --set profile=kind \
  --set registry.host=harbor.example.com \
  --set registry.audience=harbor.example.com

kubectl rollout status daemonset/harbor-credential-provider -n kube-system
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
helm upgrade --install harbor-credential-provider \
  oci://8gears.container-registry.com/8gcr/harbor-credential-provider \
  --namespace kube-system \
  --set profile=kind \
  --set registry.host=harbor.example.com \
  --set registry.audience=harbor.example.com \
  --set kubelet.forceExecStartOverride=true
```

That writes the equivalent of [`../kind-kubelet-systemd-dropin.conf`](../kind-kubelet-systemd-dropin.conf): it clears `ExecStart` and re-declares it with the flags spelled out. It is off by default because rewriting `ExecStart` is heavier than setting an environment variable, and it breaks if kind changes the kubelet command line. Use it only when the flags are actually missing.

## Harbor Side

Federated robot accounts are off until an admin enables the feature gate, and the Trusted Issuer needs its `audience` field. The commands are under [Harbor Setup](../README.md#harbor-setup). A Harbor on plain HTTP also needs a `hosts.toml` in every node container, described under [Plain HTTP and non-standard ports](../README.md#plain-http-and-non-standard-ports). A Harbor on TLS needs none.

## Then Pull Something

The chart already created the node audience RBAC, so there is nothing to apply for that. Point [`pod-example.yaml`](../pod-example.yaml) at an image that exists in your Harbor, then:

```bash
kubectl apply -f examples/kubernetes/pod-example.yaml
```

`no basic auth credentials` on the pod almost always means kubelet never called the provider. Check the live command line before looking anywhere else.

## Audience

kind needs nothing passed at cluster creation for this. kubelet's request for a Harbor token is authorized by the node audience RBAC the chart creates. The API server's `--api-audiences` lists the audiences it accepts on tokens presented to it, and doubles as the default audience of tokens issued without one. It plays no part in kubelet's request for the registry audience. Your registry does not belong there. Putting the registry there makes Harbor tokens valid cluster credentials and stops keyless Flux pulls; see [The Three Things That Have To Line Up](../README.md#the-three-things-that-have-to-line-up).

For Harbor to validate tokens from a local kind cluster, the cluster issuer has to be reachable from Harbor, or the Trusted Issuer has to be configured with inline JWKS. The [Talos example](../../talos/) has the offline JWKS recipe, which applies here too.

## Cleanup

The cluster is disposable, so deleting it is the removal:

```bash
kind delete cluster --name credential-provider-test
```

Keeping the cluster: `helm uninstall harbor-credential-provider -n kube-system`, then per node container take the entry out of `/var/lib/kubelet/credential-provider-config.yaml`, delete `/etc/systemd/system/kubelet.service.d/99-harbor-credential-provider.conf`, restart kubelet, then delete `/var/lib/kubelet/credential-provider/harbor-credential-provider`. Order matters, for the reason [Uninstalling](../../../deploy/helm/harbor-credential-provider/README.md#uninstalling) gives.
