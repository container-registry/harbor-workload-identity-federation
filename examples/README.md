# Examples

This directory contains copy-paste starting points for Harbor Workload Identity Federation.

## Available Examples

| Directory | Purpose |
|-----------|---------|
| [`github-actions`](github-actions/) | Push and pull images from GitHub Actions using GitHub OIDC tokens. |
| [`gitlab-ci`](gitlab-ci/) | Push images from GitLab CI using GitLab `id_tokens`. |
| [`kubernetes`](kubernetes/) | Install and test the kubelet credential provider for secretless Kubernetes image pulls. |

## Before You Start

Create a Harbor Federated IDP for the workload issuer, then create a federated robot account with pull or push permissions for the target project.

Use the same audience value in both places:

```text
Harbor Federated IDP audience == token request audience
```

For registry publishing in this repository's CI/release workflows, set these GitHub repository variables and secret:

```text
REGISTRY_ADDRESS=8gears.container-registry.com
PROJECT_NAME=8gcr
REGISTRY_USERNAME=<robot-or-user-with-push-access>
REGISTRY_PASSWORD=<secret>
```

The resulting image path is:

```text
8gears.container-registry.com/8gcr/credential-provider-harbor-deployer
```
