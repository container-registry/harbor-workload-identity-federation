# Contributing

Thanks for your interest in this project. It holds the Kubernetes side of Harbor Federated Robot Accounts: the kubelet credential provider, the on-node installer, the Helm chart, and runnable examples per distribution.

## Before You Start

Read the [Code of Conduct](CODE_OF_CONDUCT.md). It applies to issues, pull requests, and any other project space.

For anything that changes the on-node layout, the installation model, or the credential provider protocol, open a [proposal](https://github.com/container-registry/harbor-workload-identity-federation/issues/new?template=proposal.yml) first. Smaller changes can go straight to a pull request.

## Development Setup

You need Go (the version in `go.mod`), [Task](https://taskfile.dev/installation/), and Helm. `typos` ships as a prebuilt binary rather than a Go module, so you also need either `curl` (to download the release) or Cargo (to build it). The extra CI tools are installed for you:

```bash
task setup
```

That installs `typos` and `golangci-lint`. Each one is pinned to a specific version, so an older copy already on your machine is replaced rather than kept. They land in your Go bin directory (`GOBIN`, or `$(go env GOPATH)/bin`), and setup stops with the exact `export PATH=...` line to run if your shell does not look there yet.

Common tasks:

```bash
task build          # build both binaries for the current platform
task build-all      # cross-compile for linux/amd64 and linux/arm64
task test           # go test -race -v ./...
task lint           # golangci-lint
task helm-lint      # lint the chart
task helm-template  # render the chart for every supported profile
task check          # everything CI checks that does not need a cluster
```

Run `task check` before pushing. It runs the same spell check, lint, test, build, and chart jobs CI does. CI checks the pull request title rather than each commit, and the dco2 app checks sign-off.

## Testing Against a Cluster

The credential provider only does something useful when a kubelet calls it, so test on a real node. `examples/kubernetes` has a k3d setup that runs end to end on a laptop; `examples/talos` covers Talos through a system extension. Say in the pull request which distribution and profile you exercised.

## Commits

Commit subjects follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add RKE2 installation profile
fix(installer): keep the ECR provider entry on EKS
docs: document the audience mismatch failure mode
```

The pull request title matters as much as the commits, because pull requests are squash-merged and the title becomes the commit message on `main`. That commit is what release tooling reads, so a vague title costs more than a vague commit.

Every commit needs a [Developer Certificate of Origin](https://developercertificate.org/) sign-off:

```bash
git commit -s -m "fix: keep the ECR provider entry on EKS"
```

## Pull Requests

- Branch from `main`, one logical change per pull request.
- Keep the diff reviewable. A large mechanical change and a behavior change belong in separate pull requests.
- Update the affected README or example alongside the code.
- CI must be green before review.

Maintainers squash-merge. You do not need to rebase or squash your own commits.

## Reporting Security Issues

See [SECURITY.md](SECURITY.md). Do not open a public issue for a vulnerability.
