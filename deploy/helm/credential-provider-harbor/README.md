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
| `installer.hostRoot` | `/host` | Where the host filesystem is mounted in the installer container. |
| `installer.installedMarker` | `/var/run/credential-provider-harbor-installed` | Host file the installer touches when a node is done. The readiness probe watches it. |
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
| `updateStrategy` | `RollingUpdate`, `maxUnavailable: 1` | One node at a time, because installing restarts kubelet. |
| `readinessProbe.enabled` | `true` | Reports whether the node finished installing. |
| `terminationGracePeriodSeconds` | `10` | The installer has no cleanup to do. |
| `securityContext` | `privileged: true` | The installer writes to the host filesystem and restarts kubelet. Relaxing this breaks it. |
| `tolerations` | `[{operator: Exists}]` | Runs everywhere, control plane nodes included. |
| `nodeSelector`, `affinity` | `{}` | Narrow the rollout if you only want some nodes covered. |
| `resources` | `{}` | |
| `extraEnv` | `[]` | Extra environment for the installer, for example `PRESERVE_ECR_PROVIDER=false`. |

### RBAC

| Key | Default | Description |
|-----|---------|-------------|
| `nodeAudienceRbac.create` | `true` | Create the ClusterRole and binding that let kubelets request tokens for `registry.audience`. |
| `nodeAudienceRbac.groups` | `[system:nodes]` | Who gets that permission. |
| `serviceAccount.create` | `true` | |
| `serviceAccount.name` | generated | |

## Upgrading

The installer writes a marker file on the host when a node is done, and the readiness probe watches it. The marker survives the pod, which is what makes it mean "this node has the provider installed" rather than "this pod installed it".

The cost is that on an upgrade a node reports ready as soon as the new pod starts, before the new installer has finished writing anything. `kubectl rollout status` returns early. On a first install it means what it says; on an upgrade, read the installer logs to confirm the new version landed:

```bash
kubectl logs -n kube-system -l app.kubernetes.io/name=credential-provider-harbor --tail=20
```

## Uninstalling

`helm uninstall` removes the DaemonSet, the ServiceAccount, and the RBAC. It does not undo what the installer wrote on the nodes: the binary, the credential provider config, and the kubelet drop-in stay, and kubelet keeps calling the provider. Remove those by hand, or with a one-shot job, before you consider a node clean.

## Harbor Side

The cluster half is only half the setup. In Harbor, configure a Federated Identity Provider with the cluster's issuer URL and JWKS, then create a federated robot account whose claim rules match the tokens your cluster mints, usually on `iss`, `aud`, and `sub`. The audience there and `registry.audience` here have to be the same string. See https://container-registry.com/docs/ for the Harbor-side walkthrough.
