package main

import (
	"os"
	"path/filepath"
	"testing"
)

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
		{
			// kubelite splits each line on whitespace, so a tab separates a
			// flag from its value as well as a space does. Matching only the
			// space spelling left the old flag in place and appended a
			// second copy of it.
			name:    "a tab between the flag and its value is a separator too",
			content: binDirFlag + "\t/old/bin\n--cert-dir=${SNAP_DATA}/certs\n",
			want:    "--cert-dir=${SNAP_DATA}/certs\n" + binDirFlag + "=" + binDir + "\n" + configFlag + "=" + configPath + "\n",
		},
		{
			name:    "an indented flag is still the same flag",
			content: "  " + binDirFlag + "=/old/bin\n--cert-dir=${SNAP_DATA}/certs\n",
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

	requireIdempotentConfigureKubelet(t, opts)

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
