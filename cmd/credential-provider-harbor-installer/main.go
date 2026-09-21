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

// version is stamped at build time with -ldflags "-X main.version=<tag>". The
// installer ships as a release binary and as the deployer image entrypoint, so
// it reports the same version the provider binary does.
var version = "dev"

const (
	providerName = "credential-provider-harbor"

	configAPIVersion   = "kubelet.config.k8s.io/v1"
	providerAPIVersion = "credentialprovider.kubelet.k8s.io/v1"

	binDirFlag = "--image-credential-provider-bin-dir"
	configFlag = "--image-credential-provider-config"

	// markerInstallIDPrefix starts the first line of the marker file. The
	// readiness probe greps for this prefix plus the install ID of the pod it
	// runs in, so a marker left behind by a different revision cannot pass it.
	markerInstallIDPrefix = "install-id="

	// markerKubeletRestartedPrefix records whether kubelet was restarted for
	// the install named on the first line. A run with RESTART_KUBELET=false
	// writes false, so a later run that turns restarts back on can tell that
	// the node still has to pick the flags up even though nothing else
	// changed.
	markerKubeletRestartedPrefix = "kubelet-restarted="

	// standaloneInstallID is the install ID used when the installer runs
	// outside the chart, which is the only way INSTALL_ID goes unset.
	standaloneInstallID = "standalone"

	// defaultInstalledMarker is where a finished node is recorded. It has to
	// live on persistent storage. /var/run is /run on systemd hosts, which is
	// tmpfs and is emptied at every boot, so a marker kept there would be gone
	// after a reboot the node did not need: kubelet comes back up with the
	// drop-in already in effect, but the missing marker reads as "never
	// installed" and the installer restarts kubelet for a configuration
	// kubelet already started against, leaving the node NotReady for the
	// length of that restart on every single boot.
	defaultInstalledMarker = "/var/lib/credential-provider-harbor/install-marker"
)

type options struct {
	Profile               string
	HostRoot              string
	SourceBinary          string
	BinaryName            string
	BinDir                string
	ConfigPath            string
	ConfigFormat          string
	RegistryHost          string
	RegistryAudience      string
	RegistryUsername      string
	MatchImages           []string
	CacheDuration         string
	ConfigureKubelet      bool
	RestartKubelet        bool
	ForceKubeletExecStart bool
	KubeletService        string
	SystemdDropInPath     string
	K3sConfigDropInPath   string
	RKE2ConfigDropInPath  string
	KubeletDefaultsPath   string
	MicroK8sArgsPath      string
	PreserveECRProvider   bool
	SleepForever          bool
	InstalledMarker       string
	InstallID             string
}

type profileDefaults struct {
	BinDir               string
	ConfigPath           string
	ConfigFormat         string
	KubeletService       string
	SystemdDropInPath    string
	K3sConfigDropInPath  string
	RKE2ConfigDropInPath string
	KubeletDefaultsPath  string
	MicroK8sArgsPath     string
	PreserveECRProvider  bool
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
	// The installer is configured through the environment and takes no flags,
	// so -version is handled here rather than through a flag set that would
	// start rejecting the arguments the DaemonSet may pass.
	for _, arg := range os.Args[1:] {
		if arg == "-version" || arg == "--version" {
			fmt.Println(version)
			return
		}
	}

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
		Profile:               profile,
		HostRoot:              env("HOST_ROOT", "/host"),
		SourceBinary:          env("SOURCE_BINARY", "/usr/local/bin/credential-provider-harbor"),
		BinaryName:            env("BINARY_NAME", providerName),
		BinDir:                binDir,
		ConfigPath:            configPath,
		ConfigFormat:          configFormat,
		RegistryHost:          registryHost,
		RegistryAudience:      env("REGISTRY_AUDIENCE", registryHost),
		RegistryUsername:      env("REGISTRY_USERNAME", "jwt"),
		MatchImages:           listValue(env("MATCH_IMAGES", registryHost)),
		CacheDuration:         env("CACHE_DURATION", "1h"),
		ConfigureKubelet:      boolEnv("CONFIGURE_KUBELET", true),
		RestartKubelet:        boolEnv("RESTART_KUBELET", true),
		ForceKubeletExecStart: boolEnv("FORCE_KUBELET_EXECSTART", false),
		KubeletService:        env("KUBELET_SERVICE", defaults.KubeletService),
		SystemdDropInPath:     env("SYSTEMD_DROP_IN_PATH", defaults.SystemdDropInPath),
		K3sConfigDropInPath:   env("K3S_CONFIG_DROP_IN_PATH", defaults.K3sConfigDropInPath),
		RKE2ConfigDropInPath:  env("RKE2_CONFIG_DROP_IN_PATH", defaults.RKE2ConfigDropInPath),
		KubeletDefaultsPath:   env("KUBELET_DEFAULTS_PATH", defaults.KubeletDefaultsPath),
		MicroK8sArgsPath:      env("MICROK8S_KUBELET_ARGS_PATH", defaults.MicroK8sArgsPath),
		PreserveECRProvider:   preserveECR,
		SleepForever:          boolEnv("SLEEP_FOREVER", false),
		InstalledMarker:       env("INSTALLED_MARKER", defaultInstalledMarker),
		InstallID:             env("INSTALL_ID", standaloneInstallID),
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
	case "rke2":
		return profileDefaults{
			BinDir:               "/var/lib/rancher/credentialprovider/bin",
			ConfigPath:           "/var/lib/rancher/credentialprovider/config.yaml",
			ConfigFormat:         "yaml",
			KubeletService:       "rke2-agent",
			RKE2ConfigDropInPath: "/etc/rancher/rke2/config.yaml.d/99-credential-provider-harbor.yaml",
		}, nil
	case "kind":
		return profileDefaults{
			BinDir:         "/var/lib/kubelet/credential-provider",
			ConfigPath:     "/var/lib/kubelet/credential-provider-config.yaml",
			ConfigFormat:   "yaml",
			KubeletService: "kubelet",
		}, nil
	case "aks":
		return profileDefaults{
			BinDir:              "/usr/local/bin/credential-providers",
			ConfigPath:          "/etc/kubernetes/credential-providers/config.yaml",
			ConfigFormat:        "yaml",
			KubeletService:      "kubelet",
			KubeletDefaultsPath: "/etc/default/kubelet",
		}, nil
	case "microk8s":
		// The snap's writable tree, not /usr/local/bin: confinement keeps
		// kubelite out of the latter, and /var/snap/microk8s/common survives
		// a snap refresh while /var/snap/microk8s/current does not.
		return profileDefaults{
			BinDir:           "/var/snap/microk8s/common/credentialprovider/bin",
			ConfigPath:       "/var/snap/microk8s/common/credentialprovider/config.yaml",
			ConfigFormat:     "yaml",
			KubeletService:   "snap.microk8s.daemon-kubelite",
			MicroK8sArgsPath: "/var/snap/microk8s/current/args/kubelet",
		}, nil
	default:
		return profileDefaults{}, fmt.Errorf("unsupported PROFILE %q", profile)
	}
}

func run(opts options) error {
	return install(opts, restartKubelet)
}

// install does the node install, with the kubelet restart passed in. The marker
// the readiness probe greps is written last, after the restart has returned,
// and the seam is here so a test can prove that ordering rather than argue it:
// a restart that takes minutes, or never succeeds, must not leave a marker
// behind that reports the pod Ready and lets the rollout move to the next node.
func install(opts options, restart func(options) error) error {
	fmt.Println("[INFO] === credential-provider-harbor installer ===")
	fmt.Printf("[INFO] Version: %s\n", version)
	fmt.Printf("[INFO] Profile: %s\n", opts.Profile)
	fmt.Printf("[INFO] Registry: %s\n", opts.RegistryHost)
	fmt.Printf("[INFO] Audience: %s\n", opts.RegistryAudience)
	fmt.Printf("[INFO] Host binary: %s\n", opts.BinDir+"/"+opts.BinaryName)
	fmt.Printf("[INFO] Host config: %s\n", opts.ConfigPath)
	fmt.Printf("[INFO] Kubelet configure: %t\n", opts.ConfigureKubelet)
	fmt.Printf("[INFO] Kubelet restart: %t\n", opts.RestartKubelet)
	fmt.Printf("[INFO] Install ID: %s\n", opts.InstallID)

	if err := validateOptions(opts); err != nil {
		return err
	}

	// What the node last completed, read before the marker is cleared.
	previous, err := readMarker(opts)
	if err != nil {
		return err
	}
	if previous.InstallID != "" {
		fmt.Printf("[INFO] Previous install ID on this node: %s\n", previous.InstallID)
	}

	// Readiness means "this pod finished its own install", so the marker has to
	// go before any work starts. Otherwise the probe passes off the previous
	// revision's marker and the rollout advances to the next node while this
	// installer is still working, or crash-looping.
	if err := removeMarker(opts); err != nil {
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

	if !changed {
		fmt.Println("[INFO] No host changes detected")
	}

	// Carried over only when this run is the same install as the one that
	// wrote the marker. A different install ID rewrites the node's flags, so
	// whatever restart happened for the previous one no longer counts.
	restarted := previous.KubeletRestarted && previous.InstallID == opts.InstallID
	if kubeletRestartNeeded(changed, previous, opts) {
		if err := restart(opts); err != nil {
			return err
		}
		restarted = opts.RestartKubelet
	}

	// Last, and only once the kubelet restart has returned. The probe turns the
	// pod Ready off this file, and Ready has to mean the node is done.
	if err := writeMarker(opts, restarted); err != nil {
		return err
	}

	fmt.Println("[INFO] Installation complete")
	return nil
}

func validateOptions(opts options) error {
	if opts.BinaryName == "" {
		return errors.New("BINARY_NAME cannot be empty")
	}
	if opts.BinaryName == "." || opts.BinaryName == ".." || filepath.Base(opts.BinaryName) != opts.BinaryName {
		return fmt.Errorf("BINARY_NAME must be a plain filename: %q", opts.BinaryName)
	}

	// HOST_ROOT is the one path that may be the root itself: a run outside a
	// container passes "/". Everything else names something the installer
	// creates or overwrites inside a parent directory, so "/" or a trailing
	// slash there would have it copy the binary over the host root, or write
	// the config and the marker at a path that is a directory — an error
	// partway through the run instead of before it touched anything.
	named := map[string]string{
		"SOURCE_BINARY":    opts.SourceBinary,
		"BIN_DIR":          opts.BinDir,
		"CONFIG_PATH":      opts.ConfigPath,
		"INSTALLED_MARKER": opts.InstalledMarker,
	}
	paths := map[string]string{"HOST_ROOT": opts.HostRoot}
	for name, path := range named {
		paths[name] = path
	}
	for name, path := range paths {
		if path == "" {
			return fmt.Errorf("%s cannot be empty", name)
		}
		if !filepath.IsAbs(path) {
			return fmt.Errorf("%s must be an absolute path: %q", name, path)
		}
	}
	for name, path := range named {
		if path == "/" || strings.HasSuffix(path, "/") {
			return fmt.Errorf("%s must name a path inside a parent directory, not %q", name, path)
		}
	}

	optionalPaths := map[string]string{
		"SYSTEMD_DROP_IN_PATH":       opts.SystemdDropInPath,
		"K3S_CONFIG_DROP_IN_PATH":    opts.K3sConfigDropInPath,
		"RKE2_CONFIG_DROP_IN_PATH":   opts.RKE2ConfigDropInPath,
		"KUBELET_DEFAULTS_PATH":      opts.KubeletDefaultsPath,
		"MICROK8S_KUBELET_ARGS_PATH": opts.MicroK8sArgsPath,
	}
	for name, path := range optionalPaths {
		if path != "" && !filepath.IsAbs(path) {
			return fmt.Errorf("%s must be an absolute path: %q", name, path)
		}
	}
	// The readiness probe matches the marker's first line whole, so an install
	// ID that spans lines would write a marker that can never satisfy it, and
	// one padded with whitespace is a line no operator can read back reliably.
	if opts.InstallID == "" {
		return errors.New("INSTALL_ID cannot be empty")
	}
	if strings.ContainsAny(opts.InstallID, "\r\n") {
		return fmt.Errorf("INSTALL_ID must be a single line: %q", opts.InstallID)
	}
	if strings.TrimSpace(opts.InstallID) != opts.InstallID {
		return fmt.Errorf("INSTALL_ID must not start or end with whitespace: %q", opts.InstallID)
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
		info, err := os.Stat(target)
		if err != nil {
			return false, fmt.Errorf("stat installed binary: %w", err)
		}
		if info.Mode().Perm() != 0755 {
			if err := os.Chmod(target, 0755); err != nil {
				return false, fmt.Errorf("chmod installed binary: %w", err)
			}
			fmt.Printf("[INFO] Updated binary mode: %s\n", target)
			return true, nil
		}
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
	case "rke2":
		return configureRKE2(opts)
	case "aks":
		return configureKubeletDefaults(opts)
	case "microk8s":
		return configureMicroK8sArgs(opts)
	case "kind":
		if !opts.ForceKubeletExecStart {
			return configureSystemdKubelet(opts)
		}
		return configureKindSystemdKubelet(opts)
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

func configureKindSystemdKubelet(opts options) (bool, error) {
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
ExecStart=
ExecStart=/usr/bin/kubelet $KUBELET_KUBECONFIG_ARGS $KUBELET_CONFIG_ARGS $KUBELET_KUBEADM_ARGS --image-credential-provider-bin-dir=%s --image-credential-provider-config=%s
`, opts.BinDir, opts.ConfigPath, opts.BinDir, opts.ConfigPath)

	hostDropInPath := hostPath(opts, dropInPath)
	if err := os.MkdirAll(filepath.Dir(hostDropInPath), 0755); err != nil {
		return false, fmt.Errorf("create kubelet systemd drop-in directory: %w", err)
	}
	changed, err := writeFileIfChanged(hostDropInPath, []byte(content), 0644, true)
	if err != nil {
		return false, fmt.Errorf("write kind kubelet systemd drop-in: %w", err)
	}
	if changed {
		fmt.Printf("[INFO] Wrote kind kubelet systemd drop-in: %s\n", hostDropInPath)
	} else {
		fmt.Printf("[INFO] Kind kubelet systemd drop-in already up to date: %s\n", hostDropInPath)
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

func configureRKE2(opts options) (bool, error) {
	dropInPath := opts.RKE2ConfigDropInPath
	if dropInPath == "" {
		dropInPath = "/etc/rancher/rke2/config.yaml.d/99-credential-provider-harbor.yaml"
	}

	// RKE2 has no flags of its own for these, unlike k3s. They reach the
	// embedded kubelet through kubelet-arg, without the leading dashes.
	content := fmt.Sprintf(`kubelet-arg:
  - "image-credential-provider-bin-dir=%s"
  - "image-credential-provider-config=%s"
`, opts.BinDir, opts.ConfigPath)

	hostDropInPath := hostPath(opts, dropInPath)
	if err := os.MkdirAll(filepath.Dir(hostDropInPath), 0755); err != nil {
		return false, fmt.Errorf("create rke2 config drop-in directory: %w", err)
	}
	changed, err := writeFileIfChanged(hostDropInPath, []byte(content), 0644, true)
	if err != nil {
		return false, fmt.Errorf("write rke2 config drop-in: %w", err)
	}
	if changed {
		fmt.Printf("[INFO] Wrote rke2 config drop-in: %s\n", hostDropInPath)
	} else {
		fmt.Printf("[INFO] rke2 config drop-in already up to date: %s\n", hostDropInPath)
	}
	return changed, nil
}

// configureKubeletDefaults patches the kubelet EnvironmentFile that AKS nodes
// build their command line from. The AKS kubelet unit expands $KUBELET_FLAGS
// and never $KUBELET_EXTRA_ARGS, so the drop-in the generic profile writes is
// valid, is read, and does nothing.
func configureKubeletDefaults(opts options) (bool, error) {
	path := opts.KubeletDefaultsPath
	if path == "" {
		path = "/etc/default/kubelet"
	}
	hostDefaultsPath := hostPath(opts, path)

	existing, err := os.ReadFile(hostDefaultsPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("kubelet defaults file %s does not exist; this node does not build its kubelet command line from KUBELET_FLAGS. Set kubelet.configure=false and wire the flags yourself", hostDefaultsPath)
	}
	if err != nil {
		return false, fmt.Errorf("read kubelet defaults: %w", err)
	}

	updated, err := setKubeletFlags(string(existing), opts.BinDir, opts.ConfigPath)
	if err != nil {
		return false, fmt.Errorf("%s: %w", hostDefaultsPath, err)
	}

	changed, err := writeFileIfChanged(hostDefaultsPath, []byte(updated), 0644, true)
	if err != nil {
		return false, fmt.Errorf("write kubelet defaults: %w", err)
	}
	if changed {
		fmt.Printf("[INFO] Patched KUBELET_FLAGS in %s\n", hostDefaultsPath)
	} else {
		fmt.Printf("[INFO] KUBELET_FLAGS already up to date: %s\n", hostDefaultsPath)
	}
	return changed, nil
}

// setKubeletFlags rewrites the KUBELET_FLAGS assignment so that it carries
// exactly these two credential provider flags. Earlier values for them are
// replaced rather than appended to, so changing binDir and reinstalling
// converges instead of leaving a duplicate flag behind.
//
// A commented-out assignment is not an assignment. Where there is more than
// one active assignment the last one is patched, because that is the one
// systemd's EnvironmentFile parser leaves in the environment.
func setKubeletFlags(content, binDir, configPath string) (string, error) {
	lines := strings.Split(content, "\n")
	target := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "KUBELET_FLAGS=") {
			target = i
		}
	}
	if target < 0 {
		return "", errors.New("no active KUBELET_FLAGS assignment")
	}

	line := strings.TrimRight(lines[target], " \t\r")
	key, value, _ := strings.Cut(line, "=")
	value = unquote(strings.TrimSpace(value))

	fields := strings.Fields(value)
	fields = stripFlag(fields, binDirFlag)
	fields = stripFlag(fields, configFlag)
	fields = append(fields, binDirFlag+"="+binDir, configFlag+"="+configPath)

	// Always quoted on the way out. An unquoted value with spaces is fine for
	// systemd, and breaks anything that sources the file as a shell script.
	lines[target] = fmt.Sprintf("%s=%q", key, strings.Join(fields, " "))
	return strings.Join(lines, "\n"), nil
}

// stripFlag removes a flag from an argument list in both the --flag=value and
// the --flag value spelling.
func stripFlag(fields []string, flag string) []string {
	kept := make([]string, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		switch {
		case fields[i] == flag:
			i++ // and its value, which is the next field
		case strings.HasPrefix(fields[i], flag+"="):
		default:
			kept = append(kept, fields[i])
		}
	}
	return kept
}

// configureMicroK8sArgs edits the snap's kubelet arguments file. MicroK8s runs
// kubelet inside kubelite, which reads its arguments from this file at startup
// rather than from a command line or a systemd drop-in.
func configureMicroK8sArgs(opts options) (bool, error) {
	path := opts.MicroK8sArgsPath
	if path == "" {
		path = "/var/snap/microk8s/current/args/kubelet"
	}
	hostArgsPath := hostPath(opts, path)

	existing, err := os.ReadFile(hostArgsPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("MicroK8s kubelet arguments file %s does not exist; is MicroK8s installed on this node?", hostArgsPath)
	}
	if err != nil {
		return false, fmt.Errorf("read MicroK8s kubelet arguments: %w", err)
	}

	updated := setMicroK8sKubeletArgs(string(existing), opts.BinDir, opts.ConfigPath)
	changed, err := writeFileIfChanged(hostArgsPath, []byte(updated), 0644, true)
	if err != nil {
		return false, fmt.Errorf("write MicroK8s kubelet arguments: %w", err)
	}
	if changed {
		fmt.Printf("[INFO] Wrote MicroK8s kubelet arguments: %s\n", hostArgsPath)
	} else {
		fmt.Printf("[INFO] MicroK8s kubelet arguments already up to date: %s\n", hostArgsPath)
	}
	return changed, nil
}

// setMicroK8sKubeletArgs drops any existing lines for the two credential
// provider flags and appends the current ones. The file is one argument per
// line; every other line keeps its place.
func setMicroK8sKubeletArgs(content, binDir, configPath string) string {
	kept := make([]string, 0)
	for _, line := range strings.Split(content, "\n") {
		field, _, _ := strings.Cut(strings.TrimSpace(line), " ")
		field, _, _ = strings.Cut(field, "=")
		if field == binDirFlag || field == configFlag {
			continue
		}
		kept = append(kept, line)
	}

	// Trailing blank lines would push the appended arguments away from the
	// rest, and a file that does not end in a newline would join its last
	// argument to the first appended one.
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}
	kept = append(kept, binDirFlag+"="+binDir, configFlag+"="+configPath, "")
	return strings.Join(kept, "\n")
}

// kubeletRestartNeeded reports whether kubelet still has to pick up this
// install. Host changes are the obvious case. The other one is a node whose
// last completed install ID differs from this one: every host file can already
// match while the running kubelet has never been started against them. That is
// what makes a staged rollout work — install everywhere with
// kubelet.restart=false, then turn it on and let maxUnavailable=1 take the
// kubelets down one at a time.
//
// The third case is that same staged rollout run without the chart, where the
// install ID does not change between the two runs because nothing derives it
// from the values. The marker records whether kubelet was restarted, so
// turning restarts on finishes the job even when the ID and every host file
// stayed as they were.
func kubeletRestartNeeded(hostChanged bool, previous markerState, opts options) bool {
	if hostChanged || previous.InstallID != opts.InstallID {
		return true
	}
	return opts.RestartKubelet && !previous.KubeletRestarted
}

func restartKubelet(opts options) error {
	if !opts.RestartKubelet {
		fmt.Println("[WARN] Kubelet restart disabled. Restart or roll nodes before testing image pulls.")
		return nil
	}

	service := opts.KubeletService
	switch opts.Profile {
	case "k3s", "k3d":
		service = detectK3sService(opts, service)
	case "rke2":
		service = detectRKE2Service(opts, service)
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

// detectRKE2Service picks the unit that is actually installed. A node runs
// either the server or the agent, and only that unit's file is present.
func detectRKE2Service(opts options, fallback string) string {
	if fallback != "" && fallback != "rke2-agent" {
		return fallback
	}
	// Tarball installs land in /usr/local/lib/systemd/system, RPM installs in
	// /usr/lib/systemd/system, and either may be overridden in /etc.
	unitDirs := []string{
		"/etc/systemd/system",
		"/usr/local/lib/systemd/system",
		"/usr/lib/systemd/system",
		"/lib/systemd/system",
	}
	for _, service := range []string{"rke2-agent", "rke2-server"} {
		for _, dir := range unitDirs {
			if fileExists(hostPath(opts, filepath.Join(dir, service+".service"))) {
				return service
			}
		}
	}
	return "rke2-agent"
}

// markerState is what the host marker says about the install the node last
// completed. An empty InstallID means no install has completed here.
type markerState struct {
	InstallID string
	// KubeletRestarted is false when the run that wrote this marker was told
	// not to restart kubelet, which leaves the node holding the files without
	// kubelet running against them.
	KubeletRestarted bool
}

// readMarker returns what the host marker records, or the zero value when no
// node install has completed. A marker that predates the install ID, or that
// is otherwise unreadable, counts as no install: the run then redoes the work
// rather than trusting a file it cannot interpret.
func readMarker(opts options) (markerState, error) {
	data, err := os.ReadFile(hostPath(opts, opts.InstalledMarker))
	if errors.Is(err, os.ErrNotExist) {
		return markerState{}, nil
	}
	if err != nil {
		return markerState{}, fmt.Errorf("read marker file: %w", err)
	}
	// Read exactly what `grep -qxF install-id=<id>` would match: the whole
	// first line, byte for byte. Trimming here would make the installer and the
	// readiness probe disagree about an ID with surrounding whitespace — the
	// probe would pass on the marker while the installer read a different ID,
	// decided the node was new, and restarted kubelet on every container start.
	firstLine, rest, _ := strings.Cut(string(data), "\n")
	id, ok := strings.CutPrefix(firstLine, markerInstallIDPrefix)
	if !ok {
		return markerState{}, nil
	}
	// A marker written before this line existed says nothing about the
	// restart, and reading that as "not restarted" would restart kubelet once
	// on every node the first time this version runs. Absent means restarted.
	restarted := true
	for line := range strings.SplitSeq(rest, "\n") {
		if value, ok := strings.CutPrefix(line, markerKubeletRestartedPrefix); ok {
			restarted = value == "true"
			break
		}
	}
	return markerState{InstallID: id, KubeletRestarted: restarted}, nil
}

// removeMarker clears the host marker so that nothing can report this node as
// installed until this run has finished.
func removeMarker(opts options) error {
	marker := hostPath(opts, opts.InstalledMarker)
	if err := os.Remove(marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove marker file: %w", err)
	}
	return nil
}

// writeMarker records which install this node completed and when.
//
// Written to a temporary file in the marker's own directory and renamed over
// it, because the readiness probe reads the first line. A write straight into
// the live marker that fails partway through - a full filesystem is the usual
// way - can land that first line and nothing else, which is all the probe
// needs to report the node done while the installer returns an error.
func writeMarker(opts options, kubeletRestarted bool) error {
	marker := hostPath(opts, opts.InstalledMarker)
	dir := filepath.Dir(marker)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create marker directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(marker)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create marker file: %w", err)
	}
	// Named now so every failure below can remove it: a crash between here and
	// the rename would otherwise leave the directory collecting one orphan per
	// attempt.
	tmpName := tmp.Name()
	if _, err := tmp.Write(markerContent(opts, kubeletRestarted, time.Now().UTC())); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write marker file: %w", err)
	}
	// CreateTemp opens at 0600, and the marker is world-readable so that a
	// kubectl debug pod can check a node without being root.
	if err := tmp.Chmod(0644); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("set marker file mode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close marker file: %w", err)
	}
	if err := os.Rename(tmpName, marker); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("move marker file into place: %w", err)
	}
	return nil
}

func markerContent(opts options, kubeletRestarted bool, completedAt time.Time) []byte {
	return fmt.Appendf(nil, "%s%s\n%s%t\ncompleted-at=%s\n",
		markerInstallIDPrefix, opts.InstallID,
		markerKubeletRestartedPrefix, kubeletRestarted,
		completedAt.Format(time.RFC3339))
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
	defer func() { _ = in.Close() }()

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

// unquote strips one layer of matching surrounding quotes. AKS node images
// have written KUBELET_FLAGS both quoted and unquoted over the years.
func unquote(value string) string {
	if len(value) < 2 {
		return value
	}
	quote := value[0]
	if (quote == '"' || quote == '\'') && value[len(value)-1] == quote {
		return value[1 : len(value)-1]
	}
	return value
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
