# Kubernetes Examples

These examples show the resources kubelet needs to pull Harbor images with `credential-provider-harbor` and service account tokens.

## kind

For local kind clusters, make sure the kubelet process is actually started with the credential provider flags. Installing the binary and `CredentialProviderConfig` file is not enough.

Use the Helm chart with the kind profile:

```bash
helm upgrade --install credential-provider-harbor deploy/helm/credential-provider-harbor \
  --namespace kube-system \
  --create-namespace \
  --set profile=kind \
  --set registry.host=harbor.example.com \
  --set registry.audience=https://harbor.example.com
```

Verify the live kubelet command line inside the kind node includes both flags:

```bash
docker exec <kind-node> sh -c 'tr "\0" " " < /proc/$(pidof kubelet)/cmdline; printf "\n"'
```

Expected flags:

```text
--image-credential-provider-bin-dir=/var/lib/kubelet/credential-provider
--image-credential-provider-config=/var/lib/kubelet/credential-provider-config.yaml
```

If those flags are missing after install and restart, enable the kind-only forced `ExecStart` override:

```bash
helm upgrade --install credential-provider-harbor deploy/helm/credential-provider-harbor \
  --namespace kube-system \
  --create-namespace \
  --set profile=kind \
  --set registry.host=harbor.example.com \
  --set registry.audience=https://harbor.example.com \
  --set kubelet.forceExecStartOverride=true
```

This writes a drop-in equivalent to [`kind-kubelet-systemd-dropin.conf`](kind-kubelet-systemd-dropin.conf). It resets kubelet `ExecStart` and appends the credential-provider flags directly. Use it only when kind does not propagate `KUBELET_EXTRA_ARGS` into the live kubelet process.

Then apply the audience RBAC and deploy a pod without `imagePullSecrets`:

```bash
kubectl apply -f examples/kubernetes/rbac-audience.yaml
kubectl apply -f examples/kubernetes/pod-example.yaml
```

If the pod fails with `no basic auth credentials`, check the live kubelet command line first. That error usually means kubelet did not invoke the credential provider.
