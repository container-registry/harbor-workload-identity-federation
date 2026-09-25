package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// genericOptions is a node the generic profile would configure: a systemd
// kubelet, nothing distro-specific set.
func genericOptions(t *testing.T) options {
	t.Helper()
	return options{
		Profile:          "generic",
		HostRoot:         t.TempDir(),
		BinDir:           "/usr/local/bin/credential-providers",
		ConfigPath:       "/etc/kubernetes/credential-providers/config.yaml",
		ConfigureKubelet: true,
		KubeletService:   "kubelet",
	}
}

func writeHost(t *testing.T, opts options, path, content string) string {
	t.Helper()
	full := hostPath(opts, path)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return full
}

// The kubeadm packages ship /etc/default/kubelet with an empty
// KUBELET_EXTRA_ARGS. systemd resolves EnvironmentFile= after Environment=
// whatever order the drop-ins merge in, so that empty assignment erased the
// flags the drop-in set and the kubelet started without them.
func TestGenericProfileWritesFlagsWhereAnEnvironmentFileWouldWin(t *testing.T) {
	opts := genericOptions(t)
	defaults := writeHost(t, opts, "/etc/default/kubelet", "KUBELET_EXTRA_ARGS=\n")

	if _, err := configureSystemdKubelet(opts); err != nil {
		t.Fatalf("configureSystemdKubelet() error: %v", err)
	}

	got, err := os.ReadFile(defaults)
	if err != nil {
		t.Fatalf("read defaults: %v", err)
	}
	for _, want := range []string{binDirFlag + "=" + opts.BinDir, configFlag + "=" + opts.ConfigPath} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("/etc/default/kubelet does not carry %q, so the drop-in is still overridden:\n%s", want, got)
		}
	}

	// The drop-in is still written, for the nodes that have no such file.
	dropIn, err := os.ReadFile(hostPath(opts, systemdDropInPath(opts)))
	if err != nil {
		t.Fatalf("read drop-in: %v", err)
	}
	if !strings.Contains(string(dropIn), kubeletExtraArgsVar) {
		t.Fatalf("drop-in does not set %s:\n%s", kubeletExtraArgsVar, dropIn)
	}
}

func TestGenericProfileLeavesAnEnvironmentFileAloneWhenItSaysNothing(t *testing.T) {
	opts := genericOptions(t)
	const untouched = "KUBELET_NODE_LABELS=role=worker\n"
	defaults := writeHost(t, opts, "/etc/default/kubelet", untouched)

	if _, err := configureSystemdKubelet(opts); err != nil {
		t.Fatalf("configureSystemdKubelet() error: %v", err)
	}

	got, err := os.ReadFile(defaults)
	if err != nil {
		t.Fatalf("read defaults: %v", err)
	}
	if string(got) != untouched {
		t.Fatalf("rewrote a file that never assigned %s:\n%s", kubeletExtraArgsVar, got)
	}
}

func TestGenericProfileNeedsNoEnvironmentFile(t *testing.T) {
	opts := genericOptions(t)

	if _, err := configureSystemdKubelet(opts); err != nil {
		t.Fatalf("configureSystemdKubelet() error: %v", err)
	}
	if _, err := os.Stat(hostPath(opts, "/etc/default/kubelet")); !os.IsNotExist(err) {
		t.Fatalf("created /etc/default/kubelet on a node that had none (err=%v)", err)
	}
}

func TestGenericProfileIsIdempotentAcrossBothPlaces(t *testing.T) {
	opts := genericOptions(t)
	writeHost(t, opts, "/etc/default/kubelet", "KUBELET_EXTRA_ARGS=\"--max-pods=110\"\n")

	if _, err := configureSystemdKubelet(opts); err != nil {
		t.Fatalf("first configureSystemdKubelet() error: %v", err)
	}
	first, err := os.ReadFile(hostPath(opts, "/etc/default/kubelet"))
	if err != nil {
		t.Fatalf("read defaults: %v", err)
	}

	changed, err := configureSystemdKubelet(opts)
	if err != nil {
		t.Fatalf("second configureSystemdKubelet() error: %v", err)
	}
	if changed {
		t.Fatal("second run reported a change, so the node would be restarted again")
	}
	second, err := os.ReadFile(hostPath(opts, "/etc/default/kubelet"))
	if err != nil {
		t.Fatalf("read defaults: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("not idempotent:\n%q\nbecame\n%q", first, second)
	}
	if !strings.Contains(string(second), "--max-pods=110") {
		t.Fatalf("dropped what the node already had:\n%s", second)
	}
}
