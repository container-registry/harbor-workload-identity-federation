package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

const (
	providerName = "credential-provider-harbor"

	configAPIVersion   = "kubelet.config.k8s.io/v1"
	providerAPIVersion = "credentialprovider.kubelet.k8s.io/v1"
)

type options struct {
	Profile             string
	HostRoot            string
	SourceBinary        string
	BinaryName          string
	BinDir              string
	ConfigPath          string
	ConfigFormat        string
	RegistryHost        string
	RegistryAudience    string
	RegistryUsername    string
	MatchImages         []string
	CacheDuration       string
	ConfigureKubelet    bool
	RestartKubelet      bool
	KubeletService      string
	SystemdDropInPath   string
	K3sConfigDropInPath string
	PreserveECRProvider bool
	SleepForever        bool
	InstalledMarker     string
}

type profileDefaults struct {
	BinDir              string
	ConfigPath          string
	ConfigFormat        string
	KubeletService      string
	SystemdDropInPath   string
	K3sConfigDropInPath string
	PreserveECRProvider bool
}

type credentialProviderConfig struct {
	APIVersion string               `json:"apiVersion" yaml:"apiVersion"`
	Kind       string               `json:"kind" yaml:"kind"`
	Providers  []credentialProvider `json:"providers" yaml:"providers"`
}

type credentialProvider struct {
	Name                 string           `json:"name" yaml:"name"`
	APIVersion           string           `json:"apiVersion" yaml:"apiVersion"`
	MatchImages          []string         `json:"matchImages" yaml:"matchImages"`
	DefaultCacheDuration string           `json:"defaultCacheDuration" yaml:"defaultCacheDuration"`
	Args                 []string         `json:"args,omitempty" yaml:"args,omitempty"`
	Env                  []execEnvVar     `json:"env,omitempty" yaml:"env,omitempty"`
	TokenAttributes      *tokenAttributes `json:"tokenAttributes,omitempty" yaml:"tokenAttributes,omitempty"`
}

type execEnvVar struct {
	Name  string `json:"name" yaml:"name"`
	Value string `json:"value" yaml:"value"`
}

type tokenAttributes struct {
	RequireServiceAccount             bool     `json:"requireServiceAccount" yaml:"requireServiceAccount"`
	ServiceAccountTokenAudience       string   `json:"serviceAccountTokenAudience" yaml:"serviceAccountTokenAudience"`
	CacheType                         string   `json:"cacheType" yaml:"cacheType"`
	RequiredServiceAccountAnnotations []string `json:"requiredServiceAccountAnnotationKeys,omitempty" yaml:"requiredServiceAccountAnnotationKeys,omitempty"`
	OptionalServiceAccountAnnotations []string `json:"optionalServiceAccountAnnotationKeys,omitempty" yaml:"optionalServiceAccountAnnotationKeys,omitempty"`
}

func main() {
	opts, err := optionsFromEnv()
	if err != nil {
		exit(err)
	}

	if err := run(opts); err != nil {
		exit(err)
	}

	if opts.SleepForever {
		for {
			time.Sleep(time.Hour)
		}
	}
}

func exit(err error) {
	fmt.Fprintf(os.Stderr, "[ERROR] %v\n", err)
	os.Exit(1)
}

func optionsFromEnv() (options, error) {
	profile := strings.ToLower(env("PROFILE", "generic"))
	defaults, err := defaultsForProfile(profile)
	if err != nil {
		return options{}, err
	}

	registryHost := env("REGISTRY_HOST", "")
	if registryHost == "" {
		return options{}, errors.New("REGISTRY_HOST is required")
	}

	configPath := env("CONFIG_PATH", "")
	if configPath == "" {
		configDir := env("CONFIG_DIR", "")
		configFile := env("CONFIG_FILE", "")
		if configDir != "" || configFile != "" {
			if configDir == "" {
				configDir = filepath.Dir(defaults.ConfigPath)
			}
			if configFile == "" {
				configFile = filepath.Base(defaults.ConfigPath)
			}
			configPath = filepath.Join(configDir, configFile)
		}
	}
	if configPath == "" {
		configPath = defaults.ConfigPath
	}

	binDir := env("BIN_DIR", defaults.BinDir)
	configFormat := strings.ToLower(os.Getenv("CONFIG_FORMAT"))
	if configFormat == "" && configPath != defaults.ConfigPath {
		configFormat = configFormatForPath(configPath)
	}
	if configFormat == "" {
		configFormat = defaults.ConfigFormat
	}

	preserveECR := defaults.PreserveECRProvider
	if value, ok := os.LookupEnv("PRESERVE_ECR_PROVIDER"); ok {
		preserveECR = boolValue(value)
	}

	return options{
		Profile:             profile,
		HostRoot:            env("HOST_ROOT", "/host"),
		SourceBinary:        env("SOURCE_BINARY", "/usr/local/bin/credential-provider-harbor"),
		BinaryName:          env("BINARY_NAME", providerName),
		BinDir:              binDir,
		ConfigPath:          configPath,
		ConfigFormat:        configFormat,
		RegistryHost:        registryHost,
		RegistryAudience:    env("REGISTRY_AUDIENCE", registryHost),
		RegistryUsername:    env("REGISTRY_USERNAME", "jwt"),
		MatchImages:         listValue(env("MATCH_IMAGES", registryHost)),
		CacheDuration:       env("CACHE_DURATION", "1h"),
		ConfigureKubelet:    boolEnv("CONFIGURE_KUBELET", true),
		RestartKubelet:      boolEnv("RESTART_KUBELET", true),
		KubeletService:      env("KUBELET_SERVICE", defaults.KubeletService),
		SystemdDropInPath:   env("SYSTEMD_DROP_IN_PATH", defaults.SystemdDropInPath),
		K3sConfigDropInPath: env("K3S_CONFIG_DROP_IN_PATH", defaults.K3sConfigDropInPath),
		PreserveECRProvider: preserveECR,
		SleepForever:        boolEnv("SLEEP_FOREVER", false),
		InstalledMarker:     env("INSTALLED_MARKER", "/var/run/credential-provider-harbor-installed"),
	}, nil
}

func defaultsForProfile(profile string) (profileDefaults, error) {
	switch profile {
	case "generic", "custom", "gke":
		return profileDefaults{
			BinDir:         "/usr/local/bin/credential-providers",
			ConfigPath:     "/etc/kubernetes/credential-providers/config.yaml",
			ConfigFormat:   "yaml",
			KubeletService: "kubelet",
		}, nil
	case "eks", "aws":
		return profileDefaults{
			BinDir:              "/etc/eks/image-credential-provider",
			ConfigPath:          "/etc/eks/image-credential-provider/config.json",
			ConfigFormat:        "json",
			KubeletService:      "kubelet",
			PreserveECRProvider: true,
		}, nil
	case "k3s", "k3d":
		return profileDefaults{
			BinDir:              "/var/lib/rancher/credentialprovider/bin",
			ConfigPath:          "/var/lib/rancher/credentialprovider/config.yaml",
			ConfigFormat:        "yaml",
			KubeletService:      "k3s",
			K3sConfigDropInPath: "/etc/rancher/k3s/config.yaml.d/99-credential-provider-harbor.yaml",
		}, nil
	case "kind":
		return profileDefaults{
			BinDir:         "/var/lib/kubelet/credential-provider",
			ConfigPath:     "/var/lib/kubelet/credential-provider-config.yaml",
			ConfigFormat:   "yaml",
			KubeletService: "kubelet",
		}, nil
	default:
		return profileDefaults{}, fmt.Errorf("unsupported PROFILE %q", profile)
	}
}

func run(opts options) error {
	fmt.Println("[INFO] === credential-provider-harbor installer ===")
	fmt.Printf("[INFO] Profile: %s\n", opts.Profile)
	fmt.Printf("[INFO] Registry: %s\n", opts.RegistryHost)
	fmt.Printf("[INFO] Audience: %s\n", opts.RegistryAudience)
	fmt.Printf("[INFO] Host binary: %s\n", opts.BinDir+"/"+opts.BinaryName)
	fmt.Printf("[INFO] Host config: %s\n", opts.ConfigPath)
	fmt.Printf("[INFO] Kubelet configure: %t\n", opts.ConfigureKubelet)
	fmt.Printf("[INFO] Kubelet restart: %t\n", opts.RestartKubelet)

	if err := validateOptions(opts); err != nil {
		return err
	}

	changed := false
	binaryChanged, err := installBinary(opts)
	if err != nil {
		return err
	}
	changed = changed || binaryChanged

	configChanged, err := installCredentialProviderConfig(opts)
	if err != nil {
		return err
	}
	changed = changed || configChanged

	kubeletChanged, err := configureKubelet(opts)
	if err != nil {
		return err
	}
	changed = changed || kubeletChanged

	if err := touchMarker(opts); err != nil {
		return err
	}

	if changed {
		if err := restartKubelet(opts); err != nil {
			return err
		}
	} else {
		fmt.Println("[INFO] No host changes detected")
	}

	fmt.Println("[INFO] Installation complete")
	return nil
}

func validateOptions(opts options) error {
	paths := map[string]string{
		"HOST_ROOT":        opts.HostRoot,
		"SOURCE_BINARY":    opts.SourceBinary,
		"BIN_DIR":          opts.BinDir,
		"CONFIG_PATH":      opts.ConfigPath,
		"INSTALLED_MARKER": opts.InstalledMarker,
	}
	for name, path := range paths {
		if path == "" {
			return fmt.Errorf("%s cannot be empty", name)
		}
		if !filepath.IsAbs(path) {
			return fmt.Errorf("%s must be an absolute path: %q", name, path)
		}
	}
	if opts.ConfigFormat != "yaml" && opts.ConfigFormat != "json" {
		return fmt.Errorf("CONFIG_FORMAT must be yaml or json, got %q", opts.ConfigFormat)
	}
	if len(opts.MatchImages) == 0 {
		return errors.New("at least one match image is required")
	}
	return nil
}

func installBinary(opts options) (bool, error) {
	if _, err := os.Stat(opts.SourceBinary); err != nil {
		return false, fmt.Errorf("stat source binary: %w", err)
	}

	targetDir := hostPath(opts, opts.BinDir)
	target := filepath.Join(targetDir, opts.BinaryName)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return false, fmt.Errorf("create host binary directory: %w", err)
	}

	equal, err := sameFileContent(opts.SourceBinary, target)
	if err != nil {
		return false, err
	}
	if equal {
		fmt.Printf("[INFO] Binary already up to date: %s\n", target)
		return false, nil
	}

	if err := copyFile(opts.SourceBinary, target, 0755); err != nil {
		return false, fmt.Errorf("install binary: %w", err)
	}
	fmt.Printf("[INFO] Installed binary: %s\n", target)
	return true, nil
}

func installCredentialProviderConfig(opts options) (bool, error) {
	hostConfigPath := hostPath(opts, opts.ConfigPath)
	if err := os.MkdirAll(filepath.Dir(hostConfigPath), 0755); err != nil {
		return false, fmt.Errorf("create credential provider config directory: %w", err)
	}

	cfg, err := readCredentialProviderConfig(hostConfigPath, opts.ConfigFormat)
	if err != nil {
		return false, err
	}

	cfg = mergeProvider(cfg, harborProvider(opts))
	if opts.PreserveECRProvider {
		cfg = ensureProvider(cfg, ecrProvider())
	}

	data, err := marshalConfig(cfg, opts.ConfigFormat)
	if err != nil {
		return false, err
	}

	changed, err := writeFileIfChanged(hostConfigPath, data, 0644, true)
	if err != nil {
		return false, fmt.Errorf("write credential provider config: %w", err)
	}
	if changed {
		fmt.Printf("[INFO] Wrote credential provider config: %s\n", hostConfigPath)
	} else {
		fmt.Printf("[INFO] Credential provider config already up to date: %s\n", hostConfigPath)
	}
	return changed, nil
}

func configureKubelet(opts options) (bool, error) {
	if !opts.ConfigureKubelet {
		fmt.Printf("[WARN] Kubelet configuration disabled. Ensure kubelet uses --image-credential-provider-bin-dir=%s and --image-credential-provider-config=%s\n", opts.BinDir, opts.ConfigPath)
		return false, nil
	}

	switch opts.Profile {
	case "eks", "aws":
		fmt.Println("[INFO] EKS profile uses the AMI credential-provider path; no kubelet flag drop-in required")
		return false, nil
	case "k3s", "k3d":
		return configureK3s(opts)
	default:
		return configureSystemdKubelet(opts)
	}
}

func configureSystemdKubelet(opts options) (bool, error) {
	service := opts.KubeletService
	if service == "" {
		service = "kubelet"
	}
	dropInPath := opts.SystemdDropInPath
	if dropInPath == "" {
		dropInPath = filepath.Join("/etc/systemd/system", service+".service.d", "99-credential-provider-harbor.conf")
	}

	content := fmt.Sprintf(`[Service]
Environment="KUBELET_EXTRA_ARGS=--image-credential-provider-bin-dir=%s --image-credential-provider-config=%s"
`, opts.BinDir, opts.ConfigPath)

	hostDropInPath := hostPath(opts, dropInPath)
	if err := os.MkdirAll(filepath.Dir(hostDropInPath), 0755); err != nil {
		return false, fmt.Errorf("create kubelet systemd drop-in directory: %w", err)
	}
	changed, err := writeFileIfChanged(hostDropInPath, []byte(content), 0644, true)
	if err != nil {
		return false, fmt.Errorf("write kubelet systemd drop-in: %w", err)
	}
	if changed {
		fmt.Printf("[INFO] Wrote kubelet systemd drop-in: %s\n", hostDropInPath)
	} else {
		fmt.Printf("[INFO] Kubelet systemd drop-in already up to date: %s\n", hostDropInPath)
	}
	return changed, nil
}

func configureK3s(opts options) (bool, error) {
	dropInPath := opts.K3sConfigDropInPath
	if dropInPath == "" {
		dropInPath = "/etc/rancher/k3s/config.yaml.d/99-credential-provider-harbor.yaml"
	}

	content := fmt.Sprintf(`image-credential-provider-bin-dir: %q
image-credential-provider-config: %q
`, opts.BinDir, opts.ConfigPath)

	hostDropInPath := hostPath(opts, dropInPath)
	if err := os.MkdirAll(filepath.Dir(hostDropInPath), 0755); err != nil {
		return false, fmt.Errorf("create k3s config drop-in directory: %w", err)
	}
	changed, err := writeFileIfChanged(hostDropInPath, []byte(content), 0644, true)
	if err != nil {
		return false, fmt.Errorf("write k3s config drop-in: %w", err)
	}
	if changed {
		fmt.Printf("[INFO] Wrote k3s config drop-in: %s\n", hostDropInPath)
	} else {
		fmt.Printf("[INFO] k3s config drop-in already up to date: %s\n", hostDropInPath)
	}
	return changed, nil
}

func restartKubelet(opts options) error {
	if !opts.RestartKubelet {
		fmt.Println("[WARN] Kubelet restart disabled. Restart or roll nodes before testing image pulls.")
		return nil
	}

	service := opts.KubeletService
	if opts.Profile == "k3s" || opts.Profile == "k3d" {
		service = detectK3sService(opts, service)
	}
	if service == "" {
		service = "kubelet"
	}

	fmt.Printf("[INFO] Restarting %s\n", service)
	if opts.ConfigureKubelet && opts.Profile != "eks" && opts.Profile != "aws" {
		if err := systemctl("daemon-reload"); err != nil {
			return err
		}
	}
	return systemctl("restart", service)
}

func systemctl(args ...string) error {
	cmdArgs := append([]string{"-t", "1", "-m", "-u", "-i", "-n", "-p", "--", "systemctl"}, args...)
	cmd := exec.Command("nsenter", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err == nil {
		return nil
	} else if !errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("nsenter systemctl %s: %w", strings.Join(args, " "), err)
	}

	cmd = exec.Command("systemctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func detectK3sService(opts options, fallback string) string {
	if fallback != "" && fallback != "k3s" {
		return fallback
	}
	if fileExists(hostPath(opts, "/etc/systemd/system/k3s-agent.service")) {
		return "k3s-agent"
	}
	if fileExists(hostPath(opts, "/etc/systemd/system/k3s.service")) {
		return "k3s"
	}
	return "k3s"
}

func touchMarker(opts options) error {
	marker := hostPath(opts, opts.InstalledMarker)
	if err := os.MkdirAll(filepath.Dir(marker), 0755); err != nil {
		return fmt.Errorf("create marker directory: %w", err)
	}
	if err := os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0644); err != nil {
		return fmt.Errorf("write marker file: %w", err)
	}
	return nil
}

func harborProvider(opts options) credentialProvider {
	requireServiceAccount := true
	return credentialProvider{
		Name:                 opts.BinaryName,
		APIVersion:           providerAPIVersion,
		MatchImages:          opts.MatchImages,
		DefaultCacheDuration: opts.CacheDuration,
		Args:                 []string{"--username=" + opts.RegistryUsername},
		TokenAttributes: &tokenAttributes{
			RequireServiceAccount:       requireServiceAccount,
			ServiceAccountTokenAudience: opts.RegistryAudience,
			CacheType:                   "Token",
		},
	}
}

func ecrProvider() credentialProvider {
	return credentialProvider{
		Name:                 "ecr-credential-provider",
		APIVersion:           providerAPIVersion,
		DefaultCacheDuration: "12h0m0s",
		MatchImages: []string{
			"*.dkr.ecr.*.amazonaws.com",
			"*.dkr-ecr.*.on.aws",
			"*.dkr.ecr.*.amazonaws.com.cn",
			"*.dkr-ecr.*.on.amazonwebservices.com.cn",
			"*.dkr.ecr-fips.*.amazonaws.com",
			"*.dkr-ecr-fips.*.on.aws",
			"*.dkr.ecr.*.c2s.ic.gov",
			"*.dkr.ecr.*.sc2s.sgov.gov",
			"*.dkr.ecr.*.cloud.adc-e.uk",
			"*.dkr.ecr.*.csp.hci.ic.gov",
			"*.dkr.ecr.*.amazonaws.eu",
			"public.ecr.aws",
		},
	}
}

func readCredentialProviderConfig(path, format string) (credentialProviderConfig, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return newCredentialProviderConfig(), nil
	}
	if err != nil {
		return credentialProviderConfig{}, fmt.Errorf("read credential provider config: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return newCredentialProviderConfig(), nil
	}

	cfg := newCredentialProviderConfig()
	switch format {
	case "json":
		if err := json.Unmarshal(data, &cfg); err != nil {
			return credentialProviderConfig{}, fmt.Errorf("parse credential provider JSON config %s: %w", path, err)
		}
	case "yaml":
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return credentialProviderConfig{}, fmt.Errorf("parse credential provider YAML config %s: %w", path, err)
		}
	default:
		return credentialProviderConfig{}, fmt.Errorf("unsupported config format %q", format)
	}
	if cfg.APIVersion == "" {
		cfg.APIVersion = configAPIVersion
	}
	if cfg.Kind == "" {
		cfg.Kind = "CredentialProviderConfig"
	}
	return cfg, nil
}

func newCredentialProviderConfig() credentialProviderConfig {
	return credentialProviderConfig{
		APIVersion: configAPIVersion,
		Kind:       "CredentialProviderConfig",
		Providers:  []credentialProvider{},
	}
}

func mergeProvider(cfg credentialProviderConfig, provider credentialProvider) credentialProviderConfig {
	providers := make([]credentialProvider, 0, len(cfg.Providers)+1)
	providers = append(providers, provider)
	for _, existing := range cfg.Providers {
		if existing.Name != provider.Name {
			providers = append(providers, existing)
		}
	}
	cfg.Providers = providers
	return cfg
}

func ensureProvider(cfg credentialProviderConfig, provider credentialProvider) credentialProviderConfig {
	for _, existing := range cfg.Providers {
		if existing.Name == provider.Name {
			return cfg
		}
	}
	cfg.Providers = append(cfg.Providers, provider)
	return cfg
}

func marshalConfig(cfg credentialProviderConfig, format string) ([]byte, error) {
	switch format {
	case "json":
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal credential provider JSON config: %w", err)
		}
		return append(data, '\n'), nil
	case "yaml":
		data, err := yaml.Marshal(cfg)
		if err != nil {
			return nil, fmt.Errorf("marshal credential provider YAML config: %w", err)
		}
		return data, nil
	default:
		return nil, fmt.Errorf("unsupported config format %q", format)
	}
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := fmt.Sprintf("%s.tmp.%d", dst, os.Getpid())
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func writeFileIfChanged(path string, data []byte, mode os.FileMode, backup bool) (bool, error) {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, data) {
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}

	if backup && err == nil {
		backupPath := fmt.Sprintf("%s.bak.%d", path, time.Now().Unix())
		if err := os.WriteFile(backupPath, existing, mode); err != nil {
			return false, fmt.Errorf("write backup %s: %w", backupPath, err)
		}
	}

	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return false, err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, nil
}

func sameFileContent(left, right string) (bool, error) {
	leftData, err := os.ReadFile(left)
	if err != nil {
		return false, err
	}
	rightData, err := os.ReadFile(right)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return bytes.Equal(leftData, rightData), nil
}

func hostPath(opts options, path string) string {
	root := strings.TrimRight(opts.HostRoot, "/")
	if root == "" || root == "/" {
		return path
	}
	return root + path
}

func configFormatForPath(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return "json"
	}
	return "yaml"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func env(name, defaultValue string) string {
	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		return defaultValue
	}
	return value
}

func boolEnv(name string, defaultValue bool) bool {
	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		return defaultValue
	}
	return boolValue(value)
}

func boolValue(value string) bool {
	switch strings.ToLower(value) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}

func listValue(value string) []string {
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}
