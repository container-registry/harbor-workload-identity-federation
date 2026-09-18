# OpenShift

Not supported. This page says why, and what it would take, so nobody has to rediscover it.

## Why the Chart Does Not Work Here

Three things block it, and each would need its own answer.

**The node filesystem is not yours to write.** RHCOS is managed by rpm-ostree and `/usr` is read-only, so the chart's default `/usr/local/bin/credential-providers` is unusable from the start. Writable paths under `/etc` and `/var` do survive an update, but they are unmanaged state: nothing reproduces them on a replacement node, and nothing reconciles them if they drift. The supported way to put a file on an RHCOS node is a `MachineConfig` with an Ignition payload, which reboots the node when applied.

**kubelet is owned by the Machine Config Operator.** The MCO renders the kubelet unit and its arguments. Editing the unit or a drop-in on the node puts it out of sync, and the MCO reverts it. The `KubeletConfig` CRD is the supported route for kubelet settings, and it does not expose `--image-credential-provider-bin-dir` or `--image-credential-provider-config`.

**Security Context Constraints.** The installer needs `privileged`, `hostPID`, and a `hostPath` mount of `/`. On OpenShift that means granting the `privileged` SCC to its service account. That is doable, and it is the smallest of the three problems, but it is a real decision for a cluster admin to make rather than something a chart should assume.

## What Would Actually Work

A `MachineConfig` per machine pool that ships three things in one Ignition payload:

1. The `credential-provider-harbor` binary, base64 encoded, into a path outside `/usr`.
2. The `CredentialProviderConfig` file.
3. A systemd drop-in for `kubelet.service` adding the two flags.

Applying it drains and reboots each node in the pool, the same as any other MachineConfig. The binary is embedded in the payload, which means a new MachineConfig for every upgrade of this component, so it wants to be generated rather than hand-written.

That still is not enough on its own. The cluster-side setup is the same as everywhere else and none of it is optional:

- The API server has to accept your Harbor audience. kubelet asks for a token with that audience, and an API server that does not list it refuses before the provider runs.
- `system:nodes` needs `request-serviceaccounts-token-audience` on that audience. [`../rbac-audience.yaml`](../rbac-audience.yaml) is the standalone manifest; on other distributions the chart creates it, and here nothing does.

See the [index](../README.md) for both.

This is the same shape as the [Talos example](../../talos/), where the binary ships as a system extension and kubelet is wired through machine config. An immutable OS wants the provider delivered with the OS, not installed into it.

## Interested?

Nobody has built the MachineConfig generator yet. If you run OpenShift and want this, open an issue describing your OpenShift version and machine pool layout, and say whether you can test a reboot-inducing MachineConfig on a real cluster. Testing is the part that is hard to do without you. See [CONTRIBUTING.md](../../../CONTRIBUTING.md).

## In the Meantime

Image pull secrets still work on OpenShift. Federated Robot Accounts can remove them from your [CI pipelines](../../github-actions/) today, and from clusters on the distributions the [index](../README.md) lists as working, which may already be most of where your pull secrets live.
