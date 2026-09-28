# Amazon EKS

EKS is the easiest case, because the AL2023 AMI already runs kubelet with the credential provider flags:

```text
--image-credential-provider-bin-dir=/etc/eks/image-credential-provider
--image-credential-provider-config=/etc/eks/image-credential-provider/config.json
```

So the installer does not touch the kubelet unit at all. It drops the binary next to `ecr-credential-provider` and merges its entry into the existing `config.json`.

## Audience

kubelet asks the API server for a token whose audience is `registry.audience`, and that request is authorized by the node audience RBAC the chart creates. EKS does not expose `--api-audiences`, and does not need to: that flag governs the tokens the API server accepts, not the ones it issues. Any agreed string works as the audience, as long as the Harbor Trusted Issuer is configured with the same one.

## The ECR Entry

`config.json` on an EKS node already has an `ecr-credential-provider` entry, and the AWS add-ons (VPC CNI, kube-proxy, CoreDNS) pull through it. The installer preserves that entry by default. Do not turn `PRESERVE_ECR_PROVIDER` off unless you are certain nothing on the node pulls from ECR, including during a node replacement.

Preserving it means leaving it alone: the installer adds an ECR entry only when the file has none, so an entry the AMI already wrote keeps its own `args` and `env`. The file is still read and rewritten as a whole, though, so check the result on a node rather than assuming it.

## The Config `apiVersion`

The installer keeps whatever top-level `apiVersion` it finds in `config.json` and only sets `kubelet.config.k8s.io/v1` when it writes the file from scratch. The harbor entry carries `tokenAttributes`, which the kubelet understands only under the `v1` config API, so a node whose AMI shipped an older `apiVersion` gets a config the kubelet will not use, and pulls fail with `no basic auth credentials` while every file looks correct. Read the version off a node before rolling this out:

```bash
NODE=$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}')
kubectl debug node/$NODE --image=busybox:1.36 --profile=sysadmin -n kube-system \
  -q --attach=true -- chroot /host cat /etc/eks/image-credential-provider/config.json
```

Expect `"apiVersion": "kubelet.config.k8s.io/v1"` at the top, both `credential-provider-harbor` and `ecr-credential-provider` under `providers`, the ECR entry still carrying its original `args` and `env`, and the harbor entry's `tokenAttributes.serviceAccountTokenAudience` equal to `registry.audience`. Clean up the debug pod afterwards:

```bash
kubectl get pods -n kube-system -o name | grep -F "pod/node-debugger-${NODE}-" \
  | xargs -I{} kubectl delete -n kube-system {}
```

The name filter is scoped to `${NODE}` so it cannot take another node's debug pod with it, and `xargs -I{}` runs nothing on empty input on both GNU and BSD `xargs`.

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
