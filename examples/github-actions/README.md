# GitHub Actions Example

This example uses GitHub Actions OIDC to authenticate to Harbor without storing a registry password in GitHub secrets.

## Harbor Setup

Add GitHub as a Trusted Issuer in Harbor:

```text
OpenID configuration URL: https://token.actions.githubusercontent.com/.well-known/openid-configuration
Issuer: https://token.actions.githubusercontent.com
Audience: <your-registry-domain-or-url>
```

The issuer needs the `audience` field, and an admin has to turn on the federated robot account feature gate once per Harbor. Then create the issuer, a robot account tied to it, and a claim rule on `repository`:

```bash
H=harbor.example.com
AUD='<your-registry-domain-or-url>'
A=(-u 'admin:<password>' -H "Content-Type: application/json")

# Feature gate, once per Harbor.
curl -s "${A[@]}" -X PUT "https://$H/api/v2.0/configurations" \
  -d '{"enable_commercial_federated_robot_accounts":true,"enable_project_federated_robot_accounts":true}'

TI=$(jq -n --arg a "$AUD" --arg n github-actions \
  '{name:$n,offline_validation:false,openid_config_url:"https://token.actions.githubusercontent.com/.well-known/openid-configuration",audience:$a}' \
  | curl -s "${A[@]}" -X POST "https://$H/api/v2.0/trusted-issuers" -d @- | jq -r .id)

RB=$(curl -s "${A[@]}" -X POST "https://$H/api/v2.0/robots" \
  -d '{"name":"github-actions","level":"system","duration":-1,"trusted_issuer_id":'$TI',"permissions":[{"kind":"project","namespace":"<project>","access":[{"resource":"repository","action":"pull"}]}]}' | jq -r .id)

curl -s "${A[@]}" -X POST "https://$H/api/v2.0/trusted-issuers/$TI/claims" \
  -d '{"rules":[{"claim_path":"repository","value":"<owner>/<repo>","robot_id":'$RB'}]}'
```

`iss` and `aud` are not claim rules. Both belong to the issuer, and Harbor rejects them as claim paths. A Trusted Issuer created before Harbor 2.15.12 may have no audience after the upgrade; see [Upgrading Harbor to 2.15.12](../../deploy/helm/harbor-credential-provider/README.md#upgrading-harbor-to-21512). `repository` has to be in the issuer's `claims_supported`, and the match is exact. Add a `push` access entry next to `pull` if the job pushes images.

## Use The Example

Copy [`example_1.yml`](example_1.yml) to your repository:

```bash
mkdir -p .github/workflows
cp examples/github-actions/example_1.yml .github/workflows/harbor-wif.yml
```

Replace these values in the workflow:

```text
silly-snyder.container-registry.com -> <your-registry-domain>
library/image -> <your-project>/<your-image>
```

The workflow must request an OIDC token:

```yaml
permissions:
  id-token: write
  contents: read
```

The token request audience must exactly match the Harbor Trusted Issuer audience:

```bash
"${ACTIONS_ID_TOKEN_REQUEST_URL}&audience=<your-registry-domain-or-url>"
```

## Notes

The example prints only decoded JWT header and payload for debugging. Do not print the raw token in real pipelines.

## Harbor Side

Configuring the Trusted Issuer and the robot account claim rules is documented at [Federated Identity Provider for Workload Authentication](https://container-registry.com/docs/2.16/administration-manual/authentication-management/system-robot-accounts/federated-identity-provider-for-workload-authentication/).
