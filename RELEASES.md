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

The chart's `appVersion` is not hand-edited. It carries the `# x-release-please-version` annotation and is listed in the binary train's `extra-files`, so every binary release rewrites it to the version that was just released.

Only the binary train writes it. The chart train uses release-please's `helm` strategy, which updates `Chart.yaml` through the `ChartYaml` updater; that updater sets `version` and nothing else ([`src/updaters/helm/chart-yaml.ts`](https://github.com/googleapis/release-please/blob/main/src/updaters/helm/chart-yaml.ts)). The chart package declares no `extra-files`, and per-package config overrides the top level rather than merging with it ([`mergeReleaserConfig`](https://github.com/googleapis/release-please/blob/main/src/manifest.ts)), so it inherits none either. A chart-only release therefore cannot move `appVersion` onto a version the binary train never built.

That matters because the chart defaults its image tag to `v` plus `appVersion`. `task version-check` asserts `appVersion` equals the `"."` entry in `.release-please-manifest.json`, and CI runs it on every pull request including release-please's own, so a future release-please change that started writing `appVersion` from the chart train would fail the release pull request rather than ship a chart whose image pull 404s.

The same override-not-merge rule applies to `exclude-paths`. The root package repeats `CONTRIBUTORS.md` from the top level rather than relying on it, because a per-package list replaces the top-level one instead of adding to it. Drop that repeat and the root package excludes nothing, so every all-contributors commit cuts a patch release of the binaries.

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

The commit type decides how far the version moves and whether the commit is listed. It does not decide *whether* a release happens: release-please opens a release pull request for any conventional commit in range, and `DefaultVersioningStrategy.determineReleaseType` falls through to a patch bump for every type that is not a feature or a breaking change ([`src/versioning-strategies/default.ts`](https://github.com/googleapis/release-please/blob/main/src/versioning-strategies/default.ts)). A range of nothing but `chore:` still cuts a patch.

| Commit type | Version change | Release notes |
|-------------|----------------|---------------|
| `feat!:` or `BREAKING CHANGE:` | Major, or minor while on 0.x | Breaking changes |
| `feat:` | Minor | Features |
| `fix:` | Patch | Bug Fixes |
| `perf:`, `revert:`, `docs:`, `refactor:` | Patch | Listed |
| `ci:`, `chore:`, `build:`, `style:`, `test:` | Patch | Hidden |

That is why `exclude-paths` matters below: a path that is excluded produces no commits for the train to see, which is the only way to keep an automated commit from cutting a release.

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

`release-assets.yml`, `release-image.yml`, and `release-chart.yml` also accept `workflow_dispatch`, which is the way to re-run a publish for a tag that already exists without cutting a new release. Each dispatch path applies the same three checks before anything is checked out:

1. The tag name has to be exactly `vX.Y.Z`, or `chart-vX.Y.Z` for the chart. The whole value is matched, so a multi-line input cannot slip a second line past the check.
2. A published GitHub Release has to exist for it. A tag alone proves nothing, because anyone with write access can push one.
3. The tag is resolved to the commit it points at, and that commit id is what gets checked out. A bare tag name loses to a branch of the same name, and a tag can be retargeted after the Release check; a commit id is neither.

`release-image.yml` is also callable from `ci.yml`, which passes its own ref. That path publishes a development tag and never moves `latest`.

`release-chart.yml` splits packaging from pushing. The packaging job runs the tag's `Taskfile` and holds no registry password; the push job holds the password, checks nothing out, and runs `helm push` on the packaged tarball directly. `task helm-push` stays the local equivalent.

## Required Repository Settings

- Squash merging enabled. release-please reads the squashed commit, so the pull request title is what it parses.
- GitHub Actions allowed to create and approve pull requests. Without it release-please cannot open the release pull request at all.

Set both under Settings in the repository. The workflows do not check them, so a wrong setting shows up as a failed or missing release rather than a warning.

Token scopes are not a repository setting; they live in the workflow files. Permissions are granted per job rather than workflow-wide: only release-please needs `pull-requests: write`, the asset upload job needs `contents: write` to attach files to the release, and the image and chart jobs need only `contents: read` for their checkout.
