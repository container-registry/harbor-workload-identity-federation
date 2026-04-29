package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/kubelet/pkg/apis/credentialprovider/v1"
)

func TestHandleReturnsTokenCredentials(t *testing.T) {
	request := v1.CredentialProviderRequest{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1.SchemeGroupVersion.String(),
			Kind:       "CredentialProviderRequest",
		},
		Image:               "harbor.example.com/library/nginx:1.25",
		ServiceAccountToken: "service-account-token",
	}

	var stdout bytes.Buffer
	if err := handle("jwt", jsonReader(t, request), &stdout); err != nil {
		t.Fatalf("handle() returned error: %v", err)
	}

	var response v1.CredentialProviderResponse
	if err := json.NewDecoder(&stdout).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.APIVersion != v1.SchemeGroupVersion.String() {
		t.Errorf("APIVersion = %q, want %q", response.APIVersion, v1.SchemeGroupVersion.String())
	}
	if response.Kind != "CredentialProviderResponse" {
		t.Errorf("Kind = %q, want CredentialProviderResponse", response.Kind)
	}
	if response.CacheKeyType != v1.RegistryPluginCacheKeyType {
		t.Errorf("CacheKeyType = %q, want %q", response.CacheKeyType, v1.RegistryPluginCacheKeyType)
	}
	if response.CacheDuration != nil {
		t.Errorf("CacheDuration = %v, want nil so kubelet defaultCacheDuration is used", response.CacheDuration)
	}

	auth, ok := response.Auth["harbor.example.com"]
	if !ok {
		t.Fatalf("response.Auth missing harbor.example.com: %#v", response.Auth)
	}
	if auth.Username != "jwt" {
		t.Errorf("Username = %q, want jwt", auth.Username)
	}
	if auth.Password != "service-account-token" {
		t.Errorf("Password = %q, want service-account-token", auth.Password)
	}
}

func TestHandleRejectsMissingServiceAccountToken(t *testing.T) {
	request := v1.CredentialProviderRequest{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1.SchemeGroupVersion.String(),
			Kind:       "CredentialProviderRequest",
		},
		Image: "harbor.example.com/library/nginx:1.25",
	}

	var stdout bytes.Buffer
	err := handle("jwt", jsonReader(t, request), &stdout)
	if err == nil {
		t.Fatal("handle() returned nil error, want missing token error")
	}
	if !strings.Contains(err.Error(), "service account token") {
		t.Fatalf("handle() error = %q, want service account token error", err)
	}
}

func TestRegistryHost(t *testing.T) {
	tests := []struct {
		name  string
		image string
		want  string
	}{
		{
			name:  "tagged image",
			image: "harbor.example.com/library/nginx:1.25",
			want:  "harbor.example.com",
		},
		{
			name:  "registry with port",
			image: "harbor.example.com:8443/library/nginx:1.25",
			want:  "harbor.example.com:8443",
		},
		{
			name:  "image digest",
			image: "harbor.example.com/library/nginx@sha256:abc123",
			want:  "harbor.example.com",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := registryHost(test.image); got != test.want {
				t.Fatalf("registryHost(%q) = %q, want %q", test.image, got, test.want)
			}
		})
	}
}

func jsonReader(t *testing.T, value any) *bytes.Reader {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return bytes.NewReader(data)
}
