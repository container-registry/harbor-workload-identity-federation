# MicroK8s

Partly supported, in the same way as [RKE2](../rke2/) and [AKS](../aks/): the chart places the files, you add the kubelet arguments.

MicroK8s runs kubelet inside `kubelite`, from a snap. Its arguments live in `/var/snap/microk8s/current/args/kubelet`, one per line, not in a systemd drop-in.

## Paths

The snap's confinement means `/usr/local/bin` is not a useful place for the binary. Put it under the snap's own writable tree, `/var/snap/microk8s/common`, which survives snap refreshes.

## Steps

**1. Add the arguments on every node:**

```bash
sudo tee -a /var/snap/microk8s/current/args/kubelet <<'ARGS'
--image-credential-provider-bin-dir=/var/snap/microk8s/common/credentialprovider/bin
--image-credential-provider-config=/var/snap/microk8s/common/credentialprovider/config.yaml
ARGS
```

**2. Install the chart:**

```bash
helm upgrade --install credential-provider-harbor \
  oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system \
  -f examples/kubernetes/microk8s/values.yaml \
  --set registry.host=harbor.example.com \
  --set registry.audience=harbor.example.com

kubectl rollout status daemonset/credential-provider-harbor -n kube-system
```

The installer restarts `snap.microk8s.daemon-kubelite`, which is what re-reads the arguments file.

**3. Confirm:**

```bash
./scripts/verify-node-install.sh
microk8s kubectl get --raw /.well-known/openid-configuration | jq -r .issuer
```

## Requirements

MicroK8s on Kubernetes 1.34 or newer. Check with `microk8s version`. Older channels do not have the service account token support this depends on.

## Snap Refreshes

A snap refresh rewrites `/var/snap/microk8s/current/args/kubelet`. The arguments you appended are lost, and pulls from Harbor start failing. `/var/snap/microk8s/common` is left alone, so the binary and config survive.

Re-applying the arguments is not enough on its own. `kubelite` reads that file once at startup, and the refresh has already restarted it with the cleaned file, so you have to restart it again:

```bash
sudo snap restart microk8s.daemon-kubelite
```

Or hold refreshes with `sudo snap refresh --hold microk8s` and take them deliberately.
