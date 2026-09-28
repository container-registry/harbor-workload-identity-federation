# kind

For local testing. kind nodes are containers running systemd, so the chart's `kind` profile works with nothing passed beyond the registry.

## Install

```bash
helm upgrade --install credential-provider-harbor \
  oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system \
  --set profile=kind \
  --set registry.host=harbor.example.com \
  --set registry.audience=harbor.example.com

kubectl rollout status daemonset/credential-provider-harbor -n kube-system
```

Files land at `/var/lib/kubelet/credential-provider/` and `/var/lib/kubelet/credential-provider-config.yaml` inside the node container.

## Where the Flags Go on a kind Node

Worth knowing, because it explains a failure that used to look like the install had done nothing.

kind's kubelet unit does expand `$KUBELET_EXTRA_ARGS`. Its `10-kubeadm.conf` ends with:

```text
EnvironmentFile=-/etc/default/kubelet
ExecStart=
ExecStart=/usr/bin/kubelet $KUBELET_KUBECONFIG_ARGS $KUBELET_CONFIG_ARGS $KUBELET_KUBEADM_ARGS $KUBELET_EXTRA_ARGS
```

The node image also ships `/etc/default/kubelet`, and it assigns that variable:

```text
KUBELET_EXTRA_ARGS=--runtime-cgroups=/system.slice/containerd.service
```

systemd applies `EnvironmentFile=` after `Environment=`, whatever order the drop-ins merge in. So a drop-in that sets `KUBELET_EXTRA_ARGS` lands, is read, and is then overwritten by that file, and kubelet starts with kind's own value and neither of the credential provider flags.

The installer writes both places. The drop-in goes to `/etc/systemd/system/kubelet.service.d/99-credential-provider-harbor.conf`, and `/etc/default/kubelet` gets the two flags appended to the value already there, so `--runtime-cgroups` survives.

## Check the Live Command Line

The files are not the thing that decides. Read the running process:

```bash
docker exec <kind-node> sh -c 'tr "\0" " " < /proc/$(pidof kubelet)/cmdline; printf "\n"'
```

You want both of these in it:

```text
--image-credential-provider-bin-dir=/var/lib/kubelet/credential-provider
--image-credential-provider-config=/var/lib/kubelet/credential-provider-config.yaml
```

[`scripts/verify-node-install.sh`](../../../scripts/verify-node-install.sh) does the same check across every node in the cluster.

## If a Node Image Changes the Rules

`kubelet.forceExecStartOverride=true` is the fallback for a node image whose unit stops expanding `$KUBELET_EXTRA_ARGS` at all:

```bash
helm upgrade --install credential-provider-harbor \
  oci://8gears.container-registry.com/8gcr/credential-provider-harbor \
  --namespace kube-system \
  --set profile=kind \
  --set registry.host=harbor.example.com \
  --set registry.audience=harbor.example.com \
  --set kubelet.forceExecStartOverride=true
```

That writes the equivalent of [`../kind-kubelet-systemd-dropin.conf`](../kind-kubelet-systemd-dropin.conf): it clears `ExecStart` and re-declares it with the flags spelled out. `$KUBELET_EXTRA_ARGS` is still expanded ahead of them, so kind's `--runtime-cgroups` survives. It is off by default because it pins kind's kubelet command line into a file this project writes, and it breaks when kind changes that command line. No node image released so far needs it.

## Then Pull Something

```bash
kubectl apply -f examples/kubernetes/rbac-audience.yaml
kubectl apply -f examples/kubernetes/pod-example.yaml
```

`no basic auth credentials` on the pod almost always means kubelet never called the provider. Check the live command line before looking anywhere else.

## Audience

kind needs nothing passed at cluster creation for this. kubelet's request for a Harbor token is authorized by the node audience RBAC the chart creates. The API server's `--api-audiences` governs the tokens it accepts, not the ones it issues, so your registry does not belong there.

For Harbor to validate tokens from a local kind cluster, the cluster issuer has to be reachable from Harbor, or the Trusted Issuer has to be configured with inline JWKS. The [Talos example](../../talos/) has the offline JWKS recipe, which applies here too.
