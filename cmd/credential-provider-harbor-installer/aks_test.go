package main

import (
	"os"
	"path/filepath"
	"testing"
)

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
		{
			// A node hand-patched before this installer existed can carry an
			// unquoted path with a space in it. The tail of that path is not
			// an argument of its own: left behind, kubelet reads it as the
			// value of --max-pods.
			name:    "the tail of a space-containing value goes with the flag",
			content: "KUBELET_FLAGS=\"" + binDirFlag + "=/old/bin dir --max-pods=110\"\n",
			want:    "KUBELET_FLAGS=\"--max-pods=110 " + binDirFlag + "=" + binDir + " " + configFlag + "=" + configPath + "\"\n",
		},
		{
			name:    "the tail goes with the space-separated spelling too",
			content: "KUBELET_FLAGS=\"--max-pods=110 " + configFlag + " /old/config dir.yaml\"\n",
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

	requireIdempotentConfigureKubelet(t, opts)

	data, err := os.ReadFile(defaultsPath)
	if err != nil {
		t.Fatalf("read defaults: %v", err)
	}
	// The whole file, not a substring each: flags appended outside the
	// quotes, a duplicated flag or a mangled quote all carry the substrings
	// and would pass, and this file is the only end state the AKS write path
	// has.
	want := "KUBELET_FLAGS=\"--max-pods=110 " +
		binDirFlag + "=/usr/local/bin/credential-providers " +
		configFlag + "=/etc/kubernetes/credential-providers/config.yaml\"\n"
	if string(data) != want {
		t.Fatalf("defaults =\n%q\nwant\n%q", data, want)
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
