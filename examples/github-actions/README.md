# GitHub Actions Example

This example uses GitHub Actions OIDC to authenticate to Harbor without storing a registry password in GitHub secrets.

## Harbor Setup

Create a Harbor Federated IDP:

```text
OpenID configuration URL: https://token.actions.githubusercontent.com/.well-known/openid-configuration
Issuer: https://token.actions.githubusercontent.com
Audience: <your-registry-domain-or-url>
```

Create a federated robot account with claim rules matching your workflow, for example:

```text
iss == https://token.actions.githubusercontent.com
aud == <your-registry-domain-or-url>
repository == <owner>/<repo>
```

Grant the robot account push or pull permission on the target Harbor project.

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

The token request audience must exactly match the Harbor Federated IDP audience:

```bash
"${ACTIONS_ID_TOKEN_REQUEST_URL}&audience=<your-registry-domain-or-url>"
```

## Notes

The example prints only decoded JWT header and payload for debugging. Do not print the raw token in real pipelines.
