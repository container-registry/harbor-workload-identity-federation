# GitLab CI Example

This example uses GitLab CI OIDC `id_tokens` to authenticate to Harbor without storing a registry password in GitLab variables.

## Harbor Setup

Create a Harbor Federated IDP for your GitLab instance. For GitLab.com:

```text
OpenID configuration URL: https://gitlab.com/.well-known/openid-configuration
Issuer: https://gitlab.com
Audience: <your-registry-domain-or-url>
```

Create a federated robot account with claim rules matching your project, for example:

```text
iss == https://gitlab.com
aud == <your-registry-domain-or-url>
project_path == <group>/<project>
```

Grant the robot account push or pull permission on the target Harbor project.

## Use The Example

Copy [`.gitlab-ci.yml`](.gitlab-ci.yml) into your GitLab repository or merge the job into your existing pipeline.

Replace these values:

```text
macfly4200.8gears.ch -> <your-registry-domain>
library/image -> <your-project>/<your-image>
```

The `id_tokens` audience must exactly match the Harbor Federated IDP audience:

```yaml
id_tokens:
  ID_TOKEN:
    aud: <your-registry-domain-or-url>
```

The example writes Docker auth as `not-relevant:<OIDC-token>`. Harbor validates the token through the configured Federated IDP and maps it to the federated robot account.

## BuildKit

The sample uses a remote BuildKit endpoint:

```yaml
BUILDKIT_HOST: tcp://buildkitd:1234
```

If you do not run a remote BuildKit service, replace the build step with your own Docker or BuildKit setup.

## Notes

The example prints only decoded JWT header and payload for debugging. Do not print the raw token in real pipelines.
