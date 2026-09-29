# Amazon EKS

EKS is the easiest case, because the AL2023 AMI already runs kubelet with the credential provider flags:

```text
--image-credential-provider-bin-dir=/etc/eks/image-credential-provider
--image-credential-provider-config=/etc/eks/image-credential-provider/config.json
```

So the installer does not touch the kubelet unit at all. It drops the binary next to `ecr-credential-provider` and merges its entry into the existing `config.json`.

## The ECR Entry

`config.json` on an EKS node already has an `ecr-credential-provider` entry, and the AWS add-ons (VPC CNI, kube-proxy, CoreDNS) pull through it. The installer preserves that entry by default. Do not turn `PRESERVE_ECR_PROVIDER` off unless you are certain nothing on the node pulls from ECR, including during a node replacement.

## Install

```bash
helm upgrade --install credential-provider-harbor \
  oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system \
  -f examples/kubernetes/eks/values.yaml \
  --set registry.host=harbor.example.com \
  --set registry.audience=harbor.example.com

kubectl rollout status daemonset/credential-provider-harbor -n kube-system
```

## New Nodes

This installs onto nodes that exist now. A node group scale-up, an AMI update, or a node replacement brings up a node without it, and the DaemonSet installs onto that node when its pod starts. Between the node going Ready and the installer finishing, pulls from Harbor on that node fail. That window is short, but it exists.

If you want it closed, bake the binary and the config into a custom AMI, or add the install to the node group's user data, and use the chart only to keep configuration in sync.

## Requirements

- Kubernetes 1.34 or newer on the cluster. EKS versions below that do not have the service account token support this depends on.
- AL2023 node images. AL2 uses different paths; use `profile=custom` there and point at wherever that AMI keeps its credential provider directory.
- No IAM changes. The token comes from the cluster's own service account issuer, not from AWS.

## Harbor Side

The cluster's issuer is the EKS OIDC provider URL:

```bash
aws eks describe-cluster --name <cluster> --query cluster.identity.oidc.issuer --output text
```

That URL is publicly reachable, so the Harbor Trusted Issuer can validate tokens online. Point it at `<issuer>/.well-known/openid-configuration` and set the audience to the value in `registry.audience`.

## Check It Took

```bash
./scripts/verify-node-install.sh
```

## Uninstall

```bash
helm uninstall credential-provider-harbor -n kube-system
```

Removes the DaemonSet, the ServiceAccount and the RBAC. Touches no node. Enough on its own if the node group is about to roll: replacements come up from the AMI clean.

To clean a node in place: `config.json` is shared with `ecr-credential-provider`, so edit it rather than delete it. Take the AWS entry out and the VPC CNI, kube-proxy and CoreDNS stop pulling.

Entry out, restart, then delete. Kubelet reads the config at startup, so it keeps exec'ing the provider until a restart; delete the binary first and pulls from Harbor fail against a missing file.

```bash
# 1. Remove only the credential-provider-harbor entry.
sudo "${EDITOR:-vi}" /etc/eks/image-credential-provider/config.json

# 2. Kubelet reads that file only at startup.
sudo systemctl restart kubelet

# 3. Nothing calls it now.
sudo rm -f /etc/eks/image-credential-provider/credential-provider-harbor
sudo rm -f /var/lib/credential-provider-harbor/install-marker
```

No kubelet drop-in to remove: the AMI supplies the flags, hence `kubelet.configure=false`.

Reference: [Uninstalling](../../../deploy/helm/credential-provider-harbor/README.md#uninstalling).
