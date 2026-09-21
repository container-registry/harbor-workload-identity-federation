package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestConfigureRKE2WritesTheKubeletArgDropIn(t *testing.T) {
	tmpDir := t.TempDir()
	opts := options{
		Profile:          "rke2",
		HostRoot:         tmpDir,
		BinDir:           "/var/lib/rancher/credentialprovider/bin",
		ConfigPath:       "/var/lib/rancher/credentialprovider/config.yaml",
		ConfigureKubelet: true,
	}

	requireIdempotentConfigureKubelet(t, opts)

	dropIn := filepath.Join(tmpDir, "etc/rancher/rke2/config.yaml.d/99-credential-provider-harbor.yaml")
	data, err := os.ReadFile(dropIn)
	if err != nil {
		t.Fatalf("read drop-in: %v", err)
	}

	// RKE2 reads these through kubelet-arg, and the entries carry no leading
	// dashes there. A drop-in with dashes parses and is then ignored.
	var parsed struct {
		KubeletArg []string `json:"kubelet-arg"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("drop-in is not valid YAML: %v\n%s", err, data)
	}
	want := []string{
		"image-credential-provider-bin-dir=/var/lib/rancher/credentialprovider/bin",
		"image-credential-provider-config=/var/lib/rancher/credentialprovider/config.yaml",
	}
	if len(parsed.KubeletArg) != len(want) {
		t.Fatalf("kubelet-arg = %v, want %v", parsed.KubeletArg, want)
	}
	for i, arg := range want {
		if parsed.KubeletArg[i] != arg {
			t.Fatalf("kubelet-arg[%d] = %q, want %q", i, parsed.KubeletArg[i], arg)
		}
	}
}

func TestDetectRKE2ServicesCoversEveryInstalledUnit(t *testing.T) {
	tests := []struct {
		name     string
		units    []string
		fallback string
		want     []string
	}{
		{name: "server node", units: []string{"usr/local/lib/systemd/system/rke2-server.service"}, want: []string{"rke2-server"}},
		{name: "agent node", units: []string{"etc/systemd/system/rke2-agent.service"}, want: []string{"rke2-agent"}},
		{name: "rpm install", units: []string{"usr/lib/systemd/system/rke2-server.service"}, want: []string{"rke2-server"}},
		{name: "neither installed", want: []string{"rke2-agent"}},
		{name: "an explicit override wins", units: []string{"etc/systemd/system/rke2-agent.service"}, fallback: "rke2-server", want: []string{"rke2-server"}},
		{
			// Both kubelets run, and restarting one of them would leave the
			// other on the flags it started with.
			name: "a node running both units restarts both",
			units: []string{
				"etc/systemd/system/rke2-server.service",
				"etc/systemd/system/rke2-agent.service",
			},
			want: []string{"rke2-server", "rke2-agent"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			for _, unit := range tt.units {
				unitPath := filepath.Join(tmpDir, unit)
				if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
					t.Fatalf("create unit dir: %v", err)
				}
				if err := os.WriteFile(unitPath, []byte("[Unit]\n"), 0644); err != nil {
					t.Fatalf("write unit: %v", err)
				}
			}

			got := detectRKE2Services(options{HostRoot: tmpDir}, tt.fallback)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("detectRKE2Services() = %v, want %v", got, tt.want)
			}

			// restartKubelet asks through kubeletServices, so the profile
			// wiring has to carry every unit through, not just the first.
			opts := options{Profile: "rke2", HostRoot: tmpDir, KubeletService: tt.fallback}
			if got := kubeletServices(opts); !slices.Equal(got, tt.want) {
				t.Fatalf("kubeletServices() = %v, want %v", got, tt.want)
			}
		})
	}
}
