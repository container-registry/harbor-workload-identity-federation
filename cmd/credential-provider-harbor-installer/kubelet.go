package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	binDirFlag = "--image-credential-provider-bin-dir"
	configFlag = "--image-credential-provider-config"
)

// configureKubelet points the node's kubelet at the installed binary and
// config. Where those arguments live is the one thing every distribution does
// differently, so each profile has its own writer below.
func configureKubelet(opts options) (bool, error) {
	if !opts.ConfigureKubelet {
		fmt.Printf("[WARN] Kubelet configuration disabled. Ensure kubelet uses %s=%s and %s=%s\n", binDirFlag, opts.BinDir, configFlag, opts.ConfigPath)
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

// hostFile is a file a profile owns on the node: where it goes, what goes in
// it, and how it is named in the messages about it. label names the file in
// the middle of a sentence ("write %s"), title at the start of one ("%s
// already up to date").
type hostFile struct {
	path    string
	content string
	label   string
	title   string
}

// writeHostFile creates the parent directory and writes the file under the
// host root, reporting whether its contents changed. Every profile that owns a
// whole host file goes through here, so directory creation, change detection
// and the logging around them cannot drift apart between profiles.
func writeHostFile(opts options, file hostFile) (bool, error) {
	hostFilePath := hostPath(opts, file.path)
	if err := os.MkdirAll(filepath.Dir(hostFilePath), 0755); err != nil {
		return false, fmt.Errorf("create %s directory: %w", file.label, err)
	}
	changed, err := writeFileIfChanged(hostFilePath, []byte(file.content), 0644, true)
	if err != nil {
		return false, fmt.Errorf("write %s: %w", file.label, err)
	}
	if changed {
		fmt.Printf("[INFO] Wrote %s: %s\n", file.label, hostFilePath)
	} else {
		fmt.Printf("[INFO] %s already up to date: %s\n", file.title, hostFilePath)
	}
	return changed, nil
}

// systemdDropInPath is where the kubelet unit's drop-in goes, honouring the
// override and otherwise following the configured service name.
func systemdDropInPath(opts options) string {
	if opts.SystemdDropInPath != "" {
		return opts.SystemdDropInPath
	}
	service := opts.KubeletService
	if service == "" {
		service = "kubelet"
	}
	return filepath.Join("/etc/systemd/system", service+".service.d", "99-credential-provider-harbor.conf")
}

func configureSystemdKubelet(opts options) (bool, error) {
	return writeHostFile(opts, hostFile{
		path: systemdDropInPath(opts),
		content: fmt.Sprintf(`[Service]
Environment="KUBELET_EXTRA_ARGS=%s=%s %s=%s"
`, binDirFlag, opts.BinDir, configFlag, opts.ConfigPath),
		label: "kubelet systemd drop-in",
		title: "Kubelet systemd drop-in",
	})
}

func configureKindSystemdKubelet(opts options) (bool, error) {
	return writeHostFile(opts, hostFile{
		path: systemdDropInPath(opts),
		content: fmt.Sprintf(`[Service]
Environment="KUBELET_EXTRA_ARGS=%[1]s=%[2]s %[3]s=%[4]s"
ExecStart=
ExecStart=/usr/bin/kubelet $KUBELET_KUBECONFIG_ARGS $KUBELET_CONFIG_ARGS $KUBELET_KUBEADM_ARGS %[1]s=%[2]s %[3]s=%[4]s
`, binDirFlag, opts.BinDir, configFlag, opts.ConfigPath),
		label: "kind kubelet systemd drop-in",
		title: "Kind kubelet systemd drop-in",
	})
}

func configureK3s(opts options) (bool, error) {
	dropInPath := opts.K3sConfigDropInPath
	if dropInPath == "" {
		dropInPath = "/etc/rancher/k3s/config.yaml.d/99-credential-provider-harbor.yaml"
	}

	return writeHostFile(opts, hostFile{
		path: dropInPath,
		content: fmt.Sprintf(`image-credential-provider-bin-dir: %q
image-credential-provider-config: %q
`, opts.BinDir, opts.ConfigPath),
		label: "k3s config drop-in",
		title: "k3s config drop-in",
	})
}

func configureRKE2(opts options) (bool, error) {
	dropInPath := opts.RKE2ConfigDropInPath
	if dropInPath == "" {
		dropInPath = "/etc/rancher/rke2/config.yaml.d/99-credential-provider-harbor.yaml"
	}

	// RKE2 has no flags of its own for these, unlike k3s. They reach the
	// embedded kubelet through kubelet-arg, without the leading dashes.
	return writeHostFile(opts, hostFile{
		path: dropInPath,
		content: fmt.Sprintf(`kubelet-arg:
  - "image-credential-provider-bin-dir=%s"
  - "image-credential-provider-config=%s"
`, opts.BinDir, opts.ConfigPath),
		label: "rke2 config drop-in",
		title: "rke2 config drop-in",
	})
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

// setKubeletFlags patches the KUBELET_FLAGS assignment so that it carries
// these two credential provider flags and no earlier copy of them. Everything
// else in the value survives byte for byte, quoting and whitespace included:
// AKS writes this file itself, and re-serializing a value whose quoting we
// guessed wrong would change the kubelet command line in ways nobody asked for.
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
	key, raw, _ := strings.Cut(line, "=")
	raw = strings.TrimSpace(raw)

	// Keep the quote character the node image chose. An unquoted value has to
	// gain quotes, because what we append contains spaces, and an unquoted
	// value with spaces breaks anything that sources the file as a shell.
	quote := `"`
	value := raw
	if len(raw) >= 2 && (raw[0] == '"' || raw[0] == '\'') && raw[len(raw)-1] == raw[0] {
		quote = string(raw[0])
		value = raw[1 : len(raw)-1]
	}

	value = stripCredentialProviderFlags(value)
	if value != "" {
		value += " "
	}
	value += binDirFlag + "=" + binDir + " " + configFlag + "=" + configPath

	lines[target] = key + "=" + quote + value + quote
	return strings.Join(lines, "\n"), nil
}

// stripCredentialProviderFlags removes the two credential provider flags from
// an argument string, in both the --flag=value and the --flag value spelling,
// and leaves every other byte where it was. Whitespace runs between the
// arguments that remain are not collapsed, and nothing is re-quoted.
func stripCredentialProviderFlags(value string) string {
	var kept strings.Builder
	i := 0
	for i < len(value) {
		sepStart := i
		for i < len(value) && isArgSpace(value[i]) {
			i++
		}
		separator := value[sepStart:i]

		argStart := i
		for i < len(value) && !isArgSpace(value[i]) {
			i++
		}
		arg := value[argStart:i]
		if arg == "" {
			break
		}

		name, _, _ := strings.Cut(arg, "=")
		if name != binDirFlag && name != configFlag {
			kept.WriteString(separator)
			kept.WriteString(arg)
			continue
		}

		// The separator in front of a dropped flag goes with it, and so does
		// everything after it that can only be part of its value: the value
		// itself in the --flag value spelling, and every further token of a
		// value that was written with an unquoted space in it. Whatever is
		// left of such a value would otherwise survive as a stray argument,
		// which kubelet reads as the value of the flag in front of it. The
		// run stops at the first argument that starts with a dash, so a flag
		// left without a value still does not eat the flag after it.
		for {
			next, end := peekArg(value, i)
			if next == "" || strings.HasPrefix(next, "-") {
				break
			}
			i = end
		}
	}
	return strings.TrimSpace(kept.String())
}

// peekArg returns the next whitespace-separated argument at or after i, and
// the offset just past it.
func peekArg(value string, i int) (string, int) {
	for i < len(value) && isArgSpace(value[i]) {
		i++
	}
	start := i
	for i < len(value) && !isArgSpace(value[i]) {
		i++
	}
	return value[start:i], i
}

func isArgSpace(c byte) bool {
	return c == ' ' || c == '\t'
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
//
// A flag and its value may be split across two lines here, since kubelite
// passes each line on as its own argument. Dropping only the flag line would
// leave the old path behind as a stray positional argument, which kubelet then
// reads as the value of whatever flag precedes it.
func setMicroK8sKubeletArgs(content, binDir, configPath string) string {
	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		// Fields, not a split on " ": a tab between the flag and its value is
		// as good a separator as a space to kubelite, and matching only the
		// space spelling would leave the old flag in place and append a
		// second copy of it.
		fields := strings.Fields(lines[i])
		field := ""
		hasValue := false
		if len(fields) > 0 {
			field = fields[0]
			hasValue = len(fields) > 1
		}
		field, _, hasInlineValue := strings.Cut(field, "=")
		if field != binDirFlag && field != configFlag {
			kept = append(kept, lines[i])
			continue
		}
		if !hasValue && !hasInlineValue && i+1 < len(lines) {
			if next := strings.TrimSpace(lines[i+1]); next != "" && !strings.HasPrefix(next, "-") {
				i++
			}
		}
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

func restartKubelet(opts options) error {
	if !opts.RestartKubelet {
		fmt.Println("[WARN] Kubelet restart disabled. Restart or roll nodes before testing image pulls.")
		return nil
	}

	services, err := kubeletServices(opts, systemdRunsUnit)
	if err != nil {
		return err
	}

	fmt.Printf("[INFO] Restarting %s\n", strings.Join(services, " "))
	if opts.ConfigureKubelet && opts.Profile != "eks" && opts.Profile != "aws" {
		if err := systemctl("daemon-reload"); err != nil {
			return err
		}
	}
	return systemctl(append([]string{"restart"}, services...)...)
}

// kubeletServices lists the units that have to come back before the node runs
// against the flags this install wrote.
func kubeletServices(opts options, runsUnit unitPredicate) ([]string, error) {
	switch opts.Profile {
	case "k3s", "k3d":
		return []string{detectK3sService(opts, opts.KubeletService)}, nil
	case "rke2":
		return detectRKE2Services(opts, opts.KubeletService, runsUnit)
	}
	if opts.KubeletService == "" {
		return []string{"kubelet"}, nil
	}
	return []string{opts.KubeletService}, nil
}

func systemctl(args ...string) error {
	return runSystemctl(os.Stdout, os.Stderr, args...)
}

// systemctlQuiet drops systemctl's output: "not enabled" is an answer here,
// and logging it would read as a failure.
func systemctlQuiet(args ...string) error {
	return runSystemctl(io.Discard, io.Discard, args...)
}

func runSystemctl(stdout, stderr io.Writer, args ...string) error {
	cmdArgs := append([]string{"-t", "1", "-m", "-u", "-i", "-n", "-p", "--", "systemctl"}, args...)
	cmd := exec.Command("nsenter", cmdArgs...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err == nil {
		return nil
	} else if !errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("nsenter systemctl %s: %w", strings.Join(args, " "), err)
	}

	cmd = exec.Command("systemctl", args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
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

// unitPredicate answers whether this node uses a unit.
type unitPredicate func(unit string) bool

// systemdRunsUnit reports whether a unit is active now or enabled for the
// next boot. Either way the node uses it.
func systemdRunsUnit(unit string) bool {
	for _, question := range []string{"is-active", "is-enabled"} {
		if err := systemctlQuiet(question, "--quiet", unit); err == nil {
			return true
		}
	}
	return false
}

// detectRKE2Services lists the rke2 units this node actually runs. install.sh
// puts both unit files on every node, so only systemd knows the role.
func detectRKE2Services(opts options, fallback string, runsUnit unitPredicate) ([]string, error) {
	// The profile sets no default, so anything here was asked for by name.
	if fallback != "" {
		return []string{fallback}, nil
	}

	var installed, running []string
	for _, service := range []string{"rke2-server", "rke2-agent"} {
		if !rke2UnitInstalled(opts, service) {
			continue
		}
		installed = append(installed, service)
		if runsUnit(service + ".service") {
			running = append(running, service)
		}
	}

	if len(running) > 0 {
		return running, nil
	}

	// The files are already written, so the error has to say what to do next
	// rather than fail on a guessed unit.
	if len(installed) == 0 {
		return nil, errors.New("no rke2-server.service or rke2-agent.service on this node: " +
			"profile=rke2 expects one of them. Set kubelet.serviceName (KUBELET_SERVICE) to the unit " +
			"that runs kubelet here, or use the profile that matches this node")
	}
	return nil, fmt.Errorf("systemd reports neither %s running nor enabled on this node, "+
		"although the unit files are installed. Restarting all of them would start the role this "+
		"node does not run. Start the one this node uses, or set kubelet.serviceName "+
		"(KUBELET_SERVICE) to it", strings.Join(installed, " or "))
}

// rke2UnitInstalled reports whether a unit file is on the node. Tarball
// installs use /usr/local/lib, RPM /usr/lib, and /etc overrides both.
func rke2UnitInstalled(opts options, service string) bool {
	unitDirs := []string{
		"/etc/systemd/system",
		"/usr/local/lib/systemd/system",
		"/usr/lib/systemd/system",
		"/lib/systemd/system",
	}
	for _, dir := range unitDirs {
		if fileExists(hostPath(opts, filepath.Join(dir, service+".service"))) {
			return true
		}
	}
	return false
}
