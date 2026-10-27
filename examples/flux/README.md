# Flux

Keyless GitOps from Harbor with Flux 2.10. No registry secret anywhere in the cluster:

- source-controller pulls an `OCIRepository` and image-reflector-controller scans an `ImageRepository` with a token for a ServiceAccount you name (Flux calls this object identity).
- The `Kustomization` deploys a workload whose image comes from the same Harbor. kubelet pulls it through [`harbor-credential-provider`](../kubernetes/) with the workload's own ServiceAccount token, and the pod has no `imagePullSecrets`.

Harbor maps each token to a federated robot through a `sub` claim rule, so the audit log names the robot, and through it the ServiceAccount, behind every pull.

Background: [Keyless GitOps with Flux 2.10 and Harbor](https://container-registry.com/posts/flux-2-10-keyless-gitops-harbor/).

## Files

| File | Purpose |
|------|---------|
| [`flux-object-identity-rbac.yaml`](flux-object-identity-rbac.yaml) | Gives source-controller and image-reflector-controller `create` on `serviceaccounts/token`, which Flux 2.10 dropped from its default RBAC |
| [`gitops.yaml`](gitops.yaml) | The `apps` namespace, the opted-in `flux-oci` ServiceAccount, the `OCIRepository`, the `ImageRepository` and the `Kustomization` |
| [`apps/podinfo.yaml`](apps/podinfo.yaml) | The content of the OCI artifact: a `podinfo` ServiceAccount and Deployment, no `imagePullSecrets` |

They use `harbor.example.com` as the registry and audience and `platform` as the Harbor project. Replace both.

## Prerequisites

- Kubernetes 1.35.2 or newer, the minimum Flux 2.10 supports. `harbor-credential-provider` on its own needs 1.34.
- `harbor-credential-provider` installed on the nodes, with `registry.audience` equal to the registry host. Follow the page for your distribution under [`../kubernetes/`](../kubernetes/). Flux's own pulls do not need it; the workload's do.
- The registry is **not** in the API server's `--api-audiences`. See [Audience](#audience).
- Flux CLI v2.10.0 or newer. v2.10.0 installs source-controller v1.10.1, kustomize-controller v1.10.1 and image-reflector-controller v1.3.0.
- The workload image in Harbor, here `harbor.example.com/platform/podinfo:6.15.0` (a copy of `ghcr.io/stefanprodan/podinfo:6.15.0`).

## 1. Install Flux

```bash
flux install --components-extra=image-reflector-controller,image-automation-controller
kubectl apply -f examples/flux/flux-object-identity-rbac.yaml
```

Flux 2.10 removed `create` on `serviceaccounts/token` from the default controller RBAC ([fluxcd/flux2#6139](https://github.com/fluxcd/flux2/pull/6139)). Without the second command every `OCIRepository` and `ImageRepository` with a `serviceAccountName` fails with `cannot create resource "serviceaccounts/token"`. kustomize-controller and helm-controller are bound to `cluster-admin` and need nothing. The `ObjectLevelWorkloadIdentity` feature gate is retired in 2.10; the feature is always on.

Bootstrapping instead of `flux install`? Commit the RBAC file next to `gotk-components.yaml` and add it to the `resources` list in that directory's `kustomization.yaml`. The list is explicit, so a file that is only committed there is never applied:

```yaml
# clusters/<cluster>/flux-system/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - gotk-components.yaml
  - gotk-sync.yaml
  - flux-object-identity-rbac.yaml
```

## 2. Harbor

Two identities, so two federated robots, each with one `sub` rule. A robot takes one rule per claim path; a second `sub` rule on the same robot is refused with `409 robot <id> already has claim_path 'sub'`.

| ServiceAccount | Pulls | `sub` claim rule |
|----------------|-------|------------------|
| `apps/flux-oci` | the manifests artifact, the tag list for the scan | `system:serviceaccount:apps:flux-oci` |
| `apps/podinfo` | the workload image, through kubelet | `system:serviceaccount:apps:podinfo` |

Variables as in [Harbor Setup](../kubernetes/README.md#harbor-setup), which explains each call:

```bash
H=harbor.example.com
AUD=harbor.example.com
A=(-u 'admin:<password>' -H "Content-Type: application/json")
```

Feature gate, once per Harbor. The robots below are system-level, so the project-level gate (`enable_project_federated_robot_accounts`) is not needed:

```bash
curl -s "${A[@]}" -X PUT "https://$H/api/v2.0/configurations" \
  -d '{"enable_commercial_federated_robot_accounts":true}'
```

Trusted Issuer, offline variant, for a cluster Harbor cannot reach. The audience field has to equal the audience Flux and kubelet ask for, which is the registry host:

```bash
ISS=$(kubectl get --raw /.well-known/openid-configuration | jq -r .issuer)
JWKS=$(kubectl get --raw /openid/v1/jwks)
TI=$(jq -n --argjson k "$JWKS" --arg iss "$ISS" --arg a "$AUD" --arg n my-cluster \
  '{name:$n,issuer:$iss,offline_validation:true,jwks_keys:$k,audience:$a}' \
  | curl -s "${A[@]}" -X POST "https://$H/api/v2.0/trusted-issuers" -d @- | jq -r '.id // empty')
```

One robot per identity, and its claim rule with the robot's real ID:

```bash
robot() {
  curl -s "${A[@]}" -X POST "https://$H/api/v2.0/robots" \
    -d '{"name":"'"$1"'","level":"system","duration":-1,"trusted_issuer_id":'"$TI"',"permissions":[{"kind":"project","namespace":"platform","access":[{"resource":"repository","action":"pull"}]}]}' \
    | jq -r '.id // empty'
}
RB_FLUX=$(robot flux-oci)
RB_APP=$(robot podinfo)

curl -s "${A[@]}" -X POST "https://$H/api/v2.0/trusted-issuers/$TI/claims" \
  -d '{"rules":[
        {"claim_path":"sub","value":"system:serviceaccount:apps:flux-oci","robot_id":'"$RB_FLUX"'},
        {"claim_path":"sub","value":"system:serviceaccount:apps:podinfo","robot_id":'"$RB_APP"'}]}'
```

Check that both rules carry a `robot_id` before going on. A rule stored without one applies to every token of the issuer, and with it in place every other ServiceAccount of the cluster is treated as anonymous:

```bash
curl -s "${A[@]}" "https://$H/api/v2.0/trusted-issuers/$TI/claims" | jq -c '.[] | {claim_path, value, robot_id}'
```

## 3. Publish the manifests

Pushing is the publisher's job, typically CI, with a robot that has push access. It is not part of the keyless path in the cluster, so it needs credentials: `PUSH_USER` and `PUSH_SECRET` below are a robot with `push` on the project. A [GitHub Actions](../github-actions/) or [GitLab CI](../gitlab-ci/) job can do it keyless as well.

```bash
flux push artifact oci://harbor.example.com/platform/podinfo-deploy:v1 \
  --path=examples/flux/apps \
  --source="$(git config --get remote.origin.url)" \
  --revision="$(git branch --show-current)@sha1:$(git rev-parse HEAD)" \
  --creds="$PUSH_USER:$PUSH_SECRET"
```

Do not name the repository `manifests`. Harbor answers `400 Bad Request` on the first blob request for `<project>/manifests`.

## 4. Apply the Flux objects

```bash
kubectl apply -f examples/flux/gitops.yaml
```

The two annotations on `flux-oci` are what turns object identity on:

```yaml
credential.auth.fluxcd.io/provider: kubernetes
credential.auth.fluxcd.io/type: jwt
```

Without them Flux does not mint a token. It falls back to `imagePullSecrets` on the ServiceAccount, or anonymous access, and Harbor answers `UNAUTHORIZED`. Two more are optional:

| Annotation | Effect |
|------------|--------|
| `credential.auth.fluxcd.io/audiences` | Audience of the token. Defaults to the registry host, which has to equal the Trusted Issuer's audience. |
| `credential.auth.fluxcd.io/username` | Sends Basic auth with this username and the token as the password instead of a Bearer token. Harbor accepts both. |

The `OCIRepository` and `ImageRepository` set `provider: generic` and `serviceAccountName: flux-oci`, and have no `secretRef`.

## 5. Check it

```bash
flux get sources oci -n apps
flux get images repository -n apps
flux get kustomizations -n apps
kubectl -n apps get pods
kubectl -n apps get pod -l app=podinfo -o jsonpath='{.items[0].spec.imagePullSecrets}'   # empty
```

Harbor attributes each pull to a robot:

```bash
curl -s "${A[@]}" "https://$H/api/v2.0/auditlog-exts?page_size=10&sort=-op_time" \
  | jq -c '.[] | select(.operation == "pull") | {op_time, username, resource}'
```

You should see the `flux-oci` robot pulling `platform/podinfo-deploy` and the `podinfo` robot pulling `platform/podinfo`. Each name carries your Harbor's robot prefix, `robot$` by default (`robot$flux-oci`).

## Audience

Leave the registry out of the API server's `--api-audiences`. If you added it while following an earlier version of these pages, take it out. kubelet does not need it: the node audience RBAC the chart creates is what lets a node ask for a registry token. Flux does not need it either: source-controller asks for a token for the ServiceAccount in `serviceAccountName`, and the `serviceaccounts/token` RBAC above is what allows that.

With the registry in `--api-audiences`, every default ServiceAccount token carries the registry audience too, Flux's own included. Flux refuses to request a token for an audience its own token holds, so every keyless pull fails with `audience '<registry>' is not allowed as it is intended for the local kubernetes cluster`. The flag also makes a token you hand Harbor a valid credential for the cluster API. Without it, the API server answers such a token with `401`.

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| `serviceaccounts "flux-oci" is forbidden: User "system:serviceaccount:flux-system:source-controller" cannot create resource "serviceaccounts/token"` (or `image-reflector-controller`) | Flux 2.10 default RBAC | Apply [`flux-object-identity-rbac.yaml`](flux-object-identity-rbac.yaml) |
| `audience '<registry>' is not allowed as it is intended for the local kubernetes cluster` | The registry is in the API server's `--api-audiences` | Remove it, restart the API server, then the Flux controllers so they load a new token |
| `UNAUTHORIZED: unauthorized to access repository: <project>/<repo>, action: pull`, and no token error before it | The ServiceAccount is not opted in, so Flux sent no token | Add both `credential.auth.fluxcd.io/provider: kubernetes` and `credential.auth.fluxcd.io/type: jwt` |
| Same `UNAUTHORIZED` with the annotations in place | No robot matches the token: `sub` rule missing or not exact, wrong audience on the issuer, or stale JWKS | Check the rule against `system:serviceaccount:<ns>:<sa>`, the issuer audience against the registry host, and the issuer keys against `kubectl get --raw /openid/v1/jwks` |
| Every ServiceAccount of the cluster gets `UNAUTHORIZED` | A claim rule without `robot_id` on the issuer | List the rules and delete the one without a `robot_id` |
| `403 project-level federated robot accounts feature is not enabled` | A `level: project` robot with the project gate off | Use a system-level robot, or enable `enable_project_federated_robot_accounts` |
| `409 robot <id> already has claim_path 'sub'` | A second `sub` rule on one robot | One robot per ServiceAccount |
| Workload pod `ErrImagePull` with `no basic auth credentials` or `401` | kubelet never called the provider, or the workload ServiceAccount has no robot | `./scripts/verify-node-install.sh`, then the `sub` rule for the workload ServiceAccount |
| `flux push artifact` fails with `400 Bad Request` | Repository named `manifests` | Pick another name |

## Tested

On 2026-10-26 with Flux v2.10.0 (source-controller v1.10.1, kustomize-controller v1.10.1, image-reflector-controller v1.3.0), k3d v5.9.0 running k3s v1.35.5 with no `--api-audiences` argument, chart `harbor-credential-provider` 0.4.0 with `profile=k3d`, and Harbor 2.16.0 with system-level robots. The `OCIRepository` and `ImageRepository` went Ready, the `Kustomization` applied, the podinfo pod ran with no `imagePullSecrets`, and the Harbor audit log attributed the artifact pull and the image pull to their robots. The `credential.auth.fluxcd.io/username` variant was checked too. In that run the artifact referenced the workload image as `podinfo:6.15.0@sha256:ec73780a...`; earlier runs on the same Harbor pulled it by the tag alone, as in [`apps/podinfo.yaml`](apps/podinfo.yaml).

On k3d, install the chart with `--set kubelet.restart=false` and run `docker restart k3d-<cluster>-server-0` afterwards until [#32](https://github.com/container-registry/harbor-workload-identity-federation/issues/32) is fixed.

## Cleanup

```bash
kubectl delete -f examples/flux/gitops.yaml
kubectl delete -f examples/flux/flux-object-identity-rbac.yaml
```

In Harbor, delete the robots first, then the Trusted Issuer, then the `podinfo-deploy` repository.
