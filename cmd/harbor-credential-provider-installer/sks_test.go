package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// sksConf is the kubelet drop-in of an SKS node running Kubernetes 1.37,
// byte for byte, continuation tabs included.
const sksConf = `[Service]
ExecStart=
ExecStart=/usr/local/bin/kubelet \
	--config=/var/lib/kubelet/config.yaml \
	--cloud-provider="external" \
	--bootstrap-kubeconfig="/etc/kubernetes/kubelet/bootstrap-kubeconfig" \
	--kubeconfig="/etc/kubernetes/kubelet/kubeconfig" \
	--node-labels="node.exoscale.net/nodepool-id=8d1e3f41-9660-4b08-ba6a-c28fb48ba94c" \
	-v=1
`

const (
	sksBinDir     = "/usr/local/bin/credential-providers"
	sksConfigPath = "/etc/kubernetes/credential-providers/config.yaml"
)

// sksWantDropIn is the whole drop-in the installer writes for sksConf.
const sksWantDropIn = `# Written by harbor-credential-provider-installer: the kubelet command line
# from the SKS drop-in, plus the image credential provider flags.
[Service]
ExecStart=
ExecStart=/usr/local/bin/kubelet \
	--config=/var/lib/kubelet/config.yaml \
	--cloud-provider="external" \
	--bootstrap-kubeconfig="/etc/kubernetes/kubelet/bootstrap-kubeconfig" \
	--kubeconfig="/etc/kubernetes/kubelet/kubeconfig" \
	--node-labels="node.exoscale.net/nodepool-id=8d1e3f41-9660-4b08-ba6a-c28fb48ba94c" \
	-v=1 \
	--image-credential-provider-bin-dir=/usr/local/bin/credential-providers \
	--image-credential-provider-config=/etc/kubernetes/credential-providers/config.yaml
`

// sksOptions is an sks run against a host root holding this sks.conf, or no
// sks.conf at all when content is empty.
func sksOptions(t *testing.T, content string) options {
	t.Helper()
	hostRoot := t.TempDir()
	if content != "" {
		path := filepath.Join(hostRoot, sksKubeletDropInPath)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("create sks.conf dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write sks.conf: %v", err)
		}
	}
	return options{
		Profile:           "sks",
		HostRoot:          hostRoot,
		BinDir:            sksBinDir,
		ConfigPath:        sksConfigPath,
		ConfigureKubelet:  true,
		KubeletService:    "kubelet",
		SystemdDropInPath: sksHarborDropInPath,
		SKSDropInPath:     sksKubeletDropInPath,
	}
}

func readHostFile(t *testing.T, opts options, path string) string {
	t.Helper()
	data, err := os.ReadFile(hostPath(opts, path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func requireNoHostFile(t *testing.T, opts options, path string) {
	t.Helper()
	if _, err := os.Stat(hostPath(opts, path)); !os.IsNotExist(err) {
		t.Fatalf("%s exists (stat error %v), want it never written", path, err)
	}
}

func TestConfigureSKSWritesTheSKSCommandLineWithTheFlags(t *testing.T) {
	opts := sksOptions(t, sksConf)

	requireIdempotentConfigureKubelet(t, opts)

	if got := readHostFile(t, opts, sksHarborDropInPath); got != sksWantDropIn {
		t.Fatalf("drop-in =\n%s\nwant\n%s", got, sksWantDropIn)
	}
	if got := readHostFile(t, opts, sksKubeletDropInPath); got != sksConf {
		t.Fatalf("sks.conf was modified:\n%s", got)
	}

	// What systemd will run: the drop-in parsed the way the installer parses
	// sks.conf has to be the SKS command line plus exactly the two flags.
	got, err := lastExecStart(sksWantDropIn)
	if err != nil {
		t.Fatalf("lastExecStart(drop-in) error: %v", err)
	}
	stock, err := lastExecStart(sksConf)
	if err != nil {
		t.Fatalf("lastExecStart(sks.conf) error: %v", err)
	}
	want := append(slices.Clone(stock), binDirFlag+"="+sksBinDir, configFlag+"="+sksConfigPath)
	if !slices.Equal(got, want) {
		t.Fatalf("drop-in ExecStart =\n%q\nwant\n%q", got, want)
	}
}

// The re-run a pod restart or a reboot causes has to change nothing on the
// node, and so restart nothing.
func TestInstallOnSKSDoesNotRestartKubeletOnARerun(t *testing.T) {
	opts := markerOptions(t, t.TempDir(), "rev-1")
	sks := sksOptions(t, sksConf)
	opts.Profile = "sks"
	opts.HostRoot = sks.HostRoot
	opts.ConfigureKubelet = true
	opts.RestartKubelet = true
	opts.KubeletService = "kubelet"
	opts.SystemdDropInPath = sksHarborDropInPath
	opts.SKSDropInPath = sksKubeletDropInPath

	restarts := 0
	restart := func(options) error {
		restarts++
		return nil
	}
	for run := 1; run <= 2; run++ {
		if err := install(opts, restart); err != nil {
			t.Fatalf("install() run %d error: %v", run, err)
		}
	}
	if restarts != 1 {
		t.Fatalf("kubelet restarted %d times over two identical runs, want 1", restarts)
	}
}

func TestConfigureSKSLeavesANodeThatAlreadyHasTheFlags(t *testing.T) {
	withFlags := strings.TrimSuffix(sksConf, "\n") + " \\\n" +
		"\t" + binDirFlag + "=" + sksBinDir + " \\\n" +
		"\t" + configFlag + `="` + sksConfigPath + `"` + "\n"
	opts := sksOptions(t, withFlags)

	changed, err := configureKubelet(opts)
	if err != nil {
		t.Fatalf("configureKubelet() error: %v", err)
	}
	if changed {
		t.Fatal("configureKubelet() changed = true, want false")
	}
	requireNoHostFile(t, opts, sksHarborDropInPath)
}

func TestConfigureSKSReplacesFlagsWithWrongValues(t *testing.T) {
	tests := map[string]string{
		"inline values":          " \\\n\t" + binDirFlag + "=/old/bin \\\n\t" + configFlag + "=/old/config.yaml",
		"space-separated values": " \\\n\t" + binDirFlag + " /old/bin \\\n\t" + configFlag + ` "/old/config.yaml"`,
		"one right, one wrong":   " \\\n\t" + binDirFlag + "=" + sksBinDir + " \\\n\t" + configFlag + "=/old/config.yaml",
		"only one of them":       " \\\n\t" + configFlag + "=" + sksConfigPath,
		"the right ones twice": " \\\n\t" + binDirFlag + "=" + sksBinDir + " " + configFlag + "=" + sksConfigPath +
			" " + binDirFlag + "=" + sksBinDir + " " + configFlag + "=" + sksConfigPath,
	}
	for name, extra := range tests {
		t.Run(name, func(t *testing.T) {
			opts := sksOptions(t, strings.TrimSuffix(sksConf, "\n")+extra+"\n")

			requireIdempotentConfigureKubelet(t, opts)

			if got := readHostFile(t, opts, sksHarborDropInPath); got != sksWantDropIn {
				t.Fatalf("drop-in =\n%s\nwant\n%s", got, sksWantDropIn)
			}
		})
	}
}

func TestConfigureSKSFailsClosed(t *testing.T) {
	tests := map[string]string{
		"no sks.conf":                        "",
		"no ExecStart":                       "[Service]\nType=notify\n",
		"only a reset":                       "[Service]\nExecStart=\n",
		"ExecStart outside [Service]":        "[Unit]\nExecStart=/usr/local/bin/kubelet\n",
		"an unterminated quote":              "[Service]\nExecStart=\nExecStart=/usr/local/bin/kubelet --kubeconfig=\"/etc/kubernetes\n",
		"a second command after a semicolon": "[Service]\nExecStart=/usr/local/bin/kubelet ; /bin/true\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			opts := sksOptions(t, content)

			if _, err := configureKubelet(opts); err == nil {
				t.Fatal("configureKubelet() returned nil error, want a failure")
			}
			requireNoHostFile(t, opts, sksHarborDropInPath)
		})
	}
}

// A drop-in that sorts before sks.conf is applied first and then reset by it.
// The profile default does not, and an override that does is refused.
func TestConfigureSKSRefusesADropInThatSortsBeforeSKSConf(t *testing.T) {
	opts := sksOptions(t, sksConf)
	opts.SystemdDropInPath = "/etc/systemd/system/kubelet.service.d/99-harbor-credential-provider.conf"

	if _, err := configureKubelet(opts); err == nil {
		t.Fatal("configureKubelet() returned nil error, want a drop-in ordering error")
	}
	requireNoHostFile(t, opts, opts.SystemdDropInPath)
}

func TestConfigureSKSReadsAnOverriddenSKSDropIn(t *testing.T) {
	opts := sksOptions(t, "")
	opts.SKSDropInPath = "/usr/lib/systemd/system/kubelet.service.d/sks.conf"
	path := hostPath(opts, opts.SKSDropInPath)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(sksConf), 0644); err != nil {
		t.Fatalf("write sks.conf: %v", err)
	}

	requireIdempotentConfigureKubelet(t, opts)

	if got := readHostFile(t, opts, sksHarborDropInPath); got != sksWantDropIn {
		t.Fatalf("drop-in =\n%s\nwant\n%s", got, sksWantDropIn)
	}
}

func TestLastExecStartReadsTheUnitTheWaySystemdDoes(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "the SKS drop-in",
			content: sksConf,
			want: []string{
				"/usr/local/bin/kubelet",
				"--config=/var/lib/kubelet/config.yaml",
				`--cloud-provider="external"`,
				`--bootstrap-kubeconfig="/etc/kubernetes/kubelet/bootstrap-kubeconfig"`,
				`--kubeconfig="/etc/kubernetes/kubelet/kubeconfig"`,
				`--node-labels="node.exoscale.net/nodepool-id=8d1e3f41-9660-4b08-ba6a-c28fb48ba94c"`,
				"-v=1",
			},
		},
		{
			name:    "the last non-empty ExecStart wins",
			content: "[Service]\nExecStart=/first\nExecStart=\nExecStart=/second --a\n",
			want:    []string{"/second", "--a"},
		},
		{
			name:    "a comment inside a continuation is skipped",
			content: "[Service]\nExecStart=/bin/kubelet \\\n# --gone \\\n\t--kept\n",
			want:    []string{"/bin/kubelet", "--kept"},
		},
		{
			name:    "a quoted value with a space stays one word",
			content: "[Service]\nExecStart=/bin/kubelet --node-labels=\"a=b c=d\" -v=1\n",
			want:    []string{"/bin/kubelet", `--node-labels="a=b c=d"`, "-v=1"},
		},
		{
			name:    "an escaped backslash does not continue the line",
			content: "[Service]\nExecStart=/bin/kubelet --x=a\\\\\nExecStart=/bin/kubelet --y\n",
			want:    []string{"/bin/kubelet", "--y"},
		},
		{
			name:    "ExecStart in another section is not the service's",
			content: "[Service]\nExecStart=/bin/kubelet\n[Install]\nExecStart=/bin/other\n",
			want:    []string{"/bin/kubelet"},
		},
		{
			name:    "CRLF line endings",
			content: "[Service]\r\nExecStart=/bin/kubelet \\\r\n\t--a\r\n",
			want:    []string{"/bin/kubelet", "--a"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := lastExecStart(tt.content)
			if err != nil {
				t.Fatalf("lastExecStart() error: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("lastExecStart() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}
