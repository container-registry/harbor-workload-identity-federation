# Google Kubernetes Engine

Works on GKE Standard. Does not work on GKE Autopilot, which blocks the privileged host access the installer needs, and does not let you change kubelet.

Read the "What GKE Undoes" section before rolling this out to anything you care about.

## Node Image Matters

| Node image | Profile | Why |
|------------|---------|-----|
| Ubuntu | `generic` | `/usr/local/bin` is writable and executable, kubelet reads `KUBELET_EXTRA_ARGS`. |
| Container-Optimized OS | `custom` | `/usr` is read-only and mounted `noexec`. The binary has to go on the stateful partition, under `/home/kubernetes/bin`. |

```bash
# Ubuntu node pools
helm upgrade --install credential-provider-harbor \
  oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system -f examples/kubernetes/gke/values.yaml \
  --set registry.host=harbor.example.com --set registry.audience=harbor.example.com

# COS node pools
helm upgrade --install credential-provider-harbor \
  oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system -f examples/kubernetes/gke/cos-values.yaml \
  --set registry.host=harbor.example.com --set registry.audience=harbor.example.com
```

Both values files carry a `nodeSelector` on `cloud.google.com/gke-os-distribution`, so each one stays on the node image it was written for. Without it, the `generic` profile would put the binary in `/usr/local/bin` on a COS node, where `/usr` is `noexec`, and pulls would fail there.

The label values are `ubuntu_containerd` and `cos_containerd`, the node image types GKE has used since 1.24 removed the Docker runtime. The older `ubuntu` and `cos` values match nothing on a current cluster, and a selector that matches nothing installs on no nodes without reporting an error. Check what your pools actually carry before installing:

```bash
kubectl get nodes -L cloud.google.com/gke-os-distribution
```

The two commands above still install the *same* release, so running both just upgrades the first. A cluster with both node images needs two releases with different names:

```bash
helm upgrade --install cph-ubuntu oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system -f examples/kubernetes/gke/values.yaml \
  --set registry.host=harbor.example.com --set registry.audience=harbor.example.com

helm upgrade --install cph-cos oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system -f examples/kubernetes/gke/cos-values.yaml \
  --set registry.host=harbor.example.com --set registry.audience=harbor.example.com
```

If your pools do not carry `cloud.google.com/gke-os-distribution`, edit the `nodeSelector` in the values file. `--set` merges into it rather than replacing it, so adding a label on the command line leaves the GKE one in place; `--set nodeSelector=null` drops it entirely.

## What GKE Undoes

GKE treats node filesystem changes as disposable. Node auto-upgrade, node auto-repair, and any pool resize replace the node, and the replacement comes up without the binary, the config, or the kubelet drop-in. The DaemonSet reinstalls when its pod lands there, so the node heals itself, but pulls from Harbor fail in the gap.

That is why the chart calls this profile best effort. If you need Harbor pulls to work from the first second of a node's life, put the binary and the config into a [custom node image](https://cloud.google.com/kubernetes-engine/docs/how-to/node-system-config) or a node startup script instead, and keep the chart only for config drift.

## Harbor Side

GKE's issuer is `https://container.googleapis.com/v1/projects/<project>/locations/<location>/clusters/<cluster>`:

```bash
kubectl get --raw /.well-known/openid-configuration | jq -r .issuer
```

It is publicly reachable, so the Harbor Trusted Issuer can validate online.

## Check It Took

```bash
./scripts/verify-node-install.sh
```

On COS, expect `--image-credential-provider-bin-dir=/home/kubernetes/bin/credential-providers`.
