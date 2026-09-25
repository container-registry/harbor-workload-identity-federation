# End-to-end install tests

Each run stands up a cluster of one Kubernetes distribution, installs the chart
from this checkout into it, and checks that the nodes came out able to pull from
Harbor. The assertion that matters is
[`scripts/verify-node-install.sh`](../verify-node-install.sh), which reads the
live kubelet arguments on every node. A DaemonSet that rolled out is not the
same thing as a kubelet that was told about the provider, and the gap between
the two is what this component exists to close.

Harbor itself is not part of these tests. They install a provider config that
names `harbor.example.com` and stop there; whether a pull against a real Harbor
succeeds is the manual test in each
[`examples/kubernetes/<distro>/README.md`](../../examples/kubernetes).

## Running one

```bash
scripts/e2e/run.sh <distro> [pinned|latest]
```

```bash
scripts/e2e/run.sh k3d                  # latest, the default
scripts/e2e/run.sh kind pinned          # the oldest Kubernetes this supports
E2E_KEEP=1 scripts/e2e/run.sh k3d       # leave the cluster up to poke at
```

`run.sh` builds the deployer image from the checkout, hands it to the cluster,
installs the chart with `image.pullPolicy=Never`, and tears the cluster down on
the way out. So a run tests the branch, not a release.

The two channels answer different questions. `pinned` uses the versions in
[`versions.env`](versions.env), the lowest Kubernetes this component supports:
service account tokens for credential providers landed in 1.34, so nothing
below it can work. `latest` pins nothing and takes each tool's own default,
which is how an upstream change that breaks the install shows up here rather
than in somebody's cluster.

## The distributions

| Distro | Profile | Cluster |
|---|---|---|
| [`kind`](clusters/kind.sh) | `kind` | containers running systemd |
| [`k3d`](clusters/k3d.sh) | `k3d` | k3s in containers |
| [`k3s`](clusters/k3s.sh) | `k3s` | installed on the machine |
| [`rke2`](clusters/rke2.sh) | `rke2` | installed on the machine |
| [`microk8s`](clusters/microk8s.sh) | `microk8s` | snap on the machine |
| [`kubeadm`](clusters/kubeadm.sh) | `generic` | installed on the machine |

kind installs with `--set kubelet.forceExecStartOverride=true`, without which
kubelet comes back up ignoring the flags the drop-in set
([issue #33](https://github.com/container-registry/harbor-workload-identity-federation/issues/33)).
k3d installs with `--set kubelet.restart=false` and restarts the cluster
itself, because a k3d node has no init system for the installer to ask and the
attempt crashloops the DaemonSet. That is [issue #32](https://github.com/container-registry/harbor-workload-identity-federation/issues/32),
not a property of the test.

> [!WARNING]
> The bottom four install a Kubernetes distribution onto the machine that runs
> them and restart its kubelet. They refuse to run unless `CI=true` or
> `E2E_ALLOW_HOST_INSTALL=1` is set. Use a throwaway VM.

EKS, GKE, AKS, OpenShift and Talos are not here. They need an account or a node
reboot, so they are tested by hand against a release; each one's README has the
steps. `kubeadm` covers the `generic` profile that a self-managed cluster uses,
which is the closest automated stand-in.

## Environment

| Variable | Default | |
|---|---|---|
| `E2E_K8S_CHANNEL` | `latest` | `pinned` or `latest`; `run.sh`'s second argument sets it |
| `E2E_KEEP` | unset | leave the cluster up after the test |
| `E2E_ALLOW_HOST_INSTALL` | unset | permit the distros that install on the machine |
| `E2E_CLUSTER_NAME` | `cph-e2e` | |
| `E2E_KUBECONFIG` | `$TMPDIR/cph-e2e-kubeconfig` | never your own `~/.kube/config` |
| `E2E_REGISTRY_HOST` | `harbor.example.com` | the host and audience written into the config |
| `E2E_IMAGE_REPO`, `E2E_IMAGE_TAG` | `localhost/credential-provider-harbor-deployer`, `e2e` | |

## Adding a distribution

Write `clusters/<name>.sh` answering four words: `up`, `load`, `down` and
`profile`. `up` leaves a working `kubectl` in `$KUBECONFIG`, `load` puts
`$E2E_IMAGE` where that cluster's kubelet can find it, `profile` prints the
chart profile to install with.

Two more are optional, and default to doing nothing in
[`lib/common.sh`](lib/common.sh). `install-args` prints extra `--set` flags the
distro needs, and `restart-nodes` runs between the install and the
verification for a distro whose kubelet the installer cannot restart from
inside the cluster. kind uses the first, k3d uses both.

Everything after that is
[`install-test.sh`](install-test.sh), which is distro-agnostic on purpose. Then
add the name to the matrix in
[`.github/workflows/e2e.yml`](../../.github/workflows/e2e.yml).
