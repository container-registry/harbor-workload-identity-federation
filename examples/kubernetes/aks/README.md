# Azure Kubernetes Service

Partly supported. The chart installs the binary and the credential provider config onto AKS nodes. It cannot point kubelet at them, so there is a manual node step.

## Why the Chart Cannot Finish the Job

The installer's `generic` profile writes a systemd drop-in that sets `KUBELET_EXTRA_ARGS`. That works on kubeadm nodes because the kubeadm unit expands `$KUBELET_EXTRA_ARGS`. The AKS kubelet unit does not. It builds its command line from `$KUBELET_FLAGS`, which comes from `/etc/default/kubelet`, so a drop-in setting `KUBELET_EXTRA_ARGS` is written, is valid, and does nothing.

AKS also has no supported API for adding arbitrary kubelet flags. [Custom node configuration](https://learn.microsoft.com/en-us/azure/aks/custom-node-configuration) covers a fixed list of settings, and the two image credential provider flags are not on it.

## What To Do

**1. Install the chart with kubelet wiring off:**

```bash
helm upgrade --install credential-provider-harbor \
  oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system \
  -f examples/kubernetes/aks/values.yaml \
  --set registry.host=harbor.example.com \
  --set registry.audience=harbor.example.com

kubectl rollout status daemonset/credential-provider-harbor -n kube-system
```

**2. Add the flags on each node** with [`patch-kubelet-flags.sh`](patch-kubelet-flags.sh). It appends to `KUBELET_FLAGS`, keeps a backup, and restarts kubelet. It does nothing on a second run.

```bash
kubectl debug node/<node> -it --image=busybox -- chroot /host sh
# then run the script contents
```

For a whole node pool, the durable way is a VMSS custom script extension or a custom node image, so that scale-up nodes come up already configured. Nodes created after you ran the script by hand will not have it.

**3. Confirm:**

```bash
./scripts/verify-node-install.sh
```

## The Scale-Up Problem

Every AKS node image upgrade and every scale-out event produces a node without the kubelet flags. The DaemonSet reinstalls the binary and config, but the flags stay missing until someone runs the script again. Treat this as "works, with an operational cost" rather than "supported", until either AKS exposes the flags or the installer learns to patch `/etc/default/kubelet`.

## Contributing

An `aks` profile would fix this properly: a `configureKubeletDefaults` branch in the installer that appends to `KUBELET_FLAGS` in `/etc/default/kubelet`, which is what the script above does by hand. That is a small change next to `configureSystemdKubelet`. See [CONTRIBUTING.md](../../../CONTRIBUTING.md).
