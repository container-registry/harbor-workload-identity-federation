# How It Works

Federated Robot Accounts let Harbor authenticate a client with a short-lived JWT the client's own platform issued, instead of a robot account secret somebody had to store. Harbor trusts the issuer, checks the signature, and maps the token's claims to a robot account.

Two things present such a token to Harbor: a CI job, and a kubelet pulling an image for a pod.

## From a CI Job

1. The platform issues an OIDC token to the job. GitHub Actions serves one from `ACTIONS_ID_TOKEN_REQUEST_URL`; GitLab CI puts one in a variable named by the `id_tokens` keyword.
2. The job uses the token as the password in `docker login`. The username is never read, so anything will do.
3. Harbor verifies the signature against the issuer's JWKS, checks `iss`, `exp` and `aud`, and then matches the remaining claims against the claim rules on each federated robot account.
4. The matched robot account's permissions decide what the request may do.

[`examples/github-actions/`](../examples/github-actions/) and [`examples/gitlab-ci/`](../examples/gitlab-ci/) have a working workflow and pipeline.

## From a Kubelet

Kubernetes 1.34 added service account tokens for image credential providers ([KEP-4412](https://github.com/kubernetes/enhancements/tree/master/keps/sig-auth/4412-projected-service-account-tokens-for-kubelet-image-credential-providers)). The kubelet can request a token for the pod's service account with an audience of its choosing and hand it to a credential provider plugin. `credential-provider-harbor` is that plugin: it reads a `CredentialProviderRequest` on stdin and writes back Basic Auth credentials of `jwt:<service account token>`.

```text
┌─────────────────────────────────────────────────────────────────────────────┐
│                                 Cluster                                      │
│  ┌─────────────────┐    ┌──────────────────┐    ┌───────────────────────┐   │
│  │   Pod (httpd)   │    │     Kubelet      │    │  Credential Provider  │   │
│  │                 │───>│                  │───>│  Plugin               │   │
│  │ Uses SA: default│    │ Requests SA token│    │ Returns Basic Auth    │   │
│  └─────────────────┘    │ with audience    │    │ (jwt:<SA token>)      │   │
│                         └──────────────────┘    └───────────────────────┘   │
│                                  │                                          │
│                                  v                                          │
│                         ┌──────────────────┐                                │
│                         │    containerd    │                                │
│                         │ (pulls image)    │                                │
│                         └────────┬─────────┘                                │
└──────────────────────────────────┼──────────────────────────────────────────┘
                                   │ Basic Auth: jwt:<k8s-sa-token>
                                   v
┌─────────────────────────────────────────────────────────────────────────────┐
│                            Harbor Registry                                   │
│  ┌──────────────────┐    ┌─────────────────────────────────────────────┐    │
│  │ robotjwt         │───>│ Validates K8s JWT signature via JWKS        │    │
│  │ middleware       │    │ Maps claims to robot account permissions    │    │
│  └──────────────────┘    └─────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────────────────┘
```

Nothing has to change in the pod. No `imagePullSecrets`, and no projected volume: the kubelet requests the token itself.

Three things have to line up for that to happen, and they are the first things to check when a pull fails. [`examples/kubernetes/`](../examples/kubernetes/) covers them in detail:

1. kubelet is running with `--image-credential-provider-bin-dir` and `--image-credential-provider-config`. Writing the files is not enough. [`scripts/verify-node-install.sh`](../scripts/verify-node-install.sh) reads the live command line on every node.
2. The node audience RBAC grants `request-serviceaccounts-token-audience` on your audience to `system:nodes`. The chart creates it. Without it the kubelet is refused a token, and the error mentions a pod spec volume.
3. Harbor's Trusted Issuer expects the same audience string the kubelet asks for.

The audience is an agreed identifier and does not have to be a domain. The examples use the registry hostname because that makes it obvious who the token is for.

Your registry does not belong in the API server's `--api-audiences`. That flag lists the audiences the API server accepts on tokens presented to it, so putting your registry there means a token you gave Harbor also works as a cluster credential.
