# Kubernetes Examples

These examples show the resources kubelet needs to pull Harbor images with `credential-provider-harbor` and service account tokens.

## Harbor Setup

Create a Harbor Federated IDP for your Kubernetes service account issuer. For a cluster issuer of `https://kind.128.140.12.238.nip.io`, use:

```text
OpenID configuration URL: https://kind.128.140.12.238.nip.io/.well-known/openid-configuration
Issuer: https://kind.128.140.12.238.nip.io
Audience: <your-harbor-audience>
```

Create a federated robot account with pull permission and claim rules such as:

```text
iss == <cluster-service-account-issuer>
aud == <your-harbor-audience>
sub == system:serviceaccount:<namespace>:<service-account>
```

For the default service account in the default namespace:

```text
sub == system:serviceaccount:default:default
```

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

## Files

| File | Purpose |
|------|---------|
| [`k8s_credential_provider_config.yaml`](k8s_credential_provider_config.yaml) | Kubelet `CredentialProviderConfig` example. |
| [`rbac-audience.yaml`](rbac-audience.yaml) | RBAC for kubelets to request service account tokens with the Harbor audience. |
| [`pod-example.yaml`](pod-example.yaml) | Test pod that pulls from Harbor without `imagePullSecrets`. |
| [`k3d-config.yaml`](k3d-config.yaml) | k3d cluster config example for mounting provider files. |
| [`k3s-config.yaml`](k3s-config.yaml) | k3s config drop-in for credential-provider paths. |
| [`kind-kubelet-systemd-dropin.conf`](kind-kubelet-systemd-dropin.conf) | Optional kind fallback when `KUBELET_EXTRA_ARGS` is not propagated. |
