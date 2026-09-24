# Kubernetes

Install `credential-provider-harbor` on your nodes so pods pull Harbor images with service account tokens instead of `imagePullSecrets`.

Pick your distribution. The pages differ because distributions disagree about where a kubelet's arguments come from, and that is the part that decides whether any of this works.

## Distributions

| Distribution | How it goes | Page |
|--------------|-------------|------|
| kubeadm and other systemd nodes | One `helm install` | [`kubeadm/`](kubeadm/) |
| Amazon EKS | One `helm install`; the AMI already sets the kubelet flags | [`eks/`](eks/) |
| k3s | One `helm install` | [`k3s/`](k3s/) |
| k3d | One `helm install`, plus an API server audience at cluster creation | [`k3d/`](k3d/) |
| kind | One `helm install`, sometimes plus an `ExecStart` override | [`kind/`](kind/) |
| GKE Standard | One `helm install`, but nodes lose it on replacement | [`gke/`](gke/) |
| RKE2 | One `helm install`, plus an API server audience on the server nodes | [`rke2/`](rke2/) |
| MicroK8s | One `helm install`; a snap refresh undoes it | [`microk8s/`](microk8s/) |
| AKS | One `helm install` | [`aks/`](aks/) |
| Talos Linux | System extension, not the chart | [`../talos/`](../talos/) |
| OpenShift | Not supported; the page explains what it would take | [`openshift/`](openshift/) |
| GKE Autopilot | Not possible. No privileged host access, no kubelet control | — |

## The Three Things That Have To Line Up

Most failures are one of these, and they are easier to check than to debug.

**1. kubelet has the flags.** Installing the binary and the config is not enough. kubelet has to be running with `--image-credential-provider-bin-dir` and `--image-credential-provider-config`. If it is not, pulls fail with `no basic auth credentials` and the provider is never called at all.

```bash
./scripts/verify-node-install.sh
```

**2. The API server will issue the audience.** The audience your provider asks for has to be in the API server's `--api-audiences`, and the node audience RBAC has to grant `request-serviceaccounts-token-audience` on it to `system:nodes`. The chart creates that RBAC; [`rbac-audience.yaml`](rbac-audience.yaml) is the standalone version.

**3. Harbor expects the same audience string.** The audience is just an agreed identifier. It does not have to be a domain. What matters is that the same value appears in the kubelet config, the RBAC, and the Harbor Federated IDP. Using the registry hostname makes it obvious who the token is for, which is why the examples do that.

## Harbor Setup

Find the cluster's service account issuer:

```bash
kubectl get --raw /.well-known/openid-configuration | jq -r .issuer
```

Create a Harbor Federated IDP for that issuer. If the issuer is publicly reachable, as on EKS and GKE, Harbor can validate online. If it is not, as on a local or private cluster, fetch the keys and configure the IDP with inline JWKS:

```bash
kubectl get --raw "$(kubectl get --raw /.well-known/openid-configuration | jq -r .jwks_uri)"
```

Then create a federated robot account with pull permission and claim rules:

```text
iss == <cluster-service-account-issuer>
aud == <your-harbor-audience>
sub == system:serviceaccount:<namespace>:<service-account>
```

For the default service account in the default namespace, `sub` is `system:serviceaccount:default:default`.

The Harbor side is documented in full: [Federated Identity Provider for Workload Authentication](https://container-registry.com/docs/2.16/administration-manual/authentication-management/system-robot-accounts/federated-identity-provider-for-workload-authentication/) covers the IDP, the JWKS handling and the claim rules, and [Authenticating a Workload with Federated Identity](https://container-registry.com/docs/2.16/user-manual/images/authenticating-a-workload-with-federated-identity/) covers presenting the token.

## Shared Files

| File | Purpose |
|------|---------|
| [`k8s_credential_provider_config.yaml`](k8s_credential_provider_config.yaml) | A kubelet `CredentialProviderConfig`, for when you install by hand instead of with the chart |
| [`rbac-audience.yaml`](rbac-audience.yaml) | The node audience RBAC, standalone |
| [`pod-example.yaml`](pod-example.yaml) | A pod that pulls from Harbor with no `imagePullSecrets` |
| [`k3d-config.yaml`](k3d-config.yaml) | k3d cluster config with the audience allowed |
| [`k3s-config.yaml`](k3s-config.yaml) | The k3s config drop-in the installer writes |
| [`rke2/config.yaml`](rke2/config.yaml) | The RKE2 config drop-in the installer writes |
| [`kind-kubelet-systemd-dropin.conf`](kind-kubelet-systemd-dropin.conf) | The kind `ExecStart` override, for reference |

The first four use `harbor.example.com` as the registry host and the audience; replace it with yours. The last three are kubelet wiring only and contain no registry reference. `pod-example.yaml` also needs an image that exists in your Harbor, or the pod sits in `ImagePullBackOff` and looks like a credential provider failure.
