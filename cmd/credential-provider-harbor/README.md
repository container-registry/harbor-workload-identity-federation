credential-provider-harbor is an image credential provider plugin for Kubernetes
that uses service account tokens directly as Harbor registry passwords via
Workload Identity Federation (FedIDP).

Usage:

  request='{"apiVersion":"credentialprovider.kubelet.k8s.io/v1","kind":"CredentialProviderRequest","image":"...","serviceAccountToken":"..."}'
  echo "${request}" | credential-provider-harbor [--username=USER]

credential-provider-harbor is called with STDIN of a JSON-serialized
credentialprovider.kubelet.k8s.io/v1 CredentialProviderRequest, which must
contain a `serviceAccountToken` value. For example:

  {
    "apiVersion": "credentialprovider.kubelet.k8s.io/v1",
    "kind": "CredentialProviderRequest",
    "image": "...",
    "serviceAccountToken": "..."
  }

To configure this credential plugin on a node:

  1. Create a directory for credential plugin binaries, and place this binary
     in that directory

  2. Create a CredentialProviderConfig configuration file containing:

  kind: CredentialProviderConfig
  apiVersion: kubelet.config.k8s.io/v1
  providers:
  - name: credential-provider-harbor
    apiVersion: credentialprovider.kubelet.k8s.io/v1
    tokenAttributes:
      requireServiceAccount: true
      serviceAccountTokenAudience: "" # TODO: replace with your Harbor registry hostname
      cacheType: Token

    matchImages:
    - "" # TODO: replace with your Harbor registry hostname(s)

    defaultCacheDuration: "1h"

    # optionally specify the username to include in registry credentials, default is "jwt"
    args:
    - "--username=jwt"

   defaultCacheDuration is required by the kubelet config API. The binary returns
   a zero cache duration because it passes service account tokens through as the
   registry password.

  3. Adjust the kubelet startup flags to point at that configuration file:

  --image-credential-provider-bin-dir=/path/to/step-1/directory
  --image-credential-provider-config=/path/to/step-2/file
