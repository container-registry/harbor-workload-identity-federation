# Token Reference

What to put in the Harbor Trusted Issuer, and which claims each platform gives you to write robot account rules against. The Harbor side is documented in full at [Federated Identity Provider for Workload Authentication](https://container-registry.com/docs/2.16/administration-manual/authentication-management/system-robot-accounts/federated-identity-provider-for-workload-authentication/); [Authenticating a Workload with Federated Identity](https://container-registry.com/docs/2.16/user-manual/images/authenticating-a-workload-with-federated-identity/) covers presenting the token.

One rule holds for all three platforms: the audience the client asks for and the audience on the Trusted Issuer have to be the same string.

![Harbor Trusted Issuer Setup](../images/harbor-federated-idp-setup.png)

## GitHub Actions

Trusted Issuer:

```text
OpenID configuration URL: https://token.actions.githubusercontent.com/.well-known/openid-configuration
Issuer:                   https://token.actions.githubusercontent.com
Audience:                 <your-registry-domain>
```

The JWKS URI and the issuer are discovered from that URL.

A real token payload:

```json
{
  "actor": "Vad1mo",
  "actor_id": "1492007",
  "aud": "macfly4200.8gears.ch",
  "base_ref": "",
  "check_run_id": "56363837828",
  "event_name": "workflow_dispatch",
  "exp": 1764090768,
  "head_ref": "",
  "iat": 1764090468,
  "iss": "https://token.actions.githubusercontent.com",
  "job_workflow_ref": "container-registry/federated-idp-examples/.github/workflows/example_1.yml@refs/heads/main",
  "job_workflow_sha": "15a5ebfa3fb5ddf10c4b4250e14496bec7f03a56",
  "jti": "6cf8862b-b832-4372-9998-22026ecd21a1",
  "nbf": 1764090168,
  "ref": "refs/heads/main",
  "ref_protected": "false",
  "ref_type": "branch",
  "repository": "container-registry/federated-idp-examples",
  "repository_id": "1104004353",
  "repository_owner": "container-registry",
  "repository_owner_id": "46576199",
  "repository_visibility": "public",
  "run_attempt": "1",
  "run_id": "19677834613",
  "run_number": "4",
  "runner_environment": "github-hosted",
  "sha": "15a5ebfa3fb5ddf10c4b4250e14496bec7f03a56",
  "sub": "repo:container-registry/federated-idp-examples:ref:refs/heads/main",
  "workflow": "Create Image and Push Using Federated Robot Account",
  "workflow_ref": "container-registry/federated-idp-examples/.github/workflows/example_1.yml@refs/heads/main",
  "workflow_sha": "15a5ebfa3fb5ddf10c4b4250e14496bec7f03a56"
}
```

Claims worth writing rules against:

| Claim | Description | Example value |
|-------|-------------|---------------|
| `iss` | Token issuer | `https://token.actions.githubusercontent.com` |
| `aud` | Target audience, your registry | `macfly4200.8gears.ch` |
| `sub` | Subject identifier | `repo:container-registry/federated-idp-examples:ref:refs/heads/main` |
| `repository` | Full repository name | `container-registry/federated-idp-examples` |
| `repository_owner` | Organization or user | `container-registry` |
| `ref` | Git reference | `refs/heads/main` |
| `actor` | User who triggered the workflow | `Vad1mo` |

## GitLab CI

Trusted Issuer, for gitlab.com:

```text
OpenID configuration URL: https://gitlab.com/.well-known/openid-configuration
Issuer:                   https://gitlab.com
Audience:                 <your-registry-domain>
```

A real token payload:

```json
{
  "project_id": "76366029",
  "project_path": "8gears/container-registry/harbor-workload-identity-federation",
  "namespace_id": "1087575",
  "namespace_path": "8gears/container-registry",
  "user_id": "907142",
  "user_login": "vad1mo",
  "user_email": "<redacted>",
  "user_access_level": "owner",
  "pipeline_id": "2179265456",
  "pipeline_source": "push",
  "job_id": "12217223061",
  "ref": "main",
  "ref_type": "branch",
  "ref_path": "refs/heads/main",
  "ref_protected": "true",
  "runner_id": 47561171,
  "runner_environment": "self-hosted",
  "sha": "de7a8b6e3892321ac9d2305f26332dcdc775f1c2",
  "project_visibility": "public",
  "ci_config_ref_uri": "gitlab.com/8gears/container-registry/harbor-workload-identity-federation//.gitlab-ci.yml@refs/heads/main",
  "ci_config_sha": "de7a8b6e3892321ac9d2305f26332dcdc775f1c2",
  "jti": "aeb1491b-8988-49b5-ada0-2e372973b18e",
  "iat": 1764098080,
  "nbf": 1764098075,
  "exp": 1764101680,
  "iss": "https://gitlab.com",
  "sub": "project_path:8gears/container-registry/harbor-workload-identity-federation:ref_type:branch:ref:main",
  "aud": "macfly4200.8gears.ch"
}
```

Claims worth writing rules against:

| Claim | Description | Example value |
|-------|-------------|---------------|
| `iss` | Token issuer | `https://gitlab.com` |
| `aud` | Target audience, your registry | `macfly4200.8gears.ch` |
| `sub` | Subject identifier | `project_path:8gears/container-registry/harbor-workload-identity-federation:ref_type:branch:ref:main` |
| `project_path` | Full project path | `8gears/container-registry/harbor-workload-identity-federation` |
| `namespace_path` | Group or namespace path | `8gears/container-registry` |
| `ref` | Git reference | `main` |
| `user_login` | User who triggered the pipeline | `vad1mo` |

## Kubernetes

The issuer is whatever the cluster reports:

```bash
kubectl get --raw /.well-known/openid-configuration | jq -r .issuer
```

On a cluster Harbor can reach, that URL is enough and Harbor validates online. On a local or private cluster it is not, so fetch the keys and paste them into the Trusted Issuer as inline JWKS:

```bash
kubectl get --raw "$(kubectl get --raw /.well-known/openid-configuration | jq -r .jwks_uri)"
```

Recreating a cluster generates new signing keys, so the inline JWKS has to be updated with them.

A real token payload, as the kubelet requests it for a pod:

```json
{
  "aud": ["macfly4200.8gears.ch"],
  "exp": 1764290604,
  "iat": 1764287004,
  "iss": "https://kubernetes.default.svc.cluster.local",
  "jti": "a8c1d9f4-ded3-42b8-9377-402f6cc34f5e",
  "kubernetes.io": {
    "namespace": "default",
    "node": {
      "name": "k3d-credential-provider-test-server-0",
      "uid": "c922a6dd-d8af-4877-8a09-922898d7ddb5"
    },
    "pod": {
      "name": "httpd",
      "uid": "375b30f1-ef29-4477-b46a-aa80607b6cfc"
    },
    "serviceaccount": {
      "name": "default",
      "uid": "c871d5a5-d81d-4ce7-8c4b-2dd62dbde226"
    }
  },
  "nbf": 1764287004,
  "sub": "system:serviceaccount:default:default"
}
```

Claims worth writing rules against:

| Claim | Description | Example value |
|-------|-------------|---------------|
| `iss` | Token issuer | `https://kubernetes.default.svc.cluster.local` |
| `aud` | Target audience | `macfly4200.8gears.ch` |
| `sub` | Subject identifier | `system:serviceaccount:default:default` |
| `kubernetes.io.namespace` | Namespace | `default` |
| `kubernetes.io.serviceaccount.name` | Service account name | `default` |
| `kubernetes.io.pod.name` | Pod the image is being pulled for | `httpd` |

`sub` is the usual one to match on. `system:serviceaccount:<namespace>:<service-account>` pins a rule to one workload; `system:serviceaccount:*:*` accepts any service account in the cluster.

## Reading a Token

Both CI examples decode the header and the payload so you can see what you have to match on:

```bash
echo "$TOKEN" | cut -d'.' -f1 | tr '_-' '/+' | base64 -d | jq .   # header
echo "$TOKEN" | cut -d'.' -f2 | tr '_-' '/+' | base64 -d | jq .   # payload
```

A JWT segment is base64url, so `-` and `_` have to become `+` and `/` before `base64 -d` will take it. Without that `tr`, GNU `base64` stops at the first such character and reports `invalid input`, and a token that happens to contain none decodes fine, which makes the bug look intermittent.

Print only the header and the payload. The raw token is a registry credential for as long as it lives, so it does not belong in a pipeline log.
