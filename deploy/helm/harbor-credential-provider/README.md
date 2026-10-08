# harbor-credential-provider

Installs the `harbor-credential-provider` kubelet plugin on every node so pods pull Harbor images with service account tokens instead of image pull secrets.

The chart runs a privileged DaemonSet. On each node it copies the credential provider binary onto the host, writes or merges the kubelet credential provider config, creates the RBAC that lets kubelets request tokens for the registry audience, points kubelet at the provider where the profile supports it, and restarts kubelet.

## Requirements

- Kubernetes 1.34 or newer. The chart declares `kubeVersion: ">=1.34.0-0"`. Service account tokens for image credential providers ([KEP-4412](https://github.com/kubernetes/enhancements/tree/master/keps/sig-auth/4412-projected-service-account-tokens-for-kubelet-image-credential-providers)) landed in 1.34.
- Nodes you can write to. GKE Autopilot is out, because it blocks privileged host access.
- **Nodes allowed to ask for your audience.** kubelet asks for a token whose audience is `registry.audience`, and the API server issues a node a token for an audience like that only if the node is authorized for `request-serviceaccounts-token-audience` on it. The chart creates that RBAC (`nodeAudienceRbac`), so there is nothing else to set up unless you turn it off. The audience does not belong in the API server's `--api-audiences`: that flag lists what the API server accepts on tokens presented to it, and adding your registry there makes a token meant for Harbor work as a cluster credential too.
- The cluster issuer added to Harbor as a Trusted Issuer, and a federated robot account whose claim rules match the tokens the cluster issues.

## Install

```bash
helm upgrade --install harbor-credential-provider \
  oci://8gears.container-registry.com/8gcr/harbor-credential-provider \
  --namespace kube-system \
  --set registry.host=harbor.example.com \
  --set profile=generic
```

`registry.host` is required. Everything else has a default.

On a cluster where a simultaneous kubelet restart on every node is not acceptable, see [First Install](#first-install) before running that command.

Watch the rollout. Pods report ready once their node is done:

```bash
kubectl rollout status daemonset/harbor-credential-provider -n kube-system
```

Then try a pull:

```bash
kubectl run test --image=harbor.example.com/library/nginx:latest --restart=Never
```

## Profiles

The profile decides the host paths and how kubelet gets told about them.

| Profile | Nodes | Binary directory | Config |
|---------|-------|------------------|--------|
| `generic` | kubeadm and other systemd nodes | `/usr/local/bin/credential-providers` | `/etc/kubernetes/credential-providers/config.yaml` |
| `eks`, `aws` | EKS AL2023. Keeps the existing ECR provider entry | `/etc/eks/image-credential-provider` | `/etc/eks/image-credential-provider/config.json` |
| `k3s`, `k3d` | k3s and k3d | `/var/lib/rancher/credentialprovider/bin` | `/var/lib/rancher/credentialprovider/config.yaml` |
| `rke2` | RKE2 server and agent nodes | `/var/lib/rancher/credentialprovider/bin` | `/var/lib/rancher/credentialprovider/config.yaml` |
| `kind` | kind node containers | `/var/lib/kubelet/credential-provider` | `/var/lib/kubelet/credential-provider-config.yaml` |
| `gke` | GKE Standard, best effort | same as `generic` | same as `generic` |
| `aks` | AKS. Patches `KUBELET_FLAGS` in `/etc/default/kubelet` | same as `generic` | same as `generic` |
| `microk8s` | MicroK8s. Edits the snap's kubelet arguments file | `/var/snap/microk8s/common/credentialprovider/bin` | `/var/snap/microk8s/common/credentialprovider/config.yaml` |
| `sks` | Exoscale SKS. Replaces the kubelet `ExecStart` from `sks.conf` with a drop-in | same as `generic` | same as `generic` |
| `custom` | Anything else | `credentialProvider.binDir` | `credentialProvider.configPath` |

`custom` requires both paths. The values schema rejects the install if either is empty.

`rke2`, `aks`, `microk8s` and `sks` each wire kubelet the way their distribution expects, because none of them reads the `KUBELET_EXTRA_ARGS` drop-in that `generic` writes. `rke2` writes a new drop-in file. `aks` and `microk8s` edit a file the distribution owns, so they fail the install when it is not there rather than reporting success on a node they did not change. `sks` copies the `ExecStart` from the SKS drop-in into one of its own that sorts after it, and fails the same way when `sks.conf` is missing or holds no `ExecStart` it can parse. Per-distribution notes are in [`examples/kubernetes/`](../../../examples/kubernetes/).

On `kind`, check that the live kubelet command line inside the node container has `--image-credential-provider-bin-dir` and `--image-credential-provider-config`. If pulls fail with `no basic auth credentials` and those flags are missing, kind did not pick up `KUBELET_EXTRA_ARGS`; reinstall with `--set kubelet.forceExecStartOverride=true`, which rewrites the kubelet `ExecStart` line instead.

## Values

### Required

| Key | Description |
|-----|-------------|
| `registry.host` | Harbor hostname workloads pull from. Also the default audience and the default `matchImages` entry. |

### Registry

| Key | Default | Description |
|-----|---------|-------------|
| `registry.audience` | `registry.host` | Token audience. Must match the audience configured in the Harbor Trusted Issuer exactly. |
| `registry.matchImages` | `[registry.host]` | Image patterns the provider answers for. |
| `registry.cacheDuration` | `1h` | How long kubelet caches credentials. |
| `registry.username` | `jwt` | Basic auth username Harbor expects alongside the token. |

### Placement and Paths

| Key | Default | Description |
|-----|---------|-------------|
| `profile` | `generic` | See the profile table above. |
| `credentialProvider.binaryName` | `harbor-credential-provider` | Name the binary gets on the host. |
| `credentialProvider.binDir` | by profile | Host directory for the binary. |
| `credentialProvider.configPath` | by profile | Host path of the credential provider config. |
| `credentialProvider.configFormat` | by profile | `yaml` or `json`. |
| `installer.hostRoot` | `/host` | Where the host filesystem is mounted in the installer container. It becomes a volume `mountPath`, so `/` and a trailing slash are refused at install time. |
| `installer.installedMarker` | `/var/lib/harbor-credential-provider/install-marker` | Host file the installer writes once a node is done, carrying the install ID the readiness probe greps for. Keep it on persistent storage: see [Readiness](#readiness). It names a file, so `/` and a trailing slash are refused at install time. |
| `installer.sleep` | `true` | Keep the pod up after installing. Leave it on: a DaemonSet restarts an exited container, and the installer would run again on every restart. |

### Kubelet

| Key | Default | Description |
|-----|---------|-------------|
| `kubelet.configure` | `true` | Point kubelet at the provider. Turn off if you manage kubelet flags yourself. |
| `kubelet.restart` | `true` | Restart kubelet after installing, so the node works without a manual roll. |
| `kubelet.serviceName` | by profile | systemd unit to restart. |
| `kubelet.systemdDropInPath` | by profile | Drop-in written for `generic`, `kind`, `gke`, `sks`, and `custom`. On `sks` its file name has to sort after `sks.conf`. |
| `kubelet.k3sConfigDropInPath` | by profile | Drop-in written for `k3s` and `k3d`. |
| `kubelet.rke2ConfigDropInPath` | by profile | Drop-in written for `rke2`. |
| `kubelet.kubeletDefaultsPath` | by profile | EnvironmentFile patched for `aks`. |
| `kubelet.microk8sArgsPath` | by profile | Arguments file edited for `microk8s`. |
| `kubelet.sksDropInPath` | by profile | SKS drop-in `sks` reads the kubelet `ExecStart` from. Never written. |
| `kubelet.forceExecStartOverride` | `false` | kind only. See the note above. |

### Workload

| Key | Default | Description |
|-----|---------|-------------|
| `image.repository` | `8gears.container-registry.com/8gcr/harbor-credential-provider-deployer` | Deployer image. |
| `image.tag` | `v` + `Chart.appVersion` | Image tag. Releases publish the image as `vX.Y.Z`, so the default adds the prefix. Set it explicitly to run a development build from `main`. |
| `image.pullPolicy` | `IfNotPresent` | |
| `imagePullSecrets` | `[]` | Only needed if the deployer image itself is private. |
| `priorityClassName` | `system-node-critical` | Image pulls depend on this component, so it should schedule ahead of ordinary workloads. |
| `updateStrategy` | `RollingUpdate`, `maxUnavailable: 1` | One node at a time, because installing restarts kubelet. The schema pins it there: `Recreate`, a larger `maxUnavailable` and a nonzero `maxSurge` are refused at install time. |
| `readinessProbe.enabled` | `true` | Reports whether the node finished installing this revision. See [Readiness](#readiness). |
| `terminationGracePeriodSeconds` | `10` | The installer has no cleanup to do. |
| `securityContext` | `privileged: true` | The installer writes to the host filesystem and restarts kubelet. The schema pins `privileged: true` and refuses the three fields that contradict it (`allowPrivilegeEscalation: false`, `runAsNonRoot: true`, a `runAsUser` other than `0`), because each of those fails on the node rather than at install time. Fields a cluster policy needs and the installer does not care about still pass. |
| `tolerations` | `[{operator: Exists}]` | Runs everywhere, control plane nodes included. |
| `nodeSelector`, `affinity` | `{}` | Narrow the rollout if you only want some nodes covered. |
| `resources` | `{}` | |
| `podLabels`, `podAnnotations` | `{}` | Added to the installer pods. Values have to be strings, as Kubernetes labels and annotations are; the schema refuses a boolean or a number, which would otherwise render a manifest the API server rejects. A `podLabels` entry cannot displace the selector labels. |
| `extraEnv` | `[]` | Extra environment for the installer, for example `PRESERVE_ECR_PROVIDER=false`. Values are strings: quote `false` rather than passing a YAML boolean. |

### RBAC

| Key | Default | Description |
|-----|---------|-------------|
| `nodeAudienceRbac.create` | `true` | Create the ClusterRole and binding that let kubelets request tokens for `registry.audience`. |
| `nodeAudienceRbac.groups` | `[system:nodes]` | Who gets that permission. |
| `serviceAccount.create` | `true` | |
| `serviceAccount.name` | generated | |

## Readiness

The installer writes a marker file on the host once a node is done, and the readiness probe watches it. The marker survives the pod, so it means "this node has the provider installed" rather than "this pod installed it".

The marker names the install it belongs to. The chart hashes the deployer image and every environment variable that decides what lands on the node into an install ID, passes it to the installer as `INSTALL_ID`, and the probe greps the marker for that exact ID. The installer deletes the marker when it starts and rewrites it only after the kubelet restart returns. It also records whether kubelet was restarted, which is what lets a node installed with `kubelet.restart=false` pick the flags up later even when nothing else about the install has changed.

That ordering is what `kubectl rollout status` rests on. A pod is Ready only once it has finished its own install, so a marker left by the previous revision cannot make a new pod look done, a crash-looping installer keeps its node unready instead of passing on a stale file, and `maxUnavailable: 1` really does hold the rollout on one node until that node is finished. Ready is the install talking, not kubelet: `systemctl restart` returns once systemd has the unit up again, which is a moment before kubelet has finished starting and re-registering the node.

A node whose last completed install ID differs from the current one gets a kubelet restart even when every host file already matches, because the running kubelet has never been started against this configuration. Repeating `helm upgrade` with unchanged values changes neither the install ID nor the pod template, so it restarts nothing.

That makes the marker's location load-bearing. It defaults to `/var/lib/harbor-credential-provider/install-marker` and gets its own hostPath mount, because the `installer.hostRoot` mount is the node's root filesystem without its submounts. Do not move it under `/var/run` or `/run`: those are the same tmpfs on a systemd host and are emptied at every boot. A node that loses its marker on reboot comes back with the drop-in already in effect and kubelet already started against it, but reads as never installed, so the installer restarts kubelet again and the node sits `NotReady` for the length of that restart on every single boot.

The install ID covers the deployer image and the environment variables the installer reads. `extraEnv` entries it does not read, a proxy setting for instance, stay out of the hash, so setting one does not roll a kubelet restart across the fleet for a node install that is byte for byte the same. Entries that override a variable the installer does read, `PRESERVE_ECR_PROVIDER` among them, do change the ID, because they change what lands on the node.

## Upgrading

An upgrade is a normal DaemonSet rolling update: one node at a time, each node Ready before the next pod starts.

```bash
kubectl rollout status daemonset/harbor-credential-provider -n kube-system
```

## First Install

`maxUnavailable` bounds updates, not creation. When the DaemonSet is created, the controller schedules an installer pod on every eligible node at once, and with the default `kubelet.restart=true` those pods restart kubelet across the cluster at roughly the same time. Readiness does not help either: it gates how a rolling *update* advances, and there is no update here. Nothing in the DaemonSet API bounds pod creation, so the chart cannot serialize the first install for you, whatever it puts in `updateStrategy`. Kubelet restarts are quick and running containers survive them, but on a large cluster the nodes do go `NotReady` together for a few seconds.

`helm install` prints this same warning in its notes whenever `kubelet.restart` is on, so it is hard to walk into by accident. Two ways to avoid it, both using values the chart already has.

Install without restarting, then roll the restart out as an upgrade:

```bash
helm upgrade --install harbor-credential-provider <chart> \
  --namespace kube-system --set registry.host=harbor.example.com \
  --set kubelet.restart=false

helm upgrade harbor-credential-provider <chart> \
  --namespace kube-system --set registry.host=harbor.example.com \
  --set kubelet.restart=true
```

The first command copies the binary, writes the config and the kubelet drop-in on every node and touches no running kubelet. The second changes `RESTART_KUBELET`, which changes the install ID, so it is a rolling update and `maxUnavailable: 1` restarts the kubelets one node at a time.

Or install onto labelled batches of nodes and widen as each batch settles:

```bash
kubectl label node node-1 node-2 harbor-credential-provider=install
helm upgrade --install harbor-credential-provider <chart> \
  --namespace kube-system --set registry.host=harbor.example.com \
  --set nodeSelector.harbor-credential-provider=install
```

Once those nodes are Ready, label the next batch. Labelling a node creates its installer pod, which is the unbounded case again within that batch, so size the batches for what a simultaneous restart across them costs you. Drop `nodeSelector` when every node is covered.

## Upgrading from credential-provider-harbor

The chart, the binary, the image and the provider entry were all called `credential-provider-harbor` before. The name now reads the same way round as `ecr-credential-provider`, `acr-credential-provider` and the Talos extension, which sit beside it on a node.

Keep your release name. Helm does not require it to match the chart, and reusing it makes this a rolling update of one DaemonSet:

```bash
helm upgrade credential-provider-harbor \
  oci://8gears.container-registry.com/8gcr/harbor-credential-provider \
  --namespace kube-system \
  -f values.yaml
```

Installing under the new release name instead leaves the old release in place, and the cluster ends up running two DaemonSets that both install to every node. If you want the release renamed too, `helm uninstall credential-provider-harbor` first, then install. The nodes keep their files in between, so pulls carry on working across the gap.

The installer migrates each node as its pod comes up: it drops the provider entry under the old name before writing the new one, and deletes the old binary after kubelet has restarted. Both steps are skipped if you have pinned `credentialProvider.binaryName` back to `credential-provider-harbor`, which stays supported.

Nothing needs doing by hand, and nothing is left behind that kubelet still calls.

## Uninstalling

```bash
helm uninstall harbor-credential-provider -n kube-system
```

The release name is whatever you installed under. An upgrade that kept the old one, which is what [Upgrading from credential-provider-harbor](#upgrading-from-credential-provider-harbor) recommends, is still called `credential-provider-harbor`; `helm list -n kube-system` settles it.

Removes the DaemonSet, the ServiceAccount and the RBAC. Touches nothing on the nodes, and is the whole job if the nodes are about to be replaced.

What the installer wrote stays: the binary, the provider entry, the kubelet configuration where one was written, and the marker. Kubelet keeps calling the provider until its config stops naming it.

### What stays on a node

Profile defaults. `credentialProvider.binDir`, `credentialProvider.configPath` and `installer.installedMarker` move them.

| Profile | Binary | Credential provider config | Kubelet configuration |
|---------|--------|----------------------------|-----------------------|
| `generic`, `custom`, `gke` | `/usr/local/bin/credential-providers/harbor-credential-provider` | `/etc/kubernetes/credential-providers/config.yaml` | `/etc/systemd/system/kubelet.service.d/99-harbor-credential-provider.conf` |
| `eks`, `aws` | `/etc/eks/image-credential-provider/harbor-credential-provider` | `/etc/eks/image-credential-provider/config.json` | none: the AMI carries the flags, so EKS installs with `kubelet.configure=false` |
| `k3s`, `k3d` | `/var/lib/rancher/credentialprovider/bin/harbor-credential-provider` | `/var/lib/rancher/credentialprovider/config.yaml` | `/etc/rancher/k3s/config.yaml.d/99-harbor-credential-provider.yaml` |
| `rke2` | `/var/lib/rancher/credentialprovider/bin/harbor-credential-provider` | `/var/lib/rancher/credentialprovider/config.yaml` | `/etc/rancher/rke2/config.yaml.d/99-harbor-credential-provider.yaml` |
| `kind` | `/var/lib/kubelet/credential-provider/harbor-credential-provider` | `/var/lib/kubelet/credential-provider-config.yaml` | `/etc/systemd/system/kubelet.service.d/99-harbor-credential-provider.conf` |
| `aks` | `/usr/local/bin/credential-providers/harbor-credential-provider` | `/etc/kubernetes/credential-providers/config.yaml` | that drop-in, plus a `KUBELET_FLAGS` edit in `/etc/default/kubelet` |
| `microk8s` | `/var/snap/microk8s/common/credentialprovider/bin/harbor-credential-provider` | `/var/snap/microk8s/common/credentialprovider/config.yaml` | an argument in `/var/snap/microk8s/current/args/kubelet` |
| `sks` | `/usr/local/bin/credential-providers/harbor-credential-provider` | `/etc/kubernetes/credential-providers/config.yaml` | `/etc/systemd/system/kubelet.service.d/zz-harbor-credential-provider.conf` |

Kubelet configuration exists only where the install ran with `kubelet.configure=true`, the default everywhere except EKS. The marker is `/var/lib/harbor-credential-provider/install-marker` on every profile.

### Cleaning a node

Entry out, restart, then delete.

Kubelet reads the credential provider config at startup, so it keeps exec'ing the provider until a restart. Delete the binary first and pulls from that one registry fail against a missing file, which reads like a registry outage.

On a `generic` node, the chart's default:

```bash
# 1. Remove the harbor-credential-provider entry from the providers list.
sudo "${EDITOR:-vi}" /etc/kubernetes/credential-providers/config.yaml

# 2. Remove the kubelet drop-in and reload, so the restart comes up without
#    the flags.
sudo rm -f /etc/systemd/system/kubelet.service.d/99-harbor-credential-provider.conf
sudo systemctl daemon-reload

# 3. Kubelet reads both only at startup.
sudo systemctl restart kubelet

# 4. Nothing calls it now.
sudo rm -f /usr/local/bin/credential-providers/harbor-credential-provider
sudo rm -f /var/lib/harbor-credential-provider/install-marker
```

Substitute the paths for your profile from the table above. What changes by profile:

- **Step 2 does not apply on EKS.** The AMI supplies the flags, so no drop-in was written.
- **k3s and RKE2** keep that configuration in `config.yaml.d` rather than a systemd drop-in, and step 3 restarts the supervisor: whichever of `k3s`/`k3s-agent` or `rke2-server`/`rke2-agent` the node runs.
- **AKS** has a `KUBELET_FLAGS` edit in `/etc/default/kubelet` on top of the drop-in.
- **MicroK8s** keeps kubelet arguments in `/var/snap/microk8s/current/args/kubelet`, and the unit is `snap.microk8s.daemon-kubelite`.
- **SKS** names its drop-in `zz-harbor-credential-provider.conf`. Removing it puts the `ExecStart` from `sks.conf` back.

Deleting the config file outright works where the provider is its only entry. Not on EKS, where it is shared with `ecr-credential-provider`: remove that entry and the AWS add-ons stop pulling.

The per-distribution pages under [`examples/kubernetes/`](../../../examples/kubernetes/) carry these steps already filled in.

### Nodes upgraded from the old name

The installer's migration covers the two things that would otherwise conflict: the provider entry under the old name, and the old binary. It does not remove what the old name left elsewhere, because nothing calls those and removing files a running kubelet might still reference is not something an installer should do unprompted.

So on a node that was installed under `credential-provider-harbor` and upgraded, the paths to clean are the old ones as well as the ones in the table:

```bash
# The marker, on every profile.
sudo rm -rf /var/lib/credential-provider-harbor

# Wherever the old install wrote kubelet configuration, by profile:
sudo rm -f /etc/systemd/system/kubelet.service.d/99-credential-provider-harbor.conf   # generic, custom, gke, kind, aks
sudo rm -f /etc/rancher/k3s/config.yaml.d/99-credential-provider-harbor.yaml          # k3s, k3d
sudo rm -f /etc/rancher/rke2/config.yaml.d/99-credential-provider-harbor.yaml         # rke2
```

EKS writes none of these, so there is only the marker to remove there.

Those files are inert once the new install has written its replacement kubelet configuration. Every one of them points at the same binary directory and config path as its replacement, because the rename did not move them: systemd reads the old drop-in before `99-harbor-credential-provider.conf` alphabetically and the later one wins, and the k3s and RKE2 drop-ins hand the embedded kubelet the same two `kubelet-arg` values twice over. The old marker is only ever read by an installer running under the old name. On such a node they are leftovers to tidy, not a reason to hurry.

That holds only where a replacement was written. With `kubelet.configure=false`, or a custom path that put the new configuration somewhere else, the new install leaves no file for the old one to lose to, and the old drop-in is still the live kubelet configuration. Remove it, or rewrite it for the new install, before treating the upgrade as done.

### Replacing the node instead

`helm uninstall`, then roll the node group. Replacements come up clean: no per-node editing, no kubelet restart to schedule.

## Harbor Side

The cluster half is only half the setup. In Harbor, configure a Trusted Issuer with the cluster's issuer URL and JWKS, then create a federated robot account whose claim rules match the tokens your cluster mints, usually on `iss`, `aud`, and `sub`. The audience there and `registry.audience` here have to be the same string. The walkthrough is [Federated Identity Provider for Workload Authentication](https://container-registry.com/docs/2.16/administration-manual/authentication-management/system-robot-accounts/federated-identity-provider-for-workload-authentication/).
