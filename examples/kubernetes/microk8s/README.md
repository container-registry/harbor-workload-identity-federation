# MicroK8s

One `helm install`, with a caveat about snap refreshes at the bottom of this page.

MicroK8s runs kubelet inside `kubelite`, from a snap. Its arguments live in `/var/snap/microk8s/current/args/kubelet`, one per line, not in a systemd drop-in and not on a command line. The `microk8s` profile appends the two credential provider arguments to that file and restarts `snap.microk8s.daemon-kubelite`, which is what re-reads it.

## Paths

The snap's confinement makes `/usr/local/bin` a poor place for the binary. The profile puts it under the snap's own writable tree, `/var/snap/microk8s/common`, which a snap refresh leaves alone.

## Requirements

MicroK8s on Kubernetes 1.34 or newer. Check with `microk8s version`. Older channels do not have the service account token support this depends on.

## Install

```bash
helm upgrade --install credential-provider-harbor \
  oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system \
  -f examples/kubernetes/microk8s/values.yaml \
  --set registry.host=harbor.example.com \
  --set registry.audience=harbor.example.com

kubectl rollout status daemonset/credential-provider-harbor -n kube-system
```

## Confirm

```bash
./scripts/verify-node-install.sh
microk8s kubectl get --raw /.well-known/openid-configuration | jq -r .issuer
```

## Snap Refreshes

This is the part to plan for. A snap refresh rewrites `/var/snap/microk8s/current/args/kubelet` from the new revision's defaults, so the two arguments are lost and pulls from Harbor start failing. `/var/snap/microk8s/common` is untouched, so the binary and the config survive; only the arguments go.

The DaemonSet does not notice. It installs once when its pod starts and then stays up, so a refresh that happens afterwards leaves it running next to a kubelet that no longer has the flags. Re-apply by restarting the pods:

```bash
kubectl rollout restart daemonset/credential-provider-harbor -n kube-system
```

That rewrites the arguments file and restarts `kubelite`, which matters: `kubelite` reads the file once at startup, and the refresh has already restarted it with the cleaned file.

For clusters where that is too easy to forget, hold refreshes and take them deliberately:

```bash
sudo snap refresh --hold microk8s
```

## If the Install Fails

The installer stops with an error when `/var/snap/microk8s/current/args/kubelet` is not there, rather than reporting success on a node it did not change. That path exists on any node with MicroK8s installed, so its absence means the profile is pointed at the wrong node.
