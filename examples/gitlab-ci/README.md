# GitLab CI Example

This example uses GitLab CI OIDC `id_tokens` to authenticate to Harbor without storing a registry password in GitLab variables.

## Harbor Setup

Add your GitLab instance to Harbor as a Trusted Issuer. For GitLab.com:

```text
OpenID configuration URL: https://gitlab.com/.well-known/openid-configuration
Issuer: https://gitlab.com
Audience: <your-registry-domain-or-url>
```

The issuer needs the `audience` field, and an admin has to turn on the federated robot account feature gate once per Harbor. Then create the issuer, a robot account tied to it, and a claim rule on `project_path`:

```bash
H=harbor.example.com
AUD='<your-registry-domain-or-url>'
A=(-u 'admin:<password>' -H "Content-Type: application/json")

# Feature gate, once per Harbor.
curl -s "${A[@]}" -X PUT "https://$H/api/v2.0/configurations" \
  -d '{"enable_commercial_federated_robot_accounts":true,"enable_project_federated_robot_accounts":true}'

TI=$(jq -n --arg a "$AUD" --arg n gitlab-ci \
  '{name:$n,offline_validation:false,openid_config_url:"https://gitlab.com/.well-known/openid-configuration",audience:$a}' \
  | curl -s "${A[@]}" -X POST "https://$H/api/v2.0/trusted-issuers" -d @- | jq -r .id)

RB=$(curl -s "${A[@]}" -X POST "https://$H/api/v2.0/robots" \
  -d '{"name":"gitlab-ci","level":"system","duration":-1,"trusted_issuer_id":'$TI',"permissions":[{"kind":"project","namespace":"<project>","access":[{"resource":"repository","action":"pull"}]}]}' | jq -r .id)

curl -s "${A[@]}" -X POST "https://$H/api/v2.0/trusted-issuers/$TI/claims" \
  -d '{"rules":[{"claim_path":"project_path","value":"<group>/<project>","robot_id":'$RB'}]}'
```

`iss` and `aud` are not claim rules. Both belong to the issuer, and Harbor rejects them as claim paths. A Trusted Issuer created before Harbor 2.15.12 may have no audience after the upgrade; see [Upgrading Harbor to 2.15.12](../../deploy/helm/harbor-credential-provider/README.md#upgrading-harbor-to-21512). `project_path` has to be in the issuer's `claims_supported`, and the match is exact. Add a `push` access entry next to `pull` if the job pushes images.

## Use The Example

Copy [`.gitlab-ci.yml`](.gitlab-ci.yml) into your GitLab repository or merge the job into your existing pipeline.

Replace these values:

```text
macfly4200.8gears.ch -> <your-registry-domain>
library/image -> <your-project>/<your-image>
```

The `id_tokens` audience must exactly match the Harbor Trusted Issuer audience:

```yaml
id_tokens:
  ID_TOKEN:
    aud: <your-registry-domain-or-url>
```

The example writes Docker auth as `not-relevant:<OIDC-token>`. Harbor validates the token through the configured Trusted Issuer and maps it to the federated robot account.

## BuildKit

The sample uses a remote BuildKit endpoint:

```yaml
BUILDKIT_HOST: tcp://buildkitd:1234
```

If you do not run a remote BuildKit service, replace the build step with your own Docker or BuildKit setup.

## Notes

The example prints only decoded JWT header and payload for debugging. Do not print the raw token in real pipelines.

## Harbor Side

Configuring the Trusted Issuer and the robot account claim rules is documented at [Federated Identity Provider for Workload Authentication](https://container-registry.com/docs/2.16/administration-manual/authentication-management/system-robot-accounts/federated-identity-provider-for-workload-authentication/).
