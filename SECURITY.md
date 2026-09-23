# Security Policy

## Reporting a Vulnerability

Report vulnerabilities privately through [GitHub Security Advisories](https://github.com/container-registry/harbor-workload-identity-federation/security/advisories/new), or by email to security@8gears.com.

Do not open a public issue, and do not include a working exploit in the initial report.

Please include:

- The affected component: credential provider, installer, Helm chart, or an example.
- The version or commit.
- What an attacker gains, and what access they need to get it.
- Steps to reproduce.

You will get an acknowledgement within three working days. We will keep you updated while we work on a fix, and will credit you in the advisory unless you ask us not to.

## Supported Versions

Fixes land on `main` and ship in the next release. Older releases are not patched.

## Scope Notes

Two properties are worth stating plainly, because they shape what counts as a vulnerability here.

The installer runs as a privileged DaemonSet with the host filesystem mounted. That is what lets it place a binary on the node and edit the kubelet configuration. Anyone who can install this chart can already run arbitrary code on every node, so that capability by itself is not a finding. Escalation paths that do not require chart-install privileges are.

The credential provider hands the kubelet a service account token as a registry password. The token is scoped to the audience configured in Harbor and is short lived, but the registry does see it. A report that the registry can read the token it was given is expected behavior. A report that a token leaks somewhere else, such as into logs, a world-readable file on the node, or a request to a host other than the configured registry, is a vulnerability.
