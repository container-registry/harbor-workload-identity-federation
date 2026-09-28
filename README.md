# Federated Robot Accounts Examples to Push/Pull container Images Without Secrets

Repository with examples demonstrating how to use Harbor/8gears Container Registry with Federated Robot Accounts, eliminating the need for static secrets in CI/CD pipelines and Kubernetes.

## Overview

Federated Robot Accounts allow Harbor to authenticate clients using short-lived JWTs instead of static robot account secrets. By establishing a trust relationship with an external Identity Provider (like GitHub Actions, GitLab CI, or Kubernetes), Harbor can validate tokens and map them to internal robot accounts based on specific claims.

That removes the pull secrets and registry passwords from your pipelines and your clusters. Each run gets its own token, tokens expire in minutes, claim rules decide which workload may reach which project, and there is nothing left to rotate.

## Supported Identity Providers

- GitHub Actions
- GitLab CI
- Kubernetes 1.34+ (via Service Account tokens)
- FluxCD
- Forgejo Actions (TBD)

## Start Here

| You want to | Go to |
|-------------|-------|
| Push or pull from a CI job | [`examples/github-actions/`](examples/github-actions/), [`examples/gitlab-ci/`](examples/gitlab-ci/) |
| Pull from Kubernetes with no `imagePullSecrets` | [`examples/kubernetes/`](examples/kubernetes/), a page per distribution |
| Install the credential provider | [docs/install.md](docs/install.md) |
| Understand the token flow | [docs/how-it-works.md](docs/how-it-works.md) |
| Write Harbor claim rules | [docs/token-reference.md](docs/token-reference.md) |
| Configure the Harbor side | [container-registry.com/docs](https://container-registry.com/docs/2.16/administration-manual/authentication-management/system-robot-accounts/federated-identity-provider-for-workload-authentication/) |

## credential-provider-harbor

`credential-provider-harbor` is a Kubernetes kubelet credential provider plugin ([KEP-4412](https://github.com/kubernetes/enhancements/tree/master/keps/sig-auth/4412-projected-service-account-tokens-for-kubelet-image-credential-providers)) that uses Service Account tokens directly as Harbor registry passwords via Federated Robot Accounts.

The kubelet calls the binary over stdin and stdout: it sends a `CredentialProviderRequest` carrying a service account token, and gets back a `CredentialProviderResponse` with Basic Auth credentials of `jwt:<SA-token>`. Pods need no `imagePullSecrets` and no projected volume.

```bash
export HARBOR_REGISTRY=harbor.example.com
export HARBOR_AUDIENCE=https://harbor.example.com

helm upgrade --install credential-provider-harbor \
  oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system \
  --create-namespace \
  --set registry.host="${HARBOR_REGISTRY}" \
  --set registry.audience="${HARBOR_AUDIENCE}"

kubectl rollout status daemonset/credential-provider-harbor -n kube-system
./scripts/verify-node-install.sh
```

[docs/install.md](docs/install.md) has the profile for your distribution, the by-hand install, and what to check when a pull fails. The [chart README](deploy/helm/credential-provider-harbor/README.md) documents every value.

kind is the distribution tested end to end, on Kubernetes 1.34, along with k3d in CI. The other pages come from each distribution's documentation and from what the installer writes. The paths and the mechanisms are right, and nobody has watched a pull succeed on every one of them yet. Run the verify script after installing, and tell us if a page is wrong.

## Security Considerations

See also the security notes on the [Federated Identity Provider page](https://container-registry.com/docs/2.16/administration-manual/authentication-management/system-robot-accounts/federated-identity-provider-for-workload-authentication/).

- Tokens are short-lived (typically 5-10 minutes)
- Each pipeline/workflow run gets a unique token
- Claims provide fine-grained control over which workflows can access which resources
- No secrets need to be stored in CI/CD settings

## Releases

Releases are cut by [release-please](https://github.com/googleapis/release-please) from conventional commits. The binaries and the deployer image share one version line (`vX.Y.Z`); the Helm chart has its own (`chart-vX.Y.Z`). [RELEASES.md](RELEASES.md) covers the process, the repository variables that decide where artifacts are published, and how to re-run a publish by hand.

Each train publishes only its own artifacts, so with the defaults:

```text
a vX.Y.Z release        binaries for linux/amd64 and linux/arm64 on the GitHub Release
                        8gears.container-registry.com/8gcr/credential-provider-harbor-deployer:vX.Y.Z
a chart-vX.Y.Z release  oci://8gears.container-registry.com/8gcr/credential-provider-harbor:X.Y.Z
```

## References

- [KEP-4412: Service Account Tokens for Image Credential Providers](https://github.com/kubernetes/enhancements/tree/master/keps/sig-auth/4412-projected-service-account-tokens-for-kubelet-image-credential-providers)
- [Kubernetes v1.34 Release Notes](https://kubernetes.io/blog/2025/08/27/kubernetes-v1-34-release/)
- [GitHub OIDC documentation](https://docs.github.com/en/actions/deployment/security-hardening-your-deployments/about-security-hardening-with-openid-connect) and [token claims](https://docs.github.com/en/actions/deployment/security-hardening-your-deployments/about-security-hardening-with-openid-connect#understanding-the-oidc-token)
- [GitLab `id_tokens`](https://docs.gitlab.com/ci/yaml/#id_tokens) and [ID token authentication](https://docs.gitlab.com/ci/secrets/id_token_authentication.html)

## Project

- [Contributing](CONTRIBUTING.md) — development setup, commit conventions, DCO
- [Support](SUPPORT.md) — where to ask questions and how to report a bug
- [Security policy](SECURITY.md) — how to report a vulnerability privately
- [Releases](RELEASES.md) — how a release is cut and where artifacts are published
- [Roadmap](ROADMAP.md) — what is planned and where it is tracked
- [Code of Conduct](CODE_OF_CONDUCT.md)
