# Federated Identity Provider Examples

Examples demonstrating how to use Harbor/8gears Container Registry with Workload Identity Federation, eliminating the need for static secrets in CI/CD pipelines and Kubernetes.

## Overview

**Workload Identity Federation** allows Harbor to authenticate clients using short-lived JWTs instead of static robot account secrets. By establishing a trust relationship with an external Identity Provider (like GitHub Actions, GitLab CI, or Kubernetes), Harbor can validate tokens and map them to internal robot accounts based on specific claims.

### Benefits

- **No static secrets**: Eliminate the operational burden of managing pull secrets
- **Enhanced security**: Use ephemeral, workload-specific credentials
- **Simplified rotation**: No secret rotation required since tokens are short-lived
- **Audit trail**: Better traceability of which workload accessed the registry

## Supported Identity Providers

- GitHub Actions
- GitLab CI
- Kubernetes 1.33 (via Service Account tokens)

---

## GitHub Actions Example

This example demonstrates how to authenticate to Harbor from a GitHub Actions workflow using OIDC tokens.

### Prerequisites

1. **Harbor Setup**: Configure a Federated Identity Provider in Harbor:
   - OpenID Configuration URL: `https://token.actions.githubusercontent.com/.well-known/openid-configuration`
   - JWKS URI: `https://token.actions.githubusercontent.com/.well-known/jwks`
   - Issuer: `https://token.actions.githubusercontent.com`

2. **Robot Account**: Create a federated robot account in Harbor with claim rules matching your GitHub repository:
   - `iss`: `https://token.actions.githubusercontent.com`
   - `aud`: `<your-registry-domain>` (e.g., `macfly4200.8gears.ch`) — The audience can be any string, but using your registry domain is recommended. This ensures tokens are scoped specifically to your registry and prevents token reuse across different services.
   - `repository`: `<owner>/<repo>` (e.g., `myorg/myrepo`)

### Workflow Configuration

```yaml
name: Create Image and Push Using federated IDP

on:
  workflow_dispatch:

jobs:
  build-and-push:
    runs-on: ubuntu-latest
    permissions:
      id-token: write   # Required for OIDC JWT
      contents: read

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Get OIDC token
        id: oidc
        run: |
          echo "Requesting OIDC token"
          RESPONSE=$(curl -s -H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \
          "${ACTIONS_ID_TOKEN_REQUEST_URL}&audience=<your-registry-domain>")

          TOKEN=$(echo "$RESPONSE" | jq -r '.value')
          echo "TOKEN=$TOKEN" >> $GITHUB_ENV

      - name: Build and Push Image
        run: |
          echo $TOKEN | docker login -u not-relevant --password-stdin <your-registry-domain>
          docker build -t <your-registry-domain>/library/image:${{ github.sha }} .
          docker push <your-registry-domain>/library/image:${{ github.sha }}

      - name: Pull Image from Registry
        run: |
          echo $TOKEN | docker login -u not-relevant --password-stdin <your-registry-domain>
          docker pull <your-registry-domain>/library/hello-world:latest
```

### Key Points

1. **Permissions**: The workflow must have `id-token: write` permission to request OIDC tokens.

2. **Audience**: The `audience` parameter in the token request must match the audience configured in your Harbor Federated Identity Provider (typically your registry domain).

3. **Username**: The username for `docker login` is not used for authentication (can be any value like `not-relevant`). Authentication is based solely on the JWT token.

4. **Token Claims**: GitHub Actions OIDC tokens include claims such as:
   - `iss`: Issuer (always `https://token.actions.githubusercontent.com`)
   - `aud`: Audience (your registry domain)
   - `sub`: Subject (e.g., `repo:owner/repo:ref:refs/heads/main`)
   - `repository`: Repository name
   - `repository_owner`: Repository owner
   - `actor`: User who triggered the workflow
   - `ref`: Git reference
   - `sha`: Commit SHA

### Debugging JWT Tokens

To inspect the JWT token contents during workflow execution:

```yaml
- name: Debug JWT Token
  run: |
    echo "=== JWT Header ==="
    echo "$TOKEN" | cut -d'.' -f1 | base64 -d 2>/dev/null | jq .
    echo ""
    echo "=== JWT Payload ==="
    echo "$TOKEN" | cut -d'.' -f2 | base64 -d 2>/dev/null | jq .
```

### Example JWT Token

Here's an example of what a GitHub Actions OIDC token looks like:

**JWT Header:**
```json
{
  "alg": "RS256",
  "kid": "38826b17-6a30-5f9b-b169-8beb8202f723",
  "typ": "JWT",
  "x5t": "ykNaY4qM_ta4k2TgZOCEYLkcYlA"
}
```

**JWT Payload:**
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
  "workflow": "Create Image and Push Using federated IDP",
  "workflow_ref": "container-registry/federated-idp-examples/.github/workflows/example_1.yml@refs/heads/main",
  "workflow_sha": "15a5ebfa3fb5ddf10c4b4250e14496bec7f03a56"
}
```

**Key claims for Harbor claim rules:**
| Claim | Description | Example Value |
|-------|-------------|---------------|
| `iss` | Token issuer | `https://token.actions.githubusercontent.com` |
| `aud` | Target audience (your registry) | `macfly4200.8gears.ch` |
| `sub` | Subject identifier | `repo:container-registry/federated-idp-examples:ref:refs/heads/main` |
| `repository` | Full repository name | `container-registry/federated-idp-examples` |
| `repository_owner` | Organization or user | `container-registry` |
| `ref` | Git reference | `refs/heads/main` |
| `actor` | User who triggered the workflow | `Vad1mo` |

---

## How It Works

1. **Token Request**: GitHub Actions provides environment variables (`ACTIONS_ID_TOKEN_REQUEST_TOKEN` and `ACTIONS_ID_TOKEN_REQUEST_URL`) to request OIDC tokens.

2. **Token Validation**: Harbor validates the JWT by:
   - Verifying the signature using GitHub's JWKS
   - Checking the `iss` (issuer) claim matches the configured provider
   - Validating `exp` (expiration) and `aud` (audience) claims

3. **Robot Account Matching**: Harbor matches the token's claims against configured claim rules to identify the appropriate robot account.

4. **Authorization**: Once matched, the request is authorized based on the robot account's permissions.

---

## Security Considerations

- Tokens are short-lived (typically 5-10 minutes)
- Each workflow run gets a unique token
- Claims provide fine-grained control over which workflows can access which resources
- No secrets need to be stored in GitHub repository settings

## References

- [GitHub OIDC Documentation](https://docs.github.com/en/actions/deployment/security-hardening-your-deployments/about-security-hardening-with-openid-connect)
- [GitHub OIDC Token Claims](https://docs.github.com/en/actions/deployment/security-hardening-your-deployments/about-security-hardening-with-openid-connect#understanding-the-oidc-token)
