# Release Process

Releases are automated with [release-please](https://github.com/googleapis/release-please). Do not create tags or GitHub Releases by hand.

Release state lives in conventional commits on `main`, `release-please-config.json`, `.release-please-manifest.json`, and the two `CHANGELOG.md` files.

## Two Release Trains

This repository releases two things on independent version lines.

| Train | Path | Tag | Version file |
|-------|------|-----|--------------|
| Binaries and image | `.` | `vX.Y.Z` | `.release-please-manifest.json` |
| Helm chart | `deploy/helm/credential-provider-harbor` | `chart-vX.Y.Z` | `Chart.yaml` `version` |

They are separate because the chart already sits at `0.1.x` while the binaries are at `0.0.x`, and because a chart-only fix should not force a binary release. release-please assigns each commit to a train by the paths it touches: a commit that only touches the chart directory bumps the chart, anything else bumps the binaries, and a commit touching both bumps both.

The chart's `appVersion` is not hand-edited. It carries the `# x-release-please-version` annotation, so every binary release rewrites it to the version that was just released.

## How a Release Happens

1. Pull requests are squash-merged to `main` with a conventional title. That title becomes the commit release-please reads.
2. The push to `main` opens or updates a release pull request, `chore: release X.Y.Z` or `chore: release chart X.Y.Z`.
3. Merging that pull request updates the manifest and changelog, creates the tag, and publishes the GitHub Release.
4. `release-please.yml` then runs the publish jobs for whichever train released.

Each train publishes only its own artifacts. A chart-only release does not rebuild the binaries or the image, and a binary release does not publish the chart.

| Train | What gets published |
|-------|---------------------|
| Binaries and image | `linux/amd64` and `linux/arm64` builds of `credential-provider-harbor` and `credential-provider-harbor-installer`, plus `SHA256SUMS`, attached to the GitHub Release. The multi-arch deployer image is pushed with the release tag and `latest`. |
| Helm chart | The chart is packaged from `Chart.yaml` and pushed as an OCI artifact. |

Only `feat:`, `fix:`, and breaking changes create a release. Other types ride along in the next one.

| Commit type | Version change | Release notes |
|-------------|----------------|---------------|
| `feat:` | Minor | Features |
| `fix:` | Patch | Bug Fixes |
| `feat!:` or `BREAKING CHANGE:` | Major, or minor while on 0.x | Breaking changes |
| `perf:`, `revert:`, `docs:`, `refactor:` | None | Included in the next release |
| `ci:`, `chore:`, `build:`, `style:`, `test:` | None | Hidden |

## Where Artifacts Land

The registry is set by repository variables, so a fork can publish somewhere else without editing workflows.

```text
REGISTRY_ADDRESS   default 8gears.container-registry.com
PROJECT_NAME       default 8gcr
REGISTRY_USERNAME  robot account or user with push access
REGISTRY_PASSWORD  secret, not a variable
```

With the defaults:

```text
image  8gears.container-registry.com/8gcr/credential-provider-harbor-deployer:vX.Y.Z
chart  oci://8gears.container-registry.com/8gcr/credential-provider-harbor:X.Y.Z
```

Pushes to `main` also publish a development image tagged with `git describe` output, for example `v0.0.1-8-g4564615`. They do not move `latest`: merging a release pull request triggers both this and the release workflow, and whichever finished last would own the tag. `latest` follows releases only.

The chart does not pick up development images on its own. With `image.tag` unset it resolves to `v` plus `Chart.appVersion`, which is always a released tag. To run a development build, set it:

```bash
--set image.tag=v0.0.1-8-g4564615
```

## Running the Publish Steps by Hand

Every publish step is a Taskfile task, so it runs the same way locally and in CI.

```bash
task release-assets VERSION=v0.1.0   # binaries and checksums into dist/
task docker-push VERSION=v0.1.0      # multi-arch image, moves :latest
task docker-push-version VERSION=v0.1.0  # multi-arch image, leaves :latest alone
task helm-package                    # chart tarball into dist/
task helm-push                       # chart to the OCI registry
```

`release-assets.yml`, `release-image.yml`, and `release-chart.yml` also accept `workflow_dispatch`, which is the way to re-run a publish for a tag that already exists without cutting a new release.

## Required Repository Settings

- Squash merging enabled, merge commits and rebase merging disabled.
- GitHub Actions allowed to create and approve pull requests.
- Permissions scoped per job rather than workflow-wide. Only release-please needs `pull-requests: write`; the asset upload job needs `contents: write` to attach files to the release; the image and chart jobs need neither.

These are declared in `.github/settings.yml`. Applying them needs a `SETTINGS_TOKEN` with repository administration access.
