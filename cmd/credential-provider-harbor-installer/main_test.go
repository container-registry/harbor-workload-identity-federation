package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/yaml"
)

func TestMergeProviderReplacesExistingProviderAndPreservesOthers(t *testing.T) {
	cfg := credentialProviderConfig{
		APIVersion: configAPIVersion,
		Kind:       "CredentialProviderConfig",
		Providers: []credentialProvider{
			{Name: "ecr-credential-provider", APIVersion: providerAPIVersion},
			{Name: providerName, APIVersion: providerAPIVersion, MatchImages: []string{"old.example.com"}},
		},
	}

	opts := options{
		BinaryName:       providerName,
		RegistryAudience: "harbor.example.com",
		RegistryUsername: "jwt",
		MatchImages:      []string{"harbor.example.com"},
		CacheDuration:    "1h",
	}
	merged := mergeProvider(cfg, harborProvider(opts))

	if len(merged.Providers) != 2 {
		t.Fatalf("len(Providers) = %d, want 2", len(merged.Providers))
	}
	if merged.Providers[0].Name != providerName {
		t.Fatalf("Providers[0].Name = %q, want %q", merged.Providers[0].Name, providerName)
	}
	if got := merged.Providers[0].MatchImages[0]; got != "harbor.example.com" {
		t.Fatalf("Providers[0].MatchImages[0] = %q, want harbor.example.com", got)
	}
	if merged.Providers[1].Name != "ecr-credential-provider" {
		t.Fatalf("Providers[1].Name = %q, want ecr-credential-provider", merged.Providers[1].Name)
	}
}

func TestConfigureKindSystemdKubeletWritesExecStartOverride(t *testing.T) {
	tmpDir := t.TempDir()
	opts := options{
		HostRoot:       tmpDir,
		BinDir:         "/var/lib/kubelet/credential-provider",
		ConfigPath:     "/var/lib/kubelet/credential-provider-config.yaml",
		KubeletService: "kubelet",
	}

	changed, err := configureKindSystemdKubelet(opts)
	if err != nil {
		t.Fatalf("configureKindSystemdKubelet() error: %v", err)
	}
	if !changed {
		t.Fatal("configureKindSystemdKubelet() changed = false, want true")
	}
	changed, err = configureKindSystemdKubelet(opts)
	if err != nil {
		t.Fatalf("second configureKindSystemdKubelet() error: %v", err)
	}
	if changed {
		t.Fatal("second configureKindSystemdKubelet() changed = true, want false")
	}

	dropIn := filepath.Join(tmpDir, "etc/systemd/system/kubelet.service.d/99-credential-provider-harbor.conf")
	data, err := os.ReadFile(dropIn)
	if err != nil {
		t.Fatalf("read drop-in: %v", err)
	}
	for _, want := range [][]byte{
		[]byte("ExecStart="),
		[]byte("ExecStart=/usr/bin/kubelet $KUBELET_KUBECONFIG_ARGS $KUBELET_CONFIG_ARGS $KUBELET_KUBEADM_ARGS"),
		[]byte("--image-credential-provider-bin-dir=/var/lib/kubelet/credential-provider"),
		[]byte("--image-credential-provider-config=/var/lib/kubelet/credential-provider-config.yaml"),
	} {
		if !bytes.Contains(data, want) {
			t.Fatalf("drop-in missing %q:\n%s", want, data)
		}
	}
}

func TestConfigureKubeletKindDefaultsToExtraArgsDropIn(t *testing.T) {
	tmpDir := t.TempDir()
	opts := options{
		Profile:               "kind",
		HostRoot:              tmpDir,
		BinDir:                "/var/lib/kubelet/credential-provider",
		ConfigPath:            "/var/lib/kubelet/credential-provider-config.yaml",
		ConfigureKubelet:      true,
		KubeletService:        "kubelet",
		ForceKubeletExecStart: false,
	}

	changed, err := configureKubelet(opts)
	if err != nil {
		t.Fatalf("configureKubelet() error: %v", err)
	}
	if !changed {
		t.Fatal("configureKubelet() changed = false, want true")
	}

	dropIn := filepath.Join(tmpDir, "etc/systemd/system/kubelet.service.d/99-credential-provider-harbor.conf")
	data, err := os.ReadFile(dropIn)
	if err != nil {
		t.Fatalf("read drop-in: %v", err)
	}
	if bytes.Contains(data, []byte("ExecStart=")) {
		t.Fatalf("default kind drop-in contains ExecStart override:\n%s", data)
	}
	if !bytes.Contains(data, []byte("KUBELET_EXTRA_ARGS=")) {
		t.Fatalf("default kind drop-in missing KUBELET_EXTRA_ARGS:\n%s", data)
	}
}

func TestInstallCredentialProviderConfigPreservesECRProvider(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	existing := credentialProviderConfig{
		APIVersion: configAPIVersion,
		Kind:       "CredentialProviderConfig",
		Providers: []credentialProvider{
			{Name: "ecr-credential-provider", APIVersion: providerAPIVersion, MatchImages: []string{"public.ecr.aws"}, DefaultCacheDuration: "12h0m0s"},
		},
	}
	data, err := json.Marshal(existing)
	if err != nil {
		t.Fatalf("marshal existing config: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	opts := options{
		HostRoot:            "/",
		BinaryName:          providerName,
		ConfigPath:          configPath,
		ConfigFormat:        "json",
		RegistryAudience:    "harbor.example.com",
		RegistryUsername:    "jwt",
		MatchImages:         []string{"harbor.example.com"},
		CacheDuration:       "1h",
		PreserveECRProvider: true,
	}
	changed, err := installCredentialProviderConfig(opts)
	if err != nil {
		t.Fatalf("installCredentialProviderConfig() error: %v", err)
	}
	if !changed {
		t.Fatal("installCredentialProviderConfig() changed = false, want true")
	}

	cfg, err := readCredentialProviderConfig(configPath, "json")
	if err != nil {
		t.Fatalf("readCredentialProviderConfig() error: %v", err)
	}
	if len(cfg.Providers) != 2 {
		t.Fatalf("len(Providers) = %d, want 2", len(cfg.Providers))
	}
	if cfg.Providers[0].Name != providerName {
		t.Fatalf("Providers[0].Name = %q, want %q", cfg.Providers[0].Name, providerName)
	}
	if cfg.Providers[1].Name != "ecr-credential-provider" {
		t.Fatalf("Providers[1].Name = %q, want ecr-credential-provider", cfg.Providers[1].Name)
	}
}

func TestOptionsDefaultToNodeModificationAndRestart(t *testing.T) {
	t.Setenv("REGISTRY_HOST", "harbor.example.com")

	opts, err := optionsFromEnv()
	if err != nil {
		t.Fatalf("optionsFromEnv() error: %v", err)
	}
	if !opts.ConfigureKubelet {
		t.Fatal("ConfigureKubelet = false, want true")
	}
	if !opts.RestartKubelet {
		t.Fatal("RestartKubelet = false, want true")
	}
	if opts.ForceKubeletExecStart {
		t.Fatal("ForceKubeletExecStart = true, want false")
	}
}

// validOptions is the fixture every validateOptions test starts from: one set
// of values validateOptions accepts, so each test only has to say which single
// field it breaks.
func validOptions() options {
	return options{
		HostRoot:        "/host",
		SourceBinary:    "/usr/local/bin/credential-provider-harbor",
		BinaryName:      providerName,
		BinDir:          "/usr/local/bin/credential-providers",
		ConfigPath:      "/etc/kubernetes/credential-providers/config.yaml",
		ConfigFormat:    "yaml",
		MatchImages:     []string{"harbor.example.com"},
		InstalledMarker: defaultInstalledMarker,
		InstallID:       "abc123",
	}
}

func TestValidateOptionsAcceptsTheBaseFixture(t *testing.T) {
	if err := validateOptions(validOptions()); err != nil {
		t.Fatalf("validateOptions(validOptions()) error: %v", err)
	}
}

func TestValidateOptionsRejectsUnsafeBinaryName(t *testing.T) {
	base := validOptions()

	for _, binaryName := range []string{"", ".", "..", "../evil", "/bin/sh", "nested/name"} {
		t.Run(binaryName, func(t *testing.T) {
			opts := base
			opts.BinaryName = binaryName
			if err := validateOptions(opts); err == nil {
				t.Fatal("validateOptions() returned nil error, want unsafe binary name error")
			}
		})
	}
}

func TestInstallBinaryFixesModeWhenContentsMatch(t *testing.T) {
	tmpDir := t.TempDir()
	source := filepath.Join(tmpDir, "source")
	binDir := filepath.Join(tmpDir, "bin")
	target := filepath.Join(binDir, providerName)

	if err := os.WriteFile(source, []byte("binary"), 0755); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}
	if err := os.WriteFile(target, []byte("binary"), 0644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	changed, err := installBinary(options{
		HostRoot:     "/",
		SourceBinary: source,
		BinaryName:   providerName,
		BinDir:       binDir,
	})
	if err != nil {
		t.Fatalf("installBinary() error: %v", err)
	}
	if !changed {
		t.Fatal("installBinary() changed = false, want true")
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if got := info.Mode().Perm(); got != 0755 {
		t.Fatalf("target mode = %v, want 0755", got)
	}
}

func TestSetKubeletFlagsHandlesTheShapesAKSWrites(t *testing.T) {
	const binDir = "/usr/local/bin/credential-providers"
	const configPath = "/etc/kubernetes/credential-providers/config.yaml"

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "quoted",
			content: "KUBELET_FLAGS=\"--node-labels=a=b --max-pods=110\"\n",
			want:    "KUBELET_FLAGS=\"--node-labels=a=b --max-pods=110 " + binDirFlag + "=" + binDir + " " + configFlag + "=" + configPath + "\"\n",
		},
		{
			name:    "unquoted",
			content: "KUBELET_FLAGS=--max-pods=110\n",
			want:    "KUBELET_FLAGS=\"--max-pods=110 " + binDirFlag + "=" + binDir + " " + configFlag + "=" + configPath + "\"\n",
		},
		{
			name:    "empty value",
			content: "KUBELET_FLAGS=\"\"\nKUBELET_NODE_LABELS=a=b\n",
			want:    "KUBELET_FLAGS=\"" + binDirFlag + "=" + binDir + " " + configFlag + "=" + configPath + "\"\nKUBELET_NODE_LABELS=a=b\n",
		},
		{
			name:    "a commented assignment is not an assignment",
			content: "# KUBELET_FLAGS=\"--decoy\"\nKUBELET_FLAGS=\"--max-pods=110\"\n",
			want:    "# KUBELET_FLAGS=\"--decoy\"\nKUBELET_FLAGS=\"--max-pods=110 " + binDirFlag + "=" + binDir + " " + configFlag + "=" + configPath + "\"\n",
		},
		{
			name:    "the last assignment wins, as systemd reads it",
			content: "KUBELET_FLAGS=\"--first\"\nKUBELET_FLAGS=\"--second\"\n",
			want:    "KUBELET_FLAGS=\"--first\"\nKUBELET_FLAGS=\"--second " + binDirFlag + "=" + binDir + " " + configFlag + "=" + configPath + "\"\n",
		},
		{
			name:    "an earlier install's values are replaced, not duplicated",
			content: "KUBELET_FLAGS=\"--max-pods=110 " + binDirFlag + "=/old/bin " + configFlag + "=/old/config.yaml\"\n",
			want:    "KUBELET_FLAGS=\"--max-pods=110 " + binDirFlag + "=" + binDir + " " + configFlag + "=" + configPath + "\"\n",
		},
		{
			name:    "the space-separated spelling is replaced too",
			content: "KUBELET_FLAGS=\"" + binDirFlag + " /old/bin --max-pods=110\"\n",
			want:    "KUBELET_FLAGS=\"--max-pods=110 " + binDirFlag + "=" + binDir + " " + configFlag + "=" + configPath + "\"\n",
		},
		{
			name:    "one flag present without the other",
			content: "KUBELET_FLAGS=\"" + binDirFlag + "=/old/bin\"\n",
			want:    "KUBELET_FLAGS=\"" + binDirFlag + "=" + binDir + " " + configFlag + "=" + configPath + "\"\n",
		},
		{
			// A rewrite would collapse the run of spaces and re-escape the
			// quotes. Everything here but the two flags has to survive.
			name:    "the rest of the value is not reformatted",
			content: "KUBELET_FLAGS=\"--node-labels=a=b  --kube-reserved=cpu=100m\t--eviction-hard=memory.available<100Mi\"\n",
			want:    "KUBELET_FLAGS=\"--node-labels=a=b  --kube-reserved=cpu=100m\t--eviction-hard=memory.available<100Mi " + binDirFlag + "=" + binDir + " " + configFlag + "=" + configPath + "\"\n",
		},
		{
			name:    "single quotes stay single quotes",
			content: "KUBELET_FLAGS='--max-pods=110'\n",
			want:    "KUBELET_FLAGS='--max-pods=110 " + binDirFlag + "=" + binDir + " " + configFlag + "=" + configPath + "'\n",
		},
		{
			// The flag was left without its value. The argument after it
			// belongs to the node, not to us.
			name:    "a valueless flag does not swallow the next argument",
			content: "KUBELET_FLAGS=\"" + binDirFlag + " --max-pods=110\"\n",
			want:    "KUBELET_FLAGS=\"--max-pods=110 " + binDirFlag + "=" + binDir + " " + configFlag + "=" + configPath + "\"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := setKubeletFlags(tt.content, binDir, configPath)
			if err != nil {
				t.Fatalf("setKubeletFlags() error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("setKubeletFlags() =\n%q\nwant\n%q", got, tt.want)
			}

			again, err := setKubeletFlags(got, binDir, configPath)
			if err != nil {
				t.Fatalf("second setKubeletFlags() error: %v", err)
			}
			if again != got {
				t.Fatalf("setKubeletFlags() is not idempotent:\n%q\nbecame\n%q", got, again)
			}
		})
	}
}

func TestSetKubeletFlagsRejectsAFileWithNoActiveAssignment(t *testing.T) {
	for _, content := range []string{
		"",
		"# KUBELET_FLAGS=\"--max-pods=110\"\n",
		"KUBELET_EXTRA_ARGS=\"--max-pods=110\"\n",
	} {
		if _, err := setKubeletFlags(content, "/bin", "/config.yaml"); err == nil {
			t.Fatalf("setKubeletFlags(%q) returned nil error, want no-assignment error", content)
		}
	}
}

func TestConfigureKubeletDefaultsPatchesAndIsIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	defaultsPath := filepath.Join(tmpDir, "etc/default/kubelet")
	if err := os.MkdirAll(filepath.Dir(defaultsPath), 0755); err != nil {
		t.Fatalf("create defaults dir: %v", err)
	}
	if err := os.WriteFile(defaultsPath, []byte("KUBELET_FLAGS=\"--max-pods=110\"\n"), 0644); err != nil {
		t.Fatalf("write defaults: %v", err)
	}

	opts := options{
		Profile:             "aks",
		HostRoot:            tmpDir,
		BinDir:              "/usr/local/bin/credential-providers",
		ConfigPath:          "/etc/kubernetes/credential-providers/config.yaml",
		ConfigureKubelet:    true,
		KubeletDefaultsPath: "/etc/default/kubelet",
	}

	changed, err := configureKubelet(opts)
	if err != nil {
		t.Fatalf("configureKubelet() error: %v", err)
	}
	if !changed {
		t.Fatal("configureKubelet() changed = false, want true")
	}

	data, err := os.ReadFile(defaultsPath)
	if err != nil {
		t.Fatalf("read defaults: %v", err)
	}
	for _, want := range []string{"--max-pods=110", binDirFlag + "=/usr/local/bin/credential-providers", configFlag + "=/etc/kubernetes/credential-providers/config.yaml"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatalf("defaults missing %q:\n%s", want, data)
		}
	}

	changed, err = configureKubelet(opts)
	if err != nil {
		t.Fatalf("second configureKubelet() error: %v", err)
	}
	if changed {
		t.Fatal("second configureKubelet() changed = true, want false")
	}
}

func TestConfigureKubeletDefaultsFailsOnANodeWithoutTheFile(t *testing.T) {
	opts := options{
		Profile:             "aks",
		HostRoot:            t.TempDir(),
		BinDir:              "/usr/local/bin/credential-providers",
		ConfigPath:          "/etc/kubernetes/credential-providers/config.yaml",
		ConfigureKubelet:    true,
		KubeletDefaultsPath: "/etc/default/kubelet",
	}

	if _, err := configureKubelet(opts); err == nil {
		t.Fatal("configureKubelet() returned nil error, want a missing-file error")
	}
}

func TestConfigureRKE2WritesTheKubeletArgDropIn(t *testing.T) {
	tmpDir := t.TempDir()
	opts := options{
		Profile:          "rke2",
		HostRoot:         tmpDir,
		BinDir:           "/var/lib/rancher/credentialprovider/bin",
		ConfigPath:       "/var/lib/rancher/credentialprovider/config.yaml",
		ConfigureKubelet: true,
	}

	changed, err := configureKubelet(opts)
	if err != nil {
		t.Fatalf("configureKubelet() error: %v", err)
	}
	if !changed {
		t.Fatal("configureKubelet() changed = false, want true")
	}
	changed, err = configureKubelet(opts)
	if err != nil {
		t.Fatalf("second configureKubelet() error: %v", err)
	}
	if changed {
		t.Fatal("second configureKubelet() changed = true, want false")
	}

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

func TestSetMicroK8sKubeletArgsReplacesRatherThanAppends(t *testing.T) {
	const binDir = "/var/snap/microk8s/common/credentialprovider/bin"
	const configPath = "/var/snap/microk8s/common/credentialprovider/config.yaml"

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "appends to the snap's own arguments",
			content: "--kubeconfig=/var/snap/microk8s/current/credentials/kubelet.config\n--cert-dir=${SNAP_DATA}/certs\n",
			want:    "--kubeconfig=/var/snap/microk8s/current/credentials/kubelet.config\n--cert-dir=${SNAP_DATA}/certs\n" + binDirFlag + "=" + binDir + "\n" + configFlag + "=" + configPath + "\n",
		},
		{
			name:    "a file without a trailing newline does not join lines",
			content: "--cert-dir=${SNAP_DATA}/certs",
			want:    "--cert-dir=${SNAP_DATA}/certs\n" + binDirFlag + "=" + binDir + "\n" + configFlag + "=" + configPath + "\n",
		},
		{
			name:    "an earlier install's arguments are replaced",
			content: "--cert-dir=${SNAP_DATA}/certs\n" + binDirFlag + "=/old/bin\n" + configFlag + "=/old/config.yaml\n",
			want:    "--cert-dir=${SNAP_DATA}/certs\n" + binDirFlag + "=" + binDir + "\n" + configFlag + "=" + configPath + "\n",
		},
		{
			name:    "the space-separated spelling is replaced too",
			content: binDirFlag + " /old/bin\n--cert-dir=${SNAP_DATA}/certs\n",
			want:    "--cert-dir=${SNAP_DATA}/certs\n" + binDirFlag + "=" + binDir + "\n" + configFlag + "=" + configPath + "\n",
		},
		{
			// kubelite passes each line on as its own argument, so a flag and
			// its value can sit on two lines. Leaving the value behind would
			// hand kubelet a stray path.
			name:    "a flag split across two lines takes its value with it",
			content: binDirFlag + "\n/old/bin\n--cert-dir=${SNAP_DATA}/certs\n",
			want:    "--cert-dir=${SNAP_DATA}/certs\n" + binDirFlag + "=" + binDir + "\n" + configFlag + "=" + configPath + "\n",
		},
		{
			name:    "a valueless flag does not swallow the next argument",
			content: binDirFlag + "\n--cert-dir=${SNAP_DATA}/certs\n",
			want:    "--cert-dir=${SNAP_DATA}/certs\n" + binDirFlag + "=" + binDir + "\n" + configFlag + "=" + configPath + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := setMicroK8sKubeletArgs(tt.content, binDir, configPath)
			if got != tt.want {
				t.Fatalf("setMicroK8sKubeletArgs() =\n%q\nwant\n%q", got, tt.want)
			}
			if again := setMicroK8sKubeletArgs(got, binDir, configPath); again != got {
				t.Fatalf("setMicroK8sKubeletArgs() is not idempotent:\n%q\nbecame\n%q", got, again)
			}
		})
	}
}

func TestConfigureMicroK8sArgsFailsWhenTheSnapIsNotThere(t *testing.T) {
	opts := options{
		Profile:          "microk8s",
		HostRoot:         t.TempDir(),
		BinDir:           "/var/snap/microk8s/common/credentialprovider/bin",
		ConfigPath:       "/var/snap/microk8s/common/credentialprovider/config.yaml",
		ConfigureKubelet: true,
	}

	if _, err := configureKubelet(opts); err == nil {
		t.Fatal("configureKubelet() returned nil error, want a missing-file error")
	}
}

func TestConfigureMicroK8sArgsWritesTheSnapArgumentsFile(t *testing.T) {
	tmpDir := t.TempDir()
	argsPath := filepath.Join(tmpDir, "var/snap/microk8s/current/args/kubelet")
	if err := os.MkdirAll(filepath.Dir(argsPath), 0755); err != nil {
		t.Fatalf("create args dir: %v", err)
	}
	if err := os.WriteFile(argsPath, []byte("--cert-dir=${SNAP_DATA}/certs\n"), 0644); err != nil {
		t.Fatalf("write args: %v", err)
	}

	opts := options{
		Profile:          "microk8s",
		HostRoot:         tmpDir,
		BinDir:           "/var/snap/microk8s/common/credentialprovider/bin",
		ConfigPath:       "/var/snap/microk8s/common/credentialprovider/config.yaml",
		ConfigureKubelet: true,
	}

	changed, err := configureKubelet(opts)
	if err != nil {
		t.Fatalf("configureKubelet() error: %v", err)
	}
	if !changed {
		t.Fatal("configureKubelet() changed = false, want true")
	}
	changed, err = configureKubelet(opts)
	if err != nil {
		t.Fatalf("second configureKubelet() error: %v", err)
	}
	if changed {
		t.Fatal("second configureKubelet() changed = true, want false")
	}

	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	want := "--cert-dir=${SNAP_DATA}/certs\n" +
		binDirFlag + "=/var/snap/microk8s/common/credentialprovider/bin\n" +
		configFlag + "=/var/snap/microk8s/common/credentialprovider/config.yaml\n"
	if string(data) != want {
		t.Fatalf("args =\n%q\nwant\n%q", data, want)
	}
}

func TestProfileDefaultsCoverTheNewDistributions(t *testing.T) {
	tests := []struct {
		profile        string
		binDir         string
		configPath     string
		kubeletService string
	}{
		{"aks", "/usr/local/bin/credential-providers", "/etc/kubernetes/credential-providers/config.yaml", "kubelet"},
		{"rke2", "/var/lib/rancher/credentialprovider/bin", "/var/lib/rancher/credentialprovider/config.yaml", "rke2-agent"},
		{"microk8s", "/var/snap/microk8s/common/credentialprovider/bin", "/var/snap/microk8s/common/credentialprovider/config.yaml", "snap.microk8s.daemon-kubelite"},
	}

	for _, tt := range tests {
		t.Run(tt.profile, func(t *testing.T) {
			t.Setenv("PROFILE", tt.profile)
			t.Setenv("REGISTRY_HOST", "harbor.example.com")
			// env() treats empty as unset, so this asserts the profile
			// defaults rather than whatever the shell running the tests
			// happens to export.
			for _, name := range []string{
				"BIN_DIR", "CONFIG_PATH", "CONFIG_DIR", "CONFIG_FILE", "CONFIG_FORMAT",
				"KUBELET_SERVICE", "KUBELET_DEFAULTS_PATH", "MICROK8S_KUBELET_ARGS_PATH",
				"RKE2_CONFIG_DROP_IN_PATH", "K3S_CONFIG_DROP_IN_PATH", "SYSTEMD_DROP_IN_PATH",
			} {
				t.Setenv(name, "")
			}

			opts, err := optionsFromEnv()
			if err != nil {
				t.Fatalf("optionsFromEnv() error: %v", err)
			}
			if opts.BinDir != tt.binDir {
				t.Fatalf("BinDir = %q, want %q", opts.BinDir, tt.binDir)
			}
			if opts.ConfigPath != tt.configPath {
				t.Fatalf("ConfigPath = %q, want %q", opts.ConfigPath, tt.configPath)
			}
			if opts.KubeletService != tt.kubeletService {
				t.Fatalf("KubeletService = %q, want %q", opts.KubeletService, tt.kubeletService)
			}
			if err := validateOptions(opts); err != nil {
				t.Fatalf("validateOptions() error: %v", err)
			}
		})
	}
}

func TestDetectRKE2ServicePrefersTheInstalledUnit(t *testing.T) {
	tests := []struct {
		name     string
		unit     string
		fallback string
		want     string
	}{
		{name: "server node", unit: "usr/local/lib/systemd/system/rke2-server.service", want: "rke2-server"},
		{name: "agent node", unit: "etc/systemd/system/rke2-agent.service", want: "rke2-agent"},
		{name: "rpm install", unit: "usr/lib/systemd/system/rke2-server.service", want: "rke2-server"},
		{name: "neither installed", want: "rke2-agent"},
		{name: "an explicit override wins", unit: "etc/systemd/system/rke2-agent.service", fallback: "rke2-server", want: "rke2-server"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			if tt.unit != "" {
				unitPath := filepath.Join(tmpDir, tt.unit)
				if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
					t.Fatalf("create unit dir: %v", err)
				}
				if err := os.WriteFile(unitPath, []byte("[Unit]\n"), 0644); err != nil {
					t.Fatalf("write unit: %v", err)
				}
			}

			if got := detectRKE2Service(options{HostRoot: tmpDir}, tt.fallback); got != tt.want {
				t.Fatalf("detectRKE2Service() = %q, want %q", got, tt.want)
			}
		})
	}
}

// markerOptions builds a run that touches nothing but a temporary host root:
// kubelet stays untouched, so run() exercises the marker ordering end to end.
func markerOptions(t *testing.T, hostRoot, installID string) options {
	t.Helper()

	source := filepath.Join(t.TempDir(), "credential-provider-harbor")
	if err := os.WriteFile(source, []byte("binary"), 0755); err != nil {
		t.Fatalf("write source binary: %v", err)
	}

	return options{
		Profile:          "generic",
		HostRoot:         hostRoot,
		SourceBinary:     source,
		BinaryName:       providerName,
		BinDir:           "/usr/local/bin/credential-providers",
		ConfigPath:       "/etc/kubernetes/credential-providers/config.yaml",
		ConfigFormat:     "yaml",
		RegistryHost:     "harbor.example.com",
		RegistryAudience: "harbor.example.com",
		RegistryUsername: "jwt",
		MatchImages:      []string{"harbor.example.com"},
		CacheDuration:    "1h",
		ConfigureKubelet: false,
		RestartKubelet:   false,
		InstalledMarker:  defaultInstalledMarker,
		InstallID:        installID,
	}
}

// simulateReboot empties the host directories a reboot empties. /run is tmpfs
// on every systemd host and /var/run is a symlink to it, so anything written
// under either is gone once the node comes back up.
func simulateReboot(t *testing.T, hostRoot string) {
	t.Helper()

	for _, dir := range []string{"run", "var/run"} {
		if err := os.RemoveAll(filepath.Join(hostRoot, dir)); err != nil {
			t.Fatalf("wipe %s: %v", dir, err)
		}
	}
}

func writeMarkerFile(t *testing.T, opts options, content string) string {
	t.Helper()

	marker := hostPath(opts, opts.InstalledMarker)
	if err := os.MkdirAll(filepath.Dir(marker), 0755); err != nil {
		t.Fatalf("create marker directory: %v", err)
	}
	if err := os.WriteFile(marker, []byte(content), 0644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	return marker
}

func TestMarkerContentLeadsWithTheInstallIDTheProbeGrepsFor(t *testing.T) {
	completedAt := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

	tests := []struct {
		name      string
		installID string
		want      string
	}{
		{
			name:      "chart install ID",
			installID: "8a08abd5413fbe30",
			want:      "install-id=8a08abd5413fbe30\nkubelet-restarted=true\ncompleted-at=2026-03-04T05:06:07Z\n",
		},
		{
			name:      "standalone run",
			installID: standaloneInstallID,
			want:      "install-id=standalone\nkubelet-restarted=true\ncompleted-at=2026-03-04T05:06:07Z\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := string(markerContent(options{InstallID: test.installID}, true, completedAt))
			if got != test.want {
				t.Fatalf("markerContent() = %q, want %q", got, test.want)
			}
			// The readiness probe is `grep -qxF install-id=<id>`, which only
			// matches a whole line.
			if first, _, _ := strings.Cut(got, "\n"); first != markerInstallIDPrefix+test.installID {
				t.Fatalf("first line = %q, want %q", first, markerInstallIDPrefix+test.installID)
			}
		})
	}
}

func TestReadMarkerReadsTheInstallIDBackExactly(t *testing.T) {
	tests := []struct {
		name   string
		marker string
		absent bool
		want   string
	}{
		{name: "no marker", absent: true, want: ""},
		{name: "current marker", marker: "install-id=abc123\ncompleted-at=2026-03-04T05:06:07Z\n", want: "abc123"},
		{name: "marker without a trailing newline", marker: "install-id=abc123", want: "abc123"},
		{name: "marker predating install IDs", marker: "2026-03-04T05:06:07Z\n", want: ""},
		{name: "empty marker", marker: "", want: ""},
		{name: "install ID on a later line", marker: "noise\ninstall-id=abc123\n", want: ""},
		// The probe is `grep -qxF install-id=<id>`, which compares the whole
		// line byte for byte. Reading the ID back with the surrounding
		// whitespace trimmed off would have the installer see a different ID
		// from the one the probe matched, call the node uninstalled, and
		// restart kubelet again every time the container came up.
		{name: "install ID padded with whitespace", marker: "install-id=abc123 \n", want: "abc123 "},
		{name: "install ID indented", marker: "  install-id=abc123\n", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := markerOptions(t, t.TempDir(), "abc123")
			if !test.absent {
				writeMarkerFile(t, opts, test.marker)
			}

			got, err := readMarker(opts)
			if err != nil {
				t.Fatalf("readMarker() error: %v", err)
			}
			if got.InstallID != test.want {
				t.Fatalf("readMarker().InstallID = %q, want %q", got.InstallID, test.want)
			}
		})
	}
}

func TestRunReplacesAStaleMarkerWithThisInstallID(t *testing.T) {
	hostRoot := t.TempDir()
	opts := markerOptions(t, hostRoot, "new-revision")
	marker := writeMarkerFile(t, opts, "install-id=old-revision\ncompleted-at=2026-03-04T05:06:07Z\n")

	if err := run(opts); err != nil {
		t.Fatalf("run() error: %v", err)
	}

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if first, _, _ := strings.Cut(string(data), "\n"); first != "install-id=new-revision" {
		t.Fatalf("marker first line = %q, want install-id=new-revision", first)
	}
}

func TestRunLeavesNoMarkerWhenTheInstallFails(t *testing.T) {
	hostRoot := t.TempDir()
	opts := markerOptions(t, hostRoot, "new-revision")
	marker := writeMarkerFile(t, opts, "install-id=old-revision\ncompleted-at=2026-03-04T05:06:07Z\n")

	// A pod whose installer cannot run at all is the CrashLoopBackOff case: the
	// previous revision's marker must not keep the probe passing.
	opts.SourceBinary = filepath.Join(t.TempDir(), "missing")

	if err := run(opts); err == nil {
		t.Fatal("run() returned nil error, want a missing source binary error")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("stat marker after a failed run: err = %v, want not-exist", err)
	}
}

func TestDefaultInstalledMarkerIsNotOnTmpfs(t *testing.T) {
	t.Setenv("REGISTRY_HOST", "harbor.example.com")

	opts, err := optionsFromEnv()
	if err != nil {
		t.Fatalf("optionsFromEnv() error: %v", err)
	}
	if opts.InstalledMarker != defaultInstalledMarker {
		t.Fatalf("InstalledMarker = %q, want %q", opts.InstalledMarker, defaultInstalledMarker)
	}
	// /run is tmpfs on systemd hosts and /var/run is a symlink to it, so a
	// default under either is emptied at every boot.
	for _, tmpfs := range []string{"/run/", "/var/run/"} {
		if strings.HasPrefix(opts.InstalledMarker, tmpfs) {
			t.Fatalf("default marker %q lives under %s, which a reboot empties", opts.InstalledMarker, tmpfs)
		}
	}
}

// TestRebootDoesNotRepeatTheKubeletRestart is the reason the default marker
// path moved off /var/run. A node that reboots comes back with the drop-in
// already in effect, so kubelet has nothing to pick up. Whether the installer
// agrees comes down to one thing: whether the marker it wrote is still there.
func TestRebootDoesNotRepeatTheKubeletRestart(t *testing.T) {
	tests := []struct {
		name               string
		marker             string
		wantRestartOnBoot  bool
		wantIDAfterAReboot string
	}{
		{
			name:               "default marker on persistent storage",
			marker:             defaultInstalledMarker,
			wantRestartOnBoot:  false,
			wantIDAfterAReboot: "install-1",
		},
		{
			name:               "marker on tmpfs, as the old default was",
			marker:             "/var/run/credential-provider-harbor-installed",
			wantRestartOnBoot:  true,
			wantIDAfterAReboot: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hostRoot := t.TempDir()
			opts := markerOptions(t, hostRoot, "install-1")
			opts.InstalledMarker = test.marker

			if err := run(opts); err != nil {
				t.Fatalf("run() error: %v", err)
			}

			// Same pod, same node, nothing rebooted: no second restart.
			installed, err := readMarker(opts)
			if err != nil {
				t.Fatalf("readMarker() error: %v", err)
			}
			if installed.InstallID != opts.InstallID {
				t.Fatalf("install ID after a run = %q, want %q", installed.InstallID, opts.InstallID)
			}
			if kubeletRestartNeeded(false, installed, opts) {
				t.Fatal("kubeletRestartNeeded() = true right after the install, want false")
			}

			simulateReboot(t, hostRoot)

			afterBoot, err := readMarker(opts)
			if err != nil {
				t.Fatalf("readMarker() after a reboot error: %v", err)
			}
			if afterBoot.InstallID != test.wantIDAfterAReboot {
				t.Fatalf("install ID after a reboot = %q, want %q", afterBoot.InstallID, test.wantIDAfterAReboot)
			}
			if got := kubeletRestartNeeded(false, afterBoot, opts); got != test.wantRestartOnBoot {
				t.Fatalf("kubeletRestartNeeded() after a reboot = %t, want %t", got, test.wantRestartOnBoot)
			}
		})
	}
}

func TestInstallWritesTheMarkerOnlyAfterTheKubeletRestartReturns(t *testing.T) {
	hostRoot := t.TempDir()
	opts := markerOptions(t, hostRoot, "new-revision")
	opts.RestartKubelet = true
	marker := writeMarkerFile(t, opts, "install-id=old-revision\ncompleted-at=2026-03-04T05:06:07Z\n")

	// Whatever the probe would have found while the restart was still running.
	// A slow restart only matters if this is empty: the probe greps the marker,
	// and anything in it that names this revision would report the pod Ready
	// and let maxUnavailable: 1 move the rollout on to the next node.
	restarted := false
	markerDuringRestart := func() string {
		data, err := os.ReadFile(marker)
		switch {
		case os.IsNotExist(err):
			return ""
		case err != nil:
			t.Fatalf("read marker during the restart: %v", err)
		}
		return string(data)
	}

	duringRestart := "not called"
	restart := func(options) error {
		restarted = true
		duringRestart = markerDuringRestart()
		return nil
	}

	if err := install(opts, restart); err != nil {
		t.Fatalf("install() error: %v", err)
	}
	if !restarted {
		t.Fatal("install() did not restart kubelet on a fresh node")
	}
	if duringRestart != "" {
		t.Fatalf("marker while the kubelet restart was running = %q, want it gone", duringRestart)
	}

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if first, _, _ := strings.Cut(string(data), "\n"); first != "install-id=new-revision" {
		t.Fatalf("marker first line = %q, want install-id=new-revision", first)
	}
}

func TestInstallWritesNoMarkerWhenTheKubeletRestartFails(t *testing.T) {
	hostRoot := t.TempDir()
	opts := markerOptions(t, hostRoot, "new-revision")
	opts.RestartKubelet = true
	marker := writeMarkerFile(t, opts, "install-id=old-revision\ncompleted-at=2026-03-04T05:06:07Z\n")

	restart := func(options) error { return errors.New("systemctl restart kubelet: exit status 1") }

	if err := install(opts, restart); err == nil {
		t.Fatal("install() returned nil error, want the kubelet restart error")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("stat marker after a failed restart: err = %v, want not-exist", err)
	}
}

// TestTurningRestartsBackOnRestartsKubeletOnce is the staged rollout done
// without the chart: install with RESTART_KUBELET=false, then turn it on. The
// install ID does not change between the two runs, because nothing outside the
// chart derives one from the values, and by the second run every host file is
// already correct. Only the marker knows kubelet was never restarted.
func TestTurningRestartsBackOnRestartsKubeletOnce(t *testing.T) {
	hostRoot := t.TempDir()
	opts := markerOptions(t, hostRoot, standaloneInstallID)
	opts.RestartKubelet = false

	// Mirrors restartKubelet, which returns without touching the node when
	// RESTART_KUBELET is false.
	restarts := 0
	restart := func(o options) error {
		if !o.RestartKubelet {
			return nil
		}
		restarts++
		return nil
	}

	if err := install(opts, restart); err != nil {
		t.Fatalf("install() with restarts off error: %v", err)
	}
	if restarts != 0 {
		t.Fatalf("kubelet restarts with restarts off = %d, want 0", restarts)
	}

	opts.RestartKubelet = true
	if err := install(opts, restart); err != nil {
		t.Fatalf("install() with restarts on error: %v", err)
	}
	if restarts != 1 {
		t.Fatalf("kubelet restarts after turning them on = %d, want 1", restarts)
	}

	// And the run after that leaves it alone: the marker now records the
	// restart, so a container coming back up does not take kubelet down again.
	if err := install(opts, restart); err != nil {
		t.Fatalf("install() on a settled node error: %v", err)
	}
	if restarts != 1 {
		t.Fatalf("kubelet restarts on a settled node = %d, want 1", restarts)
	}
}

func TestRemoveMarkerToleratesAMissingMarker(t *testing.T) {
	opts := markerOptions(t, t.TempDir(), "abc123")

	if err := removeMarker(opts); err != nil {
		t.Fatalf("removeMarker() error: %v", err)
	}
}

func TestKubeletRestartNeeded(t *testing.T) {
	restarted := func(id string) markerState {
		return markerState{InstallID: id, KubeletRestarted: true}
	}

	tests := []struct {
		name        string
		hostChanged bool
		previous    markerState
		installID   string
		// restartKubelet mirrors RESTART_KUBELET. It only decides anything
		// when the node is otherwise up to date.
		restartKubelet bool
		want           bool
	}{
		{name: "fresh node", hostChanged: true, installID: "abc", restartKubelet: true, want: true},
		{name: "host files changed", hostChanged: true, previous: restarted("abc"), installID: "abc", restartKubelet: true, want: true},
		{name: "container restarted, nothing changed", previous: restarted("abc"), installID: "abc", restartKubelet: true, want: false},
		{name: "new install ID, host already matches", previous: restarted("abc"), installID: "def", restartKubelet: true, want: true},
		{name: "marker predating install IDs", installID: "abc", restartKubelet: true, want: true},
		// The staged rollout without the chart: the same standalone install ID
		// on both runs, the files already in place, and kubelet still running
		// against the flags it had before the first run.
		{
			name:           "restarts turned back on after a run that skipped them",
			previous:       markerState{InstallID: standaloneInstallID},
			installID:      standaloneInstallID,
			restartKubelet: true,
			want:           true,
		},
		{
			name:           "restarts still off",
			previous:       markerState{InstallID: standaloneInstallID},
			installID:      standaloneInstallID,
			restartKubelet: false,
			want:           false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := options{InstallID: test.installID, RestartKubelet: test.restartKubelet}
			got := kubeletRestartNeeded(test.hostChanged, test.previous, opts)
			if got != test.want {
				t.Fatalf("kubeletRestartNeeded(%t, %+v, %q) = %t, want %t",
					test.hostChanged, test.previous, test.installID, got, test.want)
			}
		})
	}
}

func TestValidateOptionsRejectsUnusableInstallID(t *testing.T) {
	base := validOptions()

	// The probe matches the marker's first line whole, so anything that spans
	// lines would write a marker no probe can ever satisfy, and padding an ID
	// with whitespace makes a marker line nobody can read back by eye.
	for _, installID := range []string{"", "abc\n123", "abc\r123", "abc\n", " abc", "abc ", "\tabc", "abc\t"} {
		t.Run(fmt.Sprintf("%q", installID), func(t *testing.T) {
			opts := base
			opts.InstallID = installID
			if err := validateOptions(opts); err == nil {
				t.Fatal("validateOptions() returned nil error, want an install ID error")
			}
		})
	}
}

// TestRunLeavesTheMarkerTheInstallerCanReadBackExactly is the end of the
// install-ID round trip: whatever ID a run was given, the next run has to read
// that same ID back out of the marker and conclude the node is done. Reading it
// back differently is what makes an installer restart kubelet on every single
// container start, on a node where nothing has changed.
func TestRunLeavesTheMarkerTheInstallerCanReadBackExactly(t *testing.T) {
	// Every ID validateOptions accepts, including the shapes a hand-set
	// INSTALL_ID takes: the chart only ever passes a hex digest.
	for _, installID := range []string{"8a08abd5413fbe30", standaloneInstallID, "dev build 3", "a.b-c_d"} {
		t.Run(installID, func(t *testing.T) {
			opts := markerOptions(t, t.TempDir(), installID)

			if err := run(opts); err != nil {
				t.Fatalf("run() error: %v", err)
			}

			got, err := readMarker(opts)
			if err != nil {
				t.Fatalf("readMarker() error: %v", err)
			}
			if got.InstallID != installID {
				t.Fatalf("readMarker().InstallID = %q, want %q", got.InstallID, installID)
			}
			if kubeletRestartNeeded(false, got, opts) {
				t.Fatal("kubeletRestartNeeded() = true after a completed install, want false")
			}
		})
	}
}

// TestValidateOptionsRejectsPathsThatNameADirectory covers the values Helm
// would accept but the installer cannot use. Each of these paths names
// something the installer creates or overwrites inside a parent directory, so
// the host root, or any path with a trailing slash, has to be rejected before
// the run starts rather than fail halfway through it.
func TestValidateOptionsRejectsPathsThatNameADirectory(t *testing.T) {
	base := validOptions()

	fields := map[string]func(*options, string){
		"SOURCE_BINARY":    func(o *options, v string) { o.SourceBinary = v },
		"BIN_DIR":          func(o *options, v string) { o.BinDir = v },
		"CONFIG_PATH":      func(o *options, v string) { o.ConfigPath = v },
		"INSTALLED_MARKER": func(o *options, v string) { o.InstalledMarker = v },
	}

	for name, set := range fields {
		for _, path := range []string{"/", "/var/lib/credential-provider-harbor/"} {
			t.Run(name+" "+path, func(t *testing.T) {
				opts := base
				set(&opts, path)
				if err := validateOptions(opts); err == nil {
					t.Fatalf("validateOptions() with %s=%q returned nil error, want a path error", name, path)
				}
			})
		}
	}
}

// HOST_ROOT is the exception: it names a directory, and "/" is what a run
// outside a container passes.
func TestValidateOptionsAcceptsHostRootOfSlash(t *testing.T) {
	opts := validOptions()
	opts.HostRoot = "/"

	if err := validateOptions(opts); err != nil {
		t.Fatalf("validateOptions() with HOST_ROOT=/ error: %v", err)
	}
}

// TestInstallRejectsARootMarkerBeforeTouchingTheHost pins the ordering that
// makes the rejection worth having: the check runs before anything is written
// or removed, so a bad marker path cannot leave a node half-installed.
func TestInstallRejectsARootMarkerBeforeTouchingTheHost(t *testing.T) {
	hostRoot := t.TempDir()
	opts := markerOptions(t, hostRoot, "new-revision")
	opts.InstalledMarker = "/"

	restarted := false
	err := install(opts, func(options) error {
		restarted = true
		return nil
	})
	if err == nil {
		t.Fatal("install() returned nil error, want a marker path error")
	}
	// The message, not just the failure: reading "/" as a file or removing it
	// fails too, and either would end the run with an empty host root and no
	// restart. Only validateOptions names the variable and says what is wrong
	// with it, so this is what tells the two apart.
	want := `INSTALLED_MARKER must name a path inside a parent directory, not "/"`
	if err.Error() != want {
		t.Fatalf("install() error = %q, want %q", err, want)
	}
	if restarted {
		t.Fatal("install() restarted kubelet despite an unusable marker path")
	}
	entries, readErr := os.ReadDir(hostRoot)
	if readErr != nil {
		t.Fatalf("read host root: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("install() wrote %d entries to the host root, want none", len(entries))
	}
}

func TestValidateOptionsRejectsPathsThatBreakTheFilesTheyGoInto(t *testing.T) {
	base := options{
		HostRoot:        "/host",
		SourceBinary:    "/usr/local/bin/credential-provider-harbor",
		BinaryName:      providerName,
		BinDir:          "/usr/local/bin/credential-providers",
		ConfigPath:      "/etc/kubernetes/credential-providers/config.yaml",
		ConfigFormat:    "yaml",
		MatchImages:     []string{"harbor.example.com"},
		InstalledMarker: "/var/run/credential-provider-harbor-installed",
		InstallID:       standaloneInstallID,
	}

	if err := validateOptions(base); err != nil {
		t.Fatalf("validateOptions() on ordinary paths: %v", err)
	}

	// Each of these ends up inside a systemd Environment= line, a kind
	// ExecStart line, a YAML drop-in, the MicroK8s arguments file, or the AKS
	// KUBELET_FLAGS assignment, and breaks at least one of them.
	for _, path := range []string{
		`/opt/a b/providers`,
		"/opt/a\tb/providers",
		`/opt/it's/providers`,
		`/opt/say"hi/providers`,
		`/opt/back\slash/providers`,
		"/opt/new\nline/providers",
	} {
		t.Run("binDir "+path, func(t *testing.T) {
			opts := base
			opts.BinDir = path
			if err := validateOptions(opts); err == nil {
				t.Fatal("validateOptions() returned nil error, want an unsafe path error")
			}
		})
		t.Run("configPath "+path, func(t *testing.T) {
			opts := base
			opts.ConfigPath = path
			if err := validateOptions(opts); err == nil {
				t.Fatal("validateOptions() returned nil error, want an unsafe path error")
			}
		})
	}
}
