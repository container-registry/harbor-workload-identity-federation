package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func TestDetectRKE2ServicesPicksTheUnitTheNodeRuns(t *testing.T) {
	// install.sh puts both unit files on every node, so the cases below
	// install both and differ only in what systemd says it runs.
	bothUnits := []string{
		"usr/local/lib/systemd/system/rke2-server.service",
		"usr/local/lib/systemd/system/rke2-agent.service",
	}

	tests := []struct {
		name     string
		units    []string
		running  []string
		fallback string
		want     []string
		// Neither running means the run says so rather than guessing.
		wantErr string
	}{
		{name: "server node", units: bothUnits, running: []string{"rke2-server.service"}, want: []string{"rke2-server"}},
		{name: "agent node", units: bothUnits, running: []string{"rke2-agent.service"}, want: []string{"rke2-agent"}},
		{
			name:    "rpm install",
			units:   []string{"usr/lib/systemd/system/rke2-agent.service"},
			running: []string{"rke2-agent.service"},
			want:    []string{"rke2-agent"},
		},
		{
			name:    "an override in /etc",
			units:   []string{"etc/systemd/system/rke2-server.service"},
			running: []string{"rke2-server.service"},
			want:    []string{"rke2-server"},
		},
		{
			// Enabled but stopped, or systemd out of reach. Either way a guess.
			name:    "installed but running neither",
			units:   bothUnits,
			wantErr: "rke2-server or rke2-agent",
		},
		{name: "neither installed", wantErr: "no rke2-server.service or rke2-agent.service"},
		{
			name:     "an explicit override wins",
			units:    bothUnits,
			running:  []string{"rke2-agent.service"},
			fallback: "rke2-server",
			want:     []string{"rke2-server"},
		},
		{
			// What the error above tells you to do, so rke2-agent has to work
			// as an override like any other name.
			name:     "rke2-agent asked for by name, running neither",
			units:    bothUnits,
			fallback: "rke2-agent",
			want:     []string{"rke2-agent"},
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
			runsUnit := func(unit string) bool { return slices.Contains(tt.running, unit) }

			got, err := detectRKE2Services(options{HostRoot: tmpDir}, tt.fallback, runsUnit)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("detectRKE2Services() = %v, want an error", got)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("detectRKE2Services() error = %q, want it to mention %q", err, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("detectRKE2Services() error: %v", err)
				}
				if !slices.Equal(got, tt.want) {
					t.Fatalf("detectRKE2Services() = %v, want %v", got, tt.want)
				}
			}

			// restartKubelet asks through kubeletServices, so the profile
			// wiring has to carry the answer through, and the error with it.
			opts := options{Profile: "rke2", HostRoot: tmpDir, KubeletService: tt.fallback}
			got, err = kubeletServices(opts, runsUnit)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("kubeletServices() = %v, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("kubeletServices() error: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("kubeletServices() = %v, want %v", got, tt.want)
			}
		})
	}
}
