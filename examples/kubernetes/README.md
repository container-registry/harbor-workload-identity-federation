# Kubernetes

Install `harbor-credential-provider` on your nodes so pods pull Harbor images with service account tokens instead of `imagePullSecrets`.

Pick your distribution. The pages differ because distributions disagree about where a kubelet's arguments come from, and that is the part that decides whether any of this works.

## Distributions

| Distribution | How it goes | Page |
|--------------|-------------|------|
| kubeadm and other systemd nodes | One `helm install` | [`kubeadm/`](kubeadm/) |
| Amazon EKS | One `helm install`; the AMI already sets the kubelet flags | [`eks/`](eks/) |
| k3s | One `helm install` | [`k3s/`](k3s/) |
| k3d | One `helm install` | [`k3d/`](k3d/) |
| kind | One `helm install`, sometimes plus an `ExecStart` override | [`kind/`](kind/) |
| GKE Standard | One `helm install`, but nodes lose it on replacement | [`gke/`](gke/) |
| RKE2 | One `helm install` | [`rke2/`](rke2/) |
| MicroK8s | One `helm install`; a snap refresh undoes it | [`microk8s/`](microk8s/) |
| AKS | One `helm install` | [`aks/`](aks/) |
| Exoscale SKS | One `helm install` | [`sks/`](sks/) |
| Talos Linux | System extension, not the chart | [`../talos/`](../talos/) |
| OpenShift | Not supported; the page explains what it would take | [`openshift/`](openshift/) |
| GKE Autopilot | Not possible. No privileged host access, no kubelet control | — |

## The Three Things That Have To Line Up

Most failures are one of these, and they are easier to check than to debug.

**1. kubelet has the flags.** Installing the binary and the config is not enough. kubelet has to be running with `--image-credential-provider-bin-dir` and `--image-credential-provider-config`. If it is not, pulls fail with `no basic auth credentials` and the provider is never called at all.

```bash
./scripts/verify-node-install.sh
```

**2. The node may ask for the audience.** A node gets a token for your Harbor audience only if the node audience RBAC grants `request-serviceaccounts-token-audience` on it to `system:nodes`. The chart creates that RBAC; [`rbac-audience.yaml`](rbac-audience.yaml) is the standalone version. Nothing has to be added to the API server for it.

If you set this up from an earlier version of these pages, you will have your registry in the API server's `--api-audiences` as well. Take it out. That flag lists the audiences the API server accepts on tokens presented to it, so leaving your registry there means a token Harbor is given also works as a cluster credential.

**3. Harbor expects the same audience string.** The audience is just an agreed identifier. It does not have to be a domain. What matters is that the same value appears in the kubelet config, the RBAC, and the Harbor Trusted Issuer. Using the registry hostname makes it obvious who the token is for, which is why the examples do that.

## Harbor Setup

The flow below is the one tested against Harbor 2.15.12. Run it once per cluster with an admin account. The examples set these variables:

```bash
H=harbor.example.com                  # your Harbor host, with :port if it is not 443
AUD=harbor.example.com                # the audience; same value as registry.audience in the chart
A=(-u 'admin:<password>' -H "Content-Type: application/json")
```

Upgrading from a Harbor older than 2.15.12 with an existing Trusted Issuer? Check its audience first: [Upgrading Harbor to 2.15.12](../../deploy/helm/harbor-credential-provider/README.md#upgrading-harbor-to-21512).

**1. Turn on the feature gate.** Federated robot accounts are off by default, and every call below returns 403 until you enable them. This is a system configuration, so you do it once per Harbor:

```bash
curl -s "${A[@]}" -X PUT "https://$H/api/v2.0/configurations" \
  -d '{"enable_commercial_federated_robot_accounts":true,"enable_project_federated_robot_accounts":true}'
```

**2. Create the Trusted Issuer.** Find the cluster's service account issuer:

```bash
ISS=$(kubectl get --raw /.well-known/openid-configuration | jq -r .issuer)
echo "$ISS"
```

`audience` is a field on the Trusted Issuer, and Harbor refuses the issuer without it (`400 audience is required`). If the issuer is not reachable from Harbor, as on kind, k3d, k3s, RKE2 and Talos, use offline validation and pass the cluster's public keys inline:

```bash
JWKS=$(kubectl get --raw "$(kubectl get --raw /.well-known/openid-configuration | jq -r .jwks_uri)")
TI=$(jq -n --argjson k "$JWKS" --arg iss "$ISS" --arg a "$AUD" --arg n my-cluster \
  '{name:$n,issuer:$iss,offline_validation:true,jwks_keys:$k,audience:$a}' \
  | curl -s "${A[@]}" -X POST "https://$H/api/v2.0/trusted-issuers" -d @- | jq -r .id)
```

If the issuer is publicly reachable, as on EKS, GKE and AKS, let Harbor validate online instead. Give it the discovery URL and no keys. `ISS` is the issuer URL from the command above. Harbor reads the supported claims from the discovery document:

```bash
TI=$(jq -n --arg iss "$ISS" --arg a "$AUD" --arg n my-cluster \
  '{name:$n,offline_validation:false,openid_config_url:($iss+"/.well-known/openid-configuration"),audience:$a}' \
  | curl -s "${A[@]}" -X POST "https://$H/api/v2.0/trusted-issuers" -d @- | jq -r .id)
```

**3. Create the federated robot account.** It points at the issuer with `trusted_issuer_id` and carries the permissions. This one can pull from the `wif` project:

```bash
RB=$(curl -s "${A[@]}" -X POST "https://$H/api/v2.0/robots" \
  -d '{"name":"my-cluster","level":"system","duration":-1,"trusted_issuer_id":'$TI',"permissions":[{"kind":"project","namespace":"wif","access":[{"resource":"repository","action":"pull"}]}]}' | jq -r .id)
```

**4. Add the claim rule.** Claim rules attach through the issuer's `/claims` endpoint, with the `robot_id` they belong to:

```bash
curl -s "${A[@]}" -X POST "https://$H/api/v2.0/trusted-issuers/$TI/claims" \
  -d '{"rules":[{"claim_path":"sub","value":"system:serviceaccount:default:default","robot_id":'$RB'}]}'
```

The `iss` and `aud` claims are not rules. Both belong to the issuer, and Harbor rejects them as claim paths (`400 claim_path 'iss' is not accepted`). Rules cover the other claims in the token, such as `sub`, `repository`, or a nested path like `kubernetes.io.namespace`. On a Trusted Issuer using online validation, each claim path has to appear in the issuer's `claims_supported`.

Matching is exact. `sub` takes the full string, `system:serviceaccount:<namespace>:<service-account>`, and `*` is not a wildcard. One rule per service account you want to allow.

The UI path for these steps was not rechecked against 2.15, so this page uses the API. The screenshots in the root README may show older labels.

### Plain HTTP and non-standard ports

A Harbor served over TLS on 443 needs nothing here. If your Harbor speaks plain HTTP, or sits on another port without TLS, containerd needs to be told, or the pull fails even with a good token:

- kind: write `/etc/containerd/certs.d/<host:port>/hosts.toml` on every node container:

  ```toml
  server = "http://<host:port>"

  [host."http://<host:port>"]
    capabilities = ["pull", "resolve"]
  ```

- k3s and RKE2: write `/etc/rancher/k3s/registries.yaml` or `/etc/rancher/rke2/registries.yaml`, then restart the unit:

  ```yaml
  mirrors:
    "<host:port>":
      endpoint:
        - "http://<host:port>"
  ```

Use `http://` in the endpoint and keep `<host:port>` identical to the registry host in the image name and in `registry.host`. The `curl` calls above also need `http://` in place of `https://`.

The Harbor side is documented in full: [Federated Identity Provider for Workload Authentication](https://container-registry.com/docs/2.16/administration-manual/authentication-management/system-robot-accounts/federated-identity-provider-for-workload-authentication/) covers the Trusted Issuer, the JWKS handling and the claim rules, and [Authenticating a Workload with Federated Identity](https://container-registry.com/docs/2.16/user-manual/images/authenticating-a-workload-with-federated-identity/) covers presenting the token.

## Shared Files

| File | Purpose |
|------|---------|
| [`k8s_credential_provider_config.yaml`](k8s_credential_provider_config.yaml) | A kubelet `CredentialProviderConfig`, for when you install by hand instead of with the chart |
| [`rbac-audience.yaml`](rbac-audience.yaml) | The node audience RBAC, standalone |
| [`pod-example.yaml`](pod-example.yaml) | A pod that pulls from Harbor with no `imagePullSecrets` |
| [`k3d-config.yaml`](k3d-config.yaml) | k3d cluster config with the provider mount points |
| [`k3s-config.yaml`](k3s-config.yaml) | The k3s config drop-in the installer writes |
| [`rke2/config.yaml`](rke2/config.yaml) | The RKE2 config drop-in the installer writes |
| [`kind-kubelet-systemd-dropin.conf`](kind-kubelet-systemd-dropin.conf) | The kind `ExecStart` override, for reference |

The first four use `harbor.example.com` as the registry host and the audience; replace it with yours. The last three are kubelet wiring only and contain no registry reference. `pod-example.yaml` also needs an image that exists in your Harbor, or the pod sits in `ImagePullBackOff` and looks like a credential provider failure.
