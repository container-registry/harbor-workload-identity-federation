# Documentation

The Harbor side of Federated Robot Accounts lives at [container-registry.com/docs](https://container-registry.com/docs/): how to add a Trusted Issuer, how JWKS validation and key rotation work, and how claim rules map a token to a robot account. This directory covers the parts that live in this repository.

| Page | What it covers |
|------|----------------|
| [How It Works](how-it-works.md) | The token flow, for a CI job and for a kubelet pull, and what Harbor checks |
| [Installing](install.md) | The Helm chart, the profile for your distribution, installing by hand, and uninstalling |
| [Token Reference](token-reference.md) | Trusted Issuer settings and the claims to write rules against, per provider, with a real token from each |

Runnable starting points are in [`examples/`](../examples/): a workflow for GitHub Actions, a pipeline for GitLab CI, a page per Kubernetes distribution, and Talos through its system extension.

The Helm chart has its own [README](../deploy/helm/credential-provider-harbor/README.md) covering every value, the install ID and readiness model, and what to do on a first install across a large cluster.

For working on the project itself, see [CONTRIBUTING.md](../CONTRIBUTING.md) and [RELEASES.md](../RELEASES.md).
