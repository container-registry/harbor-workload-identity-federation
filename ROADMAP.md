# Roadmap

## Where Work Is Tracked

Planned and in-progress work lives in the repository issue tracker:

- [Issues](https://github.com/container-registry/harbor-workload-identity-federation/issues)
- [Milestones](https://github.com/container-registry/harbor-workload-identity-federation/milestones)

## Current Direction

This repository is the foundation for the Federated Robot Accounts Kubernetes component. It holds the credential provider binary, the installer, the Helm chart, and per-distribution quick guides. Conceptual and reference documentation lives at https://container-registry.com/docs/.

Near-term areas of work:

- Distribution coverage. The installer ships profiles for generic kubeadm/systemd nodes, EKS, GKE, k3s/k3d, and kind. Talos is covered by a system extension. Other distributions are handled through the `custom` profile until they get a profile of their own.
- Release automation. Tagged releases publish binaries, a multi-architecture image, and the Helm chart.
- Examples. Each supported distribution gets a runnable example under `examples/`.

## Suggesting Work

1. Search [issues](https://github.com/container-registry/harbor-workload-identity-federation/issues) for the same request.
2. Open a [feature request](https://github.com/container-registry/harbor-workload-identity-federation/issues/new?template=feature_request.yml) for a concrete change, or a [proposal](https://github.com/container-registry/harbor-workload-identity-federation/issues/new?template=proposal.yml) for anything that changes the installation model or the on-node layout.

## Releases

See [docs/RELEASES.md](docs/RELEASES.md) for how a release is cut, and [GitHub Releases](https://github.com/container-registry/harbor-workload-identity-federation/releases) for published versions.
