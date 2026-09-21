# Examples

This directory contains copy-paste starting points for Harbor Federated Robot Accounts.

## Available Examples

| Directory | Purpose |
|-----------|---------|
| [`github-actions`](github-actions/) | Push and pull images from GitHub Actions using GitHub OIDC tokens. |
| [`gitlab-ci`](gitlab-ci/) | Push images from GitLab CI using GitLab `id_tokens`. |
| [`kubernetes`](kubernetes/) | Install and test the kubelet credential provider for secretless Kubernetes image pulls. Has a page per distribution: kubeadm, EKS, GKE, AKS, k3s, k3d, kind, RKE2, MicroK8s, OpenShift. |
| [`talos`](talos/) | Install the kubelet credential provider on Talos Linux via the official `siderolabs/harbor-credential-provider` system extension for secretless image pulls. |

## Scripts

| Script | Purpose |
|--------|---------|
| [`../scripts/verify-node-install.sh`](../scripts/verify-node-install.sh) | Check whether nodes are actually set up: the live kubelet flags first, then the files. Works on any distribution. |

AKS nodes used to need a per-node script to add the provider flags to `KUBELET_FLAGS`. The `aks` profile writes them now, on every node that joins; see [`kubernetes/aks`](kubernetes/aks/).

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
