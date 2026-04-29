package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
}
