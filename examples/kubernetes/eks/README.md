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

Put this in `values.yaml` and change the two hostnames:

```yaml
profile: eks

registry:
  # Your Harbor.
  host: harbor.example.com
  # The aud your Harbor trusted issuer expects in the token. Using the registry
  # host is the convention; what matters is that this string and the one in
  # Harbor's claim rule are the same.
  audience: harbor.example.com

kubelet:
  # The AL2023 AMI already passes the credential provider flags, so there is no
  # kubelet unit to rewrite. Kubelet still gets restarted, which is what makes
  # it re-read config.json.
  configure: false

extraEnv:
  # Keep the ecr-credential-provider entry the AMI ships. Turn this off only if
  # nothing on the node pulls from ECR, the AWS add-ons included.
  - name: PRESERVE_ECR_PROVIDER
    value: "true"
```

Then install the published chart against it. Nothing here needs a checkout of
this repository:

```bash
helm upgrade --install credential-provider-harbor \
  oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system \
  -f values.yaml

kubectl rollout status daemonset/credential-provider-harbor -n kube-system
```

`rollout status` is worth waiting on. A pod reports ready only once its own node
is installed and its kubelet restart has returned, so the rollout finishing means
the cluster is done, not that the pods started.

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

Each pod logs what it did to its node:

```bash
kubectl logs -l app.kubernetes.io/name=credential-provider-harbor -n kube-system
```

A finished node ends on `[INFO] Installation complete`. The
`[WARN] Kubelet configuration disabled` line above it is expected on this
profile, and names the flags it is counting on the AMI to have set:

```text
[INFO] Binary already up to date: /host/etc/eks/image-credential-provider/credential-provider-harbor
[INFO] Wrote credential provider config: /host/etc/eks/image-credential-provider/config.json
[WARN] Kubelet configuration disabled. Ensure kubelet uses --image-credential-provider-bin-dir=/etc/eks/image-credential-provider and --image-credential-provider-config=/etc/eks/image-credential-provider/config.json
[INFO] Restarting kubelet
[INFO] Installation complete
```

From a checkout of this repository, `scripts/verify-node-install.sh` checks the
nodes instead of the logs: it reads the live kubelet command line, the binary and
the merged config on every node, so it catches a node the DaemonSet never reached.

## Getting Onto a Node

The logs say what the installer did. To see the node itself — the merged `config.json`, the binary, the live kubelet command line — there are two ways in, and neither needs an SSH key or a public IP.

### A debug pod

Nothing to set up. Works on a private node today:

```bash
kubectl debug node/<node-name> -it \
  --image=public.ecr.aws/amazonlinux/amazonlinux:2023 \
  --profile=sysadmin
```

The node's filesystem is mounted at `/host`:

```bash
cat /host/etc/eks/image-credential-provider/config.json
ls -la /host/etc/eks/image-credential-provider/
```

`--profile=sysadmin` is what grants the privileges; without it the pod comes up unable to read most of `/host`. It needs kubectl 1.30 or newer, which added that profile; older clients reject it with `profile "sysadmin" is not supported`.

`kubectl debug` has no `--rm`, so the pod lands in the current namespace and stays there. Delete it when you are done:

```bash
kubectl delete pod node-debugger-<node>-<id>
```

Being a pod, this needs a working kubelet on the node. A node too broken to schedule is the case for SSM.

### SSM Session Manager

A real shell, no key, no public IP, out through the private subnet:

```bash
aws ssm start-session --target <instance-id>
```

The agent is already on the AL2023 EKS AMI. What is usually missing is the permission: a managed node group role gets `AmazonEKSWorkerNodePolicy`, `AmazonEKS_CNI_Policy` and `AmazonEC2ContainerRegistryReadOnly`, and none of those include SSM. Until the policy is attached the instance never registers, and `start-session` fails with a target-not-connected error rather than anything mentioning permissions.

```bash
# A registered instance appears here. Empty output is the symptom.
aws ssm describe-instance-information \
  --query 'InstanceInformationList[].{id:InstanceId,ping:PingStatus}' --output table

# If it is missing, attach the policy to the node group role.
aws iam attach-role-policy \
  --role-name <node-group-role> \
  --policy-arn arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore
```

Attaching it to a node that is already running is not enough on its own. The
agent retries roughly every half hour, so it can look like the policy did not
work when it is only waiting. Restart it and the node registers within a minute:

```bash
kubectl debug node/<node-name> --profile=sysadmin -q \
  --image=public.ecr.aws/amazonlinux/amazonlinux:2023 \
  -- chroot /host systemctl restart amazon-ssm-agent
```

This leaves a completed pod behind as well, one per node it runs against:

```bash
kubectl get pods -o name | grep node-debugger | xargs -r kubectl delete
```

Nodes created after the policy is on the role register on boot, so this is a
one-off for the nodes that were already up. Put the policy in whatever
provisions the node group and it stops being a step at all.

`start-session` also needs the Session Manager plugin installed locally, which
is a separate download from the AWS CLI. Without it the command fails with
`SessionManagerPlugin is not found`. `aws ssm send-command` needs no plugin, so
it is the quicker way to check the agent side is working:

```bash
aws ssm send-command --instance-ids <instance-id> \
  --document-name AWS-RunShellScript \
  --parameters 'commands=["ls /etc/eks/image-credential-provider/"]'
```

Instance IDs come from the node group:

```bash
aws ec2 describe-instances \
  --filters "Name=tag:eks:cluster-name,Values=<cluster>" \
            "Name=instance-state-name,Values=running" \
  --query 'Reservations[].Instances[].InstanceId' --output text
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
