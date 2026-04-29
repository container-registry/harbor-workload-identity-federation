# credential-provider-harbor Helm Chart

Installs `credential-provider-harbor` onto every Kubernetes node so kubelet can pull Harbor images with service account tokens instead of image pull secrets.

## Quick Start

```bash
helm upgrade --install credential-provider-harbor . \
  --namespace kube-system \
  --create-namespace \
  --set registry.host=harbor.example.com
```

By default the chart configures kubelet and restarts it after installing the binary and config. Disable that behavior only when you want to roll nodes yourself:

```bash
--set kubelet.restart=false
```

## Profiles

| Profile | Use case |
|---------|----------|
| `generic` | kubeadm/systemd nodes |
| `eks` or `aws` | Amazon EKS AL2023, preserving ECR provider config |
| `k3s` or `k3d` | k3s/k3d nodes |
| `kind` | KIND node containers for local tests |
| `gke` | GKE Standard best-effort only |
| `custom` | Explicit `credentialProvider.*` paths |

GKE Autopilot is unsupported because it blocks privileged host access.

## Required Harbor Setup

Configure Harbor FedIDP with the cluster issuer and JWKS, then create a federated robot account matching the service account token claims, typically `aud`, `iss`, and `sub`.
