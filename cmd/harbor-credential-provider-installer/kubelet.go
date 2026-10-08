package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

const (
	binDirFlag = "--image-credential-provider-bin-dir"
	configFlag = "--image-credential-provider-config"

	// The variable an AKS kubelet unit expands, and the one every other
	// systemd kubelet unit expands.
	kubeletFlagsVar     = "KUBELET_FLAGS"
	kubeletExtraArgsVar = "KUBELET_EXTRA_ARGS"
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
	case "sks":
		return configureSKS(opts)
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
	return filepath.Join("/etc/systemd/system", service+".service.d", "99-harbor-credential-provider.conf")
}

func configureSystemdKubelet(opts options) (bool, error) {
	dropIn, err := writeHostFile(opts, hostFile{
		path: systemdDropInPath(opts),
		content: fmt.Sprintf(`[Service]
Environment="%s=%s=%s %s=%s"
`, kubeletExtraArgsVar, binDirFlag, opts.BinDir, configFlag, opts.ConfigPath),
		label: "kubelet systemd drop-in",
		title: "Kubelet systemd drop-in",
	})
	if err != nil {
		return false, err
	}

	envFile, err := syncKubeletEnvironmentFile(opts)
	if err != nil {
		return false, err
	}
	return dropIn || envFile, nil
}

// syncKubeletEnvironmentFile writes the same two flags into the environment
// file the kubelet unit reads, when that file already assigns the variable the
// drop-in sets.
//
// systemd resolves EnvironmentFile= after Environment=, whatever order the
// drop-ins are merged in, so a file that assigns KUBELET_EXTRA_ARGS wins over
// the drop-in even when the drop-in sorts last. The kubeadm packages ship
// /etc/default/kubelet with an empty assignment, which is enough to erase the
// flags: the drop-in lands, systemd merges it, and the kubelet still starts
// without them.
//
// Nothing here is an error. A node whose unit reads no such file, or whose
// file leaves the variable alone, is a node where the drop-in is what works.
func syncKubeletEnvironmentFile(opts options) (bool, error) {
	path := opts.KubeletDefaultsPath
	if path == "" {
		path = "/etc/default/kubelet"
	}
	hostDefaultsPath := hostPath(opts, path)

	existing, err := os.ReadFile(hostDefaultsPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", hostDefaultsPath, err)
	}

	updated, err := setKubeletFlags(string(existing), kubeletExtraArgsVar, opts.BinDir, opts.ConfigPath)
	if err != nil {
		return false, nil
	}

	changed, err := writeFileIfChanged(hostDefaultsPath, []byte(updated), 0644, true)
	if err != nil {
		return false, fmt.Errorf("write %s: %w", hostDefaultsPath, err)
	}
	if changed {
		fmt.Printf("[INFO] Patched %s in %s, which overrides the drop-in\n", kubeletExtraArgsVar, hostDefaultsPath)
	} else {
		fmt.Printf("[INFO] %s already up to date: %s\n", kubeletExtraArgsVar, hostDefaultsPath)
	}
	return changed, nil
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
		dropInPath = "/etc/rancher/k3s/config.yaml.d/99-harbor-credential-provider.yaml"
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
		dropInPath = "/etc/rancher/rke2/config.yaml.d/99-harbor-credential-provider.yaml"
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

	updated, err := setKubeletFlags(string(existing), kubeletFlagsVar, opts.BinDir, opts.ConfigPath)
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
func setKubeletFlags(content, key, binDir, configPath string) (string, error) {
	lines := strings.Split(content, "\n")
	target := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), key+"=") {
			target = i
		}
	}
	if target < 0 {
		return "", fmt.Errorf("no active %s assignment", key)
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

const (
	// sksKubeletDropInPath is the drop-in Exoscale SKS nodes start kubelet
	// from. The installer reads it and never writes it.
	sksKubeletDropInPath = "/etc/systemd/system/kubelet.service.d/sks.conf"
	sksHarborDropInPath  = "/etc/systemd/system/kubelet.service.d/zz-harbor-credential-provider.conf"
)

// configureSKS writes a drop-in that replaces the kubelet ExecStart of an
// Exoscale SKS node with the same command line plus the two credential
// provider flags.
//
// SKS spells every kubelet flag out in the ExecStart of its own drop-in and
// expands no variable there, so neither KUBELET_EXTRA_ARGS nor an
// EnvironmentFile reaches kubelet, and KubeletConfiguration has no field for
// these two settings. A later drop-in that resets ExecStart is the one place
// left. The command line is copied from sks.conf on every run, so a node whose
// SKS drop-in changes picks that up the next time the installer runs instead
// of staying on a stale copy. Once sks.conf itself passes both flags, the
// drop-in is not needed: none is written, and one left by an earlier run is
// removed.
func configureSKS(opts options) (bool, error) {
	sourcePath := opts.SKSDropInPath
	if sourcePath == "" {
		sourcePath = sksKubeletDropInPath
	}
	dropInPath := systemdDropInPath(opts)

	// systemd applies a unit's drop-ins in file name order, whatever directory
	// each lives in. One that sorts before sks.conf has its ExecStart reset by
	// sks.conf, and kubelet starts without the flags.
	if filepath.Base(dropInPath) <= filepath.Base(sourcePath) {
		return false, fmt.Errorf("kubelet drop-in %s sorts before %s, which would reset its ExecStart; "+
			"pick a kubelet.systemdDropInPath (SYSTEMD_DROP_IN_PATH) whose file name sorts after %q",
			dropInPath, sourcePath, filepath.Base(sourcePath))
	}

	hostSourcePath := hostPath(opts, sourcePath)
	existing, err := os.ReadFile(hostSourcePath)
	if errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("SKS kubelet drop-in %s does not exist; this node does not start kubelet the way SKS nodes do. "+
			"Use the profile that matches this node, or set kubelet.sksDropInPath (SKS_DROP_IN_PATH) to the drop-in that holds its kubelet ExecStart", hostSourcePath)
	}
	if err != nil {
		return false, fmt.Errorf("read SKS kubelet drop-in: %w", err)
	}

	args, err := lastExecStart(string(existing))
	if err != nil {
		return false, fmt.Errorf("%s: %w", hostSourcePath, err)
	}

	if hasCredentialProviderFlags(args, opts.BinDir, opts.ConfigPath) {
		fmt.Printf("[INFO] %s already starts kubelet with %s=%s and %s=%s\n", hostSourcePath, binDirFlag, opts.BinDir, configFlag, opts.ConfigPath)
		// A drop-in from an earlier run sorts after sks.conf and would keep
		// kubelet on the command line copied back then.
		hostDropInPath := hostPath(opts, dropInPath)
		err := os.Remove(hostDropInPath)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("remove SKS kubelet ExecStart drop-in: %w", err)
		}
		fmt.Printf("[INFO] Removed SKS kubelet ExecStart drop-in, no longer needed: %s\n", hostDropInPath)
		return true, nil
	}

	args = append(withoutCredentialProviderFlags(args),
		binDirFlag+"="+opts.BinDir,
		configFlag+"="+opts.ConfigPath)

	// The copied arguments go back byte for byte, so whatever quoting, $ or %
	// SKS wrote means the same thing here as it did in sks.conf. Only the two
	// appended flags are new, and validateOptions keeps those free of anything
	// systemd would read as syntax.
	return writeHostFile(opts, hostFile{
		path: dropInPath,
		content: "# Written by harbor-credential-provider-installer: the kubelet command line\n" +
			"# from the SKS drop-in, plus the image credential provider flags.\n" +
			"[Service]\n" +
			"ExecStart=\n" +
			"ExecStart=" + strings.Join(args, " \\\n\t") + "\n",
		label: "SKS kubelet ExecStart drop-in",
		title: "SKS kubelet ExecStart drop-in",
	})
}

// lastExecStart returns the arguments of the ExecStart in a unit file's
// [Service] section that systemd would run, unquoted words left as written.
// An empty ExecStart= clears the ones before it, as it does in systemd, so a
// file that ends on a reset, or has no ExecStart at all, is an error rather
// than an empty command line.
func lastExecStart(content string) ([]string, error) {
	var args []string
	section := ""
	for _, line := range unitLogicalLines(content) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = line
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || section != "[Service]" || strings.TrimSpace(key) != "ExecStart" {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			args = nil
			continue
		}
		parsed, err := splitExecArgs(value)
		if err != nil {
			return nil, fmt.Errorf("parse ExecStart: %w", err)
		}
		args = parsed
	}
	if len(args) == 0 {
		return nil, errors.New("no ExecStart in [Service]")
	}
	return args, nil
}

// unitLogicalLines joins a unit file's continuation lines the way systemd
// reads them: a line ending in an unescaped backslash continues on the next
// one, the backslash becoming a space. Comment lines are dropped, including
// those inside a continuation, which systemd skips there too.
func unitLogicalLines(content string) []string {
	var lines []string
	pending := ""
	continuing := false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		trailing := len(line) - len(strings.TrimRight(line, `\`))
		if trailing%2 == 1 {
			pending += line[:len(line)-1] + " "
			continuing = true
			continue
		}
		lines = append(lines, pending+line)
		pending = ""
		continuing = false
	}
	if continuing {
		lines = append(lines, pending)
	}
	return lines
}

// splitExecArgs splits an ExecStart value into its words at whitespace outside
// quotes, keeping each word exactly as written. An unterminated quote, a
// dangling backslash or a ";" separating a second command is refused: each
// means the line is something this installer would rewrite wrongly.
func splitExecArgs(value string) ([]string, error) {
	var args []string
	var word strings.Builder
	inWord := false
	var quote byte
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c == '\\':
			if i+1 >= len(value) {
				return nil, errors.New("dangling backslash")
			}
			word.WriteByte(c)
			word.WriteByte(value[i+1])
			i++
			inWord = true
		case quote != 0:
			word.WriteByte(c)
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			word.WriteByte(c)
			quote = c
			inWord = true
		case isArgSpace(c):
			if inWord {
				args = append(args, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteByte(c)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote", quote)
	}
	if inWord {
		args = append(args, word.String())
	}
	for _, arg := range args {
		if arg == ";" {
			return nil, errors.New(`more than one command line, separated by ";"`)
		}
	}
	return args, nil
}

// hasCredentialProviderFlags reports whether the arguments pass each of the
// two credential provider flags exactly once, with these values.
func hasCredentialProviderFlags(args []string, binDir, configPath string) bool {
	values := map[string][]string{}
	for i := 0; i < len(args); i++ {
		name, value, inline := strings.Cut(unquoteWord(args[i]), "=")
		if name != binDirFlag && name != configFlag {
			continue
		}
		if !inline && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++
			value = args[i]
		}
		values[name] = append(values[name], unquoteWord(value))
	}
	return slices.Equal(values[binDirFlag], []string{binDir}) &&
		slices.Equal(values[configFlag], []string{configPath})
}

// withoutCredentialProviderFlags drops both credential provider flags, along
// with every following word that can only be part of their value, as
// stripCredentialProviderFlags does for an argument string: kubelet takes no
// positional arguments, so a leftover word would become the value of the flag
// in front of it.
func withoutCredentialProviderFlags(args []string) []string {
	kept := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		name, _, _ := strings.Cut(unquoteWord(args[i]), "=")
		if name != binDirFlag && name != configFlag {
			kept = append(kept, args[i])
			continue
		}
		for i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++
		}
	}
	return kept
}

// unquoteWord strips one pair of matching quotes around a whole word, or
// around the value of a --flag="value" word.
func unquoteWord(word string) string {
	unquote := func(s string) string {
		if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
			return s[1 : len(s)-1]
		}
		return s
	}
	word = unquote(word)
	if name, value, ok := strings.Cut(word, "="); ok {
		return name + "=" + unquote(value)
	}
	return word
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
