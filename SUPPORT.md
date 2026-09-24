# Support

How to get help with the Harbor Federated Robot Accounts credential provider.

## Documentation

- Feature documentation: https://container-registry.com/docs/
- Trusted Issuer setup (Harbor side): https://container-registry.com/docs/2.16/administration-manual/authentication-management/system-robot-accounts/federated-identity-provider-for-workload-authentication/
- Authenticating a workload: https://container-registry.com/docs/2.16/user-manual/images/authenticating-a-workload-with-federated-identity/

The quick guides in this repository cover the Kubernetes side: installing `credential-provider-harbor` on nodes and wiring up kubelet.

## Questions

Open an issue and say up front that it is a question. Pick "Open a blank issue"
at the bottom of the chooser, since the forms there are for bugs, features, and
proposals:
https://github.com/container-registry/harbor-workload-identity-federation/issues/new/choose

Include your Kubernetes distribution and version, the installation profile you used, and the kubelet logs around the failed pull. Most reports come down to a mismatch between the audience configured in Harbor and the audience the kubelet requests.

## Reporting Issues

### Bug Reports

https://github.com/container-registry/harbor-workload-identity-federation/issues/new?template=bug_report.yml

Before filing, search existing issues, and gather the credential provider config from the node plus the kubelet log around the failed pull. On systemd nodes that is `journalctl -u kubelet`; on Talos it is `talosctl logs kubelet`, and on MicroK8s `journalctl -u snap.microk8s.daemon-kubelite`.

### Feature Requests

https://github.com/container-registry/harbor-workload-identity-federation/issues/new?template=feature_request.yml

### Security Issues

Do not file public issues for security concerns. Report them privately:
https://github.com/container-registry/harbor-workload-identity-federation/security/advisories/new

## Commercial Support

Customers of 8gears Container Registry can reach technical support at support@8gears.com or through https://container-registry.com/contact/support/.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).
