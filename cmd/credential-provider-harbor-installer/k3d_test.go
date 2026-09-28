package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeNsenter puts an nsenter on PATH that behaves the way the test needs and
// takes every other command off it, systemctl included. A k3d node is the case
// this exists for: nsenter is in the deployer image, the node has no init
// system behind PID 1, and neither the node nor the container has a systemctl.
func fakeNsenter(t *testing.T, script string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "nsenter")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake nsenter: %v", err)
	}
	t.Setenv("PATH", dir)
}

// The exact failure from issue #32: nsenter enters PID 1's namespaces, finds
// no systemctl there and exits 127. Reading that as an error turned every k3d
// node into a CrashLoopBackOff, because the DaemonSet restarts the container
// and the next run does the same thing.
func TestRunSystemctlReportsANodeWithNoSystemctl(t *testing.T) {
	fakeNsenter(t, "#!/bin/sh\necho 'nsenter: failed to execute systemctl: No such file or directory' >&2\nexit 127\n")

	err := runSystemctl(io.Discard, io.Discard, "daemon-reload")
	if !errors.Is(err, errSystemctlMissing) {
		t.Fatalf("runSystemctl() error = %v, want it to wrap %v", err, errSystemctlMissing)
	}
}

// A systemctl that runs and fails is a different thing, and it still has to
// stop the install. Only a command that never ran is the node's shape rather
// than a problem with this install.
func TestRunSystemctlKeepsARealFailureVisible(t *testing.T) {
	fakeNsenter(t, "#!/bin/sh\necho 'Failed to restart kubelet.service' >&2\nexit 1\n")

	err := runSystemctl(io.Discard, io.Discard, "restart", "kubelet")
	if err == nil {
		t.Fatal("runSystemctl() error = nil, want the systemctl failure")
	}
	if errors.Is(err, errSystemctlMissing) {
		t.Fatalf("runSystemctl() reported %v for a systemctl that ran and failed: %v", errSystemctlMissing, err)
	}
}

func TestNodeHasSystemctl(t *testing.T) {
	fakeNsenter(t, "#!/bin/sh\nexit 0\n")
	if !nodeHasSystemctl() {
		t.Fatal("nodeHasSystemctl() = false on a node whose systemctl answers")
	}

	fakeNsenter(t, "#!/bin/sh\nexit 127\n")
	if nodeHasSystemctl() {
		t.Fatal("nodeHasSystemctl() = true on a node with no systemctl")
	}
}

// The whole of issue #32 in one test: the documented k3d install has to finish,
// report the node done, and say what is left to do, rather than exit 127 and be
// restarted into the same failure forever.
func TestK3dInstallFinishesOnANodeWithNoInitSystem(t *testing.T) {
	fakeNsenter(t, "#!/bin/sh\nexit 127\n")

	hostRoot := t.TempDir()
	opts := markerOptions(t, hostRoot, "k3d-revision")
	opts.Profile = "k3d"
	opts.ConfigureKubelet = true
	opts.BinDir = "/var/lib/rancher/credentialprovider/bin"
	opts.ConfigPath = "/var/lib/rancher/credentialprovider/config.yaml"
	opts.K3sConfigDropInPath = "/etc/rancher/k3s/config.yaml.d/99-credential-provider-harbor.yaml"
	opts.RestartKubelet = true

	if err := install(opts, restartKubelet); err != nil {
		t.Fatalf("install() on a node with no init system error: %v", err)
	}

	// The files the node needs are all there.
	for _, path := range []string{
		filepath.Join(opts.BinDir, opts.BinaryName),
		opts.ConfigPath,
		opts.K3sConfigDropInPath,
	} {
		if _, err := os.Stat(hostPath(opts, path)); err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
	}

	// And the marker says the node is still waiting for a restart, so the next
	// run does not decide the job is finished.
	marker, err := os.ReadFile(hostPath(opts, opts.InstalledMarker))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if !strings.Contains(string(marker), markerKubeletRestartedPrefix+"false") {
		t.Fatalf("marker does not record the missing restart:\n%s", marker)
	}

	previous, err := readMarker(opts)
	if err != nil {
		t.Fatalf("readMarker() error: %v", err)
	}
	if !kubeletRestartNeeded(false, previous, opts) {
		t.Fatal("kubeletRestartNeeded() = false, so a node that gains an init system would never pick the flags up")
	}
}

func TestNoSystemctlMessageNamesTheNextStep(t *testing.T) {
	tests := map[string]string{
		"k3d":     "k3d cluster stop",
		"k3s":     "container running k3s",
		"generic": "Reboot or recreate the node",
	}
	for profile, want := range tests {
		got := noSystemctlMessage(options{Profile: profile, ConfigureKubelet: true})
		if !strings.Contains(got, want) {
			t.Errorf("noSystemctlMessage(%q) = %q, want it to mention %q", profile, got, want)
		}
	}
}

// A run that wrote no kubelet configuration must not claim it did. A reader
// who restarts the node on that promise gets a kubelet that still has no
// flags and no idea why.
func TestNoSystemctlMessageClaimsOnlyWhatWasWritten(t *testing.T) {
	const kubeletConfigWritten = "kubelet configuration this profile owns"

	if got := noSystemctlMessage(options{Profile: "generic", ConfigureKubelet: true}); !strings.Contains(got, kubeletConfigWritten) {
		t.Errorf("message for a configured profile = %q, want it to mention the kubelet configuration", got)
	}
	for _, opts := range []options{
		{Profile: "generic", ConfigureKubelet: false},
		{Profile: "eks", ConfigureKubelet: true},
		{Profile: "aws", ConfigureKubelet: true},
	} {
		if got := noSystemctlMessage(opts); strings.Contains(got, kubeletConfigWritten) {
			t.Errorf("message for profile=%q configure=%t = %q, want no claim about the kubelet configuration",
				opts.Profile, opts.ConfigureKubelet, got)
		}
	}
}
