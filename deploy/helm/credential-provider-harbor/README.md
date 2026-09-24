# credential-provider-harbor

Installs the `credential-provider-harbor` kubelet plugin on every node so pods pull Harbor images with service account tokens instead of image pull secrets.

The chart runs a privileged DaemonSet. On each node it copies the credential provider binary onto the host, writes or merges the kubelet credential provider config, creates the RBAC that lets kubelets request tokens for the registry audience, points kubelet at the provider where the profile supports it, and restarts kubelet.

## Requirements

- Kubernetes 1.34 or newer. The chart declares `kubeVersion: ">=1.34.0-0"`. Service account tokens for image credential providers ([KEP-4412](https://github.com/kubernetes/enhancements/tree/master/keps/sig-auth/4412-projected-service-account-tokens-for-kubelet-image-credential-providers)) landed in 1.34.
- Nodes you can write to. GKE Autopilot is out, because it blocks privileged host access.
- **The API server has to accept your audience.** kubelet asks for a token whose audience is `registry.audience`, and the API server only mints audiences listed in `--api-audiences`. If yours is not there, the request is refused before the provider is ever called. This is a cluster-level flag, and the chart cannot set it. On k3s it goes in `kube-apiserver-arg`, on kind in a `kubeadmConfigPatches` block, on managed clusters it is whatever your provider exposes. The chart creates the matching RBAC (`nodeAudienceRbac`); the flag is yours.
- A Harbor Federated Identity Provider configured for the cluster issuer, and a federated robot account whose claim rules match the tokens the cluster issues.

## Install

```bash
helm upgrade --install credential-provider-harbor \
  oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system \
  --set registry.host=harbor.example.com \
  --set profile=generic
```

`registry.host` is required. Everything else has a default.

On a cluster where a simultaneous kubelet restart on every node is not acceptable, see [First Install](#first-install) before running that command.

Watch the rollout. Pods report ready once their node is done:

```bash
kubectl rollout status daemonset/credential-provider-harbor -n kube-system
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
| `kind` | kind node containers | `/var/lib/kubelet/credential-provider` | `/var/lib/kubelet/credential-provider-config.yaml` |
| `gke` | GKE Standard, best effort | same as `generic` | same as `generic` |
| `custom` | Anything else | `credentialProvider.binDir` | `credentialProvider.configPath` |

`custom` requires both paths. The values schema rejects the install if either is empty.

On `kind`, check that the live kubelet command line inside the node container has `--image-credential-provider-bin-dir` and `--image-credential-provider-config`. If pulls fail with `no basic auth credentials` and those flags are missing, kind did not pick up `KUBELET_EXTRA_ARGS`; reinstall with `--set kubelet.forceExecStartOverride=true`, which rewrites the kubelet `ExecStart` line instead.

## Values

### Required

| Key | Description |
|-----|-------------|
| `registry.host` | Harbor hostname workloads pull from. Also the default audience and the default `matchImages` entry. |

### Registry

| Key | Default | Description |
|-----|---------|-------------|
| `registry.audience` | `registry.host` | Token audience. Must match the audience configured in the Harbor Federated IDP exactly. |
| `registry.matchImages` | `[registry.host]` | Image patterns the provider answers for. |
| `registry.cacheDuration` | `1h` | How long kubelet caches credentials. |
| `registry.username` | `jwt` | Basic auth username Harbor expects alongside the token. |

### Placement and Paths

| Key | Default | Description |
|-----|---------|-------------|
| `profile` | `generic` | See the profile table above. |
| `credentialProvider.binaryName` | `credential-provider-harbor` | Name the binary gets on the host. |
| `credentialProvider.binDir` | by profile | Host directory for the binary. |
| `credentialProvider.configPath` | by profile | Host path of the credential provider config. |
| `credentialProvider.configFormat` | by profile | `yaml` or `json`. |
| `installer.hostRoot` | `/host` | Where the host filesystem is mounted in the installer container. It becomes a volume `mountPath`, so `/` and a trailing slash are refused at install time. |
| `installer.installedMarker` | `/var/lib/credential-provider-harbor/install-marker` | Host file the installer writes once a node is done, carrying the install ID the readiness probe greps for. Keep it on persistent storage: see [Readiness](#readiness). It names a file, so `/` and a trailing slash are refused at install time. |
| `installer.sleep` | `true` | Keep the pod up after installing. Leave it on: a DaemonSet restarts an exited container, and the installer would run again on every restart. |

### Kubelet

| Key | Default | Description |
|-----|---------|-------------|
| `kubelet.configure` | `true` | Point kubelet at the provider. Turn off if you manage kubelet flags yourself. |
| `kubelet.restart` | `true` | Restart kubelet after installing, so the node works without a manual roll. |
| `kubelet.serviceName` | by profile | systemd unit to restart. |
| `kubelet.systemdDropInPath` | by profile | Drop-in written for `generic`, `kind`, `gke`, and `custom`. |
| `kubelet.k3sConfigDropInPath` | by profile | Drop-in written for `k3s` and `k3d`. |
| `kubelet.forceExecStartOverride` | `false` | kind only. See the note above. |

### Workload

| Key | Default | Description |
|-----|---------|-------------|
| `image.repository` | `8gears.container-registry.com/8gcr/credential-provider-harbor-deployer` | Deployer image. |
| `image.tag` | `v` + `Chart.appVersion` | Image tag. Releases publish the image as `vX.Y.Z`, so the default adds the prefix. Set it explicitly to run a development build from `main`. |
| `image.pullPolicy` | `IfNotPresent` | |
| `imagePullSecrets` | `[]` | Only needed if the deployer image itself is private. |
| `priorityClassName` | `system-node-critical` | Image pulls depend on this component, so it should schedule ahead of ordinary workloads. |
| `updateStrategy` | `RollingUpdate`, `maxUnavailable: 1` | One node at a time, because installing restarts kubelet. The schema pins it there: `Recreate`, a larger `maxUnavailable` and any `maxSurge` are refused at install time. |
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

The marker names the install it belongs to. The chart hashes the deployer image and every environment variable that decides what lands on the node into an install ID, passes it to the installer as `INSTALL_ID`, and the probe greps the marker for that exact ID. The installer deletes the marker when it starts and rewrites it only after the kubelet restart returns.

That ordering is what `kubectl rollout status` rests on. A pod is Ready only once it has finished its own install, so a marker left by the previous revision cannot make a new pod look done, a crash-looping installer keeps its node unready instead of passing on a stale file, and `maxUnavailable: 1` really does hold the rollout on one node until that node is finished. Ready is the install talking, not kubelet: `systemctl restart` returns once systemd has the unit up again, which is a moment before kubelet has finished starting and re-registering the node.

A node whose last completed install ID differs from the current one gets a kubelet restart even when every host file already matches, because the running kubelet has never been started against this configuration. Repeating `helm upgrade` with unchanged values changes neither the install ID nor the pod template, so it restarts nothing.

That makes the marker's location load-bearing. It defaults to `/var/lib/credential-provider-harbor/install-marker` and gets its own hostPath mount, because the `installer.hostRoot` mount is the node's root filesystem without its submounts. Do not move it under `/var/run` or `/run`: those are the same tmpfs on a systemd host and are emptied at every boot. A node that loses its marker on reboot comes back with the drop-in already in effect and kubelet already started against it, but reads as never installed, so the installer restarts kubelet again and the node sits `NotReady` for the length of that restart on every single boot.

The install ID covers the deployer image and the environment variables the installer reads. `extraEnv` entries it does not read, a proxy setting for instance, stay out of the hash, so setting one does not roll a kubelet restart across the fleet for a node install that is byte for byte the same. Entries that override a variable the installer does read, `PRESERVE_ECR_PROVIDER` among them, do change the ID, because they change what lands on the node.

## Upgrading

An upgrade is a normal DaemonSet rolling update: one node at a time, each node Ready before the next pod starts.

```bash
kubectl rollout status daemonset/credential-provider-harbor -n kube-system
```

## First Install

`maxUnavailable` bounds updates, not creation. When the DaemonSet is created, the controller schedules an installer pod on every eligible node at once, and with the default `kubelet.restart=true` those pods restart kubelet across the cluster at roughly the same time. Readiness does not help either: it gates how a rolling *update* advances, and there is no update here. Nothing in the DaemonSet API bounds pod creation, so the chart cannot serialize the first install for you, whatever it puts in `updateStrategy`. Kubelet restarts are quick and running containers survive them, but on a large cluster the nodes do go `NotReady` together for a few seconds.

`helm install` prints this same warning in its notes whenever `kubelet.restart` is on, so it is hard to walk into by accident. Two ways to avoid it, both using values the chart already has.

Install without restarting, then roll the restart out as an upgrade:

```bash
helm upgrade --install credential-provider-harbor <chart> \
  --namespace kube-system --set registry.host=harbor.example.com \
  --set kubelet.restart=false

helm upgrade credential-provider-harbor <chart> \
  --namespace kube-system --set registry.host=harbor.example.com \
  --set kubelet.restart=true
```

The first command copies the binary, writes the config and the kubelet drop-in on every node and touches no running kubelet. The second changes `RESTART_KUBELET`, which changes the install ID, so it is a rolling update and `maxUnavailable: 1` restarts the kubelets one node at a time.

Or install onto labelled batches of nodes and widen as each batch settles:

```bash
kubectl label node node-1 node-2 credential-provider-harbor=install
helm upgrade --install credential-provider-harbor <chart> \
  --namespace kube-system --set registry.host=harbor.example.com \
  --set nodeSelector.credential-provider-harbor=install
```

Once those nodes are Ready, label the next batch. Labelling a node creates its installer pod, which is the unbounded case again within that batch, so size the batches for what a simultaneous restart across them costs you. Drop `nodeSelector` when every node is covered.

## Uninstalling

`helm uninstall` removes the DaemonSet, the ServiceAccount, and the RBAC. It does not undo what the installer wrote on the nodes: the binary, the credential provider config, the kubelet drop-in, and the marker file stay, and kubelet keeps calling the provider. Remove those by hand, or with a one-shot job, before you consider a node clean. A leftover marker cannot mislead a later install, because the installer deletes it before it starts work, but it is one more file on the node.

## Harbor Side

The cluster half is only half the setup. In Harbor, configure a Federated Identity Provider with the cluster's issuer URL and JWKS, then create a federated robot account whose claim rules match the tokens your cluster mints, usually on `iss`, `aud`, and `sub`. The audience there and `registry.audience` here have to be the same string. See https://container-registry.com/docs/ for the Harbor-side walkthrough.
