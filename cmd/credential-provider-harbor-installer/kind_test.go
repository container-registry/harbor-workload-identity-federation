package main

import (
	"os"
	"testing"
)

// What kindest/node ships at /etc/default/kubelet, byte for byte: one
// assignment, unquoted, and no trailing newline. The unit reads it with
// EnvironmentFile=-/etc/default/kubelet, which systemd applies after every
// Environment= line, so this value is what kubelet started with no matter
// which drop-in set KUBELET_EXTRA_ARGS last.
const kindNodeKubeletDefaults = "KUBELET_EXTRA_ARGS=--runtime-cgroups=/system.slice/containerd.service"

func kindOptions(t *testing.T) options {
	t.Helper()
	return options{
		Profile:          "kind",
		HostRoot:         t.TempDir(),
		BinDir:           "/var/lib/kubelet/credential-provider",
		ConfigPath:       "/var/lib/kubelet/credential-provider-config.yaml",
		ConfigureKubelet: true,
		KubeletService:   "kubelet",
	}
}

// Issue #33: the install reported success and the node came back with neither
// flag. The cause was this file, not the unit. A kind node that keeps the
// drop-in and loses the flags is the one failure the e2e job could catch and
// the installer could not, so it is pinned here.
func TestKindProfileWritesFlagsWhereTheNodeImageWouldEraseThem(t *testing.T) {
	opts := kindOptions(t)
	defaults := writeHost(t, opts, "/etc/default/kubelet", kindNodeKubeletDefaults)

	requireIdempotentConfigureKubelet(t, opts)

	// The whole file, not a substring search. What kubelet ends up with is the
	// one assignment systemd reads last, so a second assignment, a stray quote
	// or a repeated flag would pass a contains check and still leave the node
	// running on something other than these two paths. kind's own
	// --runtime-cgroups has to survive: dropping it moves the kubelet cgroup
	// and breaks the node in a way that has nothing to do with pulling images.
	want := `KUBELET_EXTRA_ARGS="--runtime-cgroups=/system.slice/containerd.service ` +
		binDirFlag + "=" + opts.BinDir + " " +
		configFlag + "=" + opts.ConfigPath + `"`
	got, err := os.ReadFile(defaults)
	if err != nil {
		t.Fatalf("read defaults: %v", err)
	}
	if string(got) != want {
		t.Fatalf("/etc/default/kubelet is\n%q\nwant\n%q", got, want)
	}
}

// The override is still there for a node image that stops expanding the
// variable, and it still writes the ExecStart form when asked for.
func TestKindForceExecStartOverrideStillAvailable(t *testing.T) {
	opts := kindOptions(t)
	opts.ForceKubeletExecStart = true
	writeHost(t, opts, "/etc/default/kubelet", kindNodeKubeletDefaults)

	if _, err := configureKubelet(opts); err != nil {
		t.Fatalf("configureKubelet() error: %v", err)
	}

	// Exact, because this drop-in is the node's whole kubelet command line.
	// $KUBELET_EXTRA_ARGS is expanded ahead of the two flags so that the
	// --runtime-cgroups the node image set survives the override.
	want := "[Service]\n" +
		"ExecStart=\n" +
		"ExecStart=/usr/bin/kubelet $KUBELET_KUBECONFIG_ARGS $KUBELET_CONFIG_ARGS $KUBELET_KUBEADM_ARGS $KUBELET_EXTRA_ARGS " +
		binDirFlag + "=" + opts.BinDir + " " +
		configFlag + "=" + opts.ConfigPath + "\n"
	dropIn, err := os.ReadFile(hostPath(opts, systemdDropInPath(opts)))
	if err != nil {
		t.Fatalf("read drop-in: %v", err)
	}
	if string(dropIn) != want {
		t.Fatalf("kind ExecStart drop-in is\n%q\nwant\n%q", dropIn, want)
	}

	// The override owns the command line, so it must not also leave the node
	// reading these flags out of the environment file.
	defaults, err := os.ReadFile(hostPath(opts, "/etc/default/kubelet"))
	if err != nil {
		t.Fatalf("read defaults: %v", err)
	}
	if string(defaults) != kindNodeKubeletDefaults {
		t.Fatalf("override rewrote /etc/default/kubelet:\n%q", defaults)
	}
}
