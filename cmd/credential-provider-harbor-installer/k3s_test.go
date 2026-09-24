package main

import (
	"os"
	"path/filepath"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestConfigureK3sWritesTheConfigDropIn(t *testing.T) {
	tmpDir := t.TempDir()
	opts := options{
		Profile:          "k3s",
		HostRoot:         tmpDir,
		BinDir:           "/var/lib/rancher/credentialprovider/bin",
		ConfigPath:       "/var/lib/rancher/credentialprovider/config.yaml",
		ConfigureKubelet: true,
	}

	requireIdempotentConfigureKubelet(t, opts)

	dropIn := filepath.Join(tmpDir, "etc/rancher/k3s/config.yaml.d/99-credential-provider-harbor.yaml")
	data, err := os.ReadFile(dropIn)
	if err != nil {
		t.Fatalf("read drop-in: %v", err)
	}

	// k3s takes these as top-level settings of its own, unlike RKE2.
	var parsed struct {
		BinDir     string `json:"image-credential-provider-bin-dir"`
		ConfigPath string `json:"image-credential-provider-config"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("drop-in is not valid YAML: %v\n%s", err, data)
	}
	if parsed.BinDir != opts.BinDir {
		t.Fatalf("image-credential-provider-bin-dir = %q, want %q", parsed.BinDir, opts.BinDir)
	}
	if parsed.ConfigPath != opts.ConfigPath {
		t.Fatalf("image-credential-provider-config = %q, want %q", parsed.ConfigPath, opts.ConfigPath)
	}
}
