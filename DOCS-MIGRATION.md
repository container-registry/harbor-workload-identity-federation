# Moving the Deep Documentation to container-registry.com/docs

This repository's README is 859 lines. Around half of it is reference material that belongs on the documentation site, where it can be versioned with the product, translated, searched, and linked from the Harbor UI. This file is the plan for moving it: what goes where, what stays, and in what order.

Nothing here has been done yet. The documentation site lives in [`container-registry/container-registry.com`](https://github.com/container-registry/container-registry.com), and a pull request there is a separate step.

## The Split

**Stays here.** Anything a person reads while their terminal is open. Installing the component, the distribution differences, the values, the failure modes, how to build and release.

**Moves.** Anything a person reads to understand the feature or to configure Harbor. Concepts, claim references, per-provider setup, sample tokens, the Harbor-side configuration.

The test: if the answer changes when Harbor changes, it belongs on the docs site. If it changes when this repository changes, it stays here.

## What Already Exists on the Docs Site

Two pages cover the Harbor half today.

| Page | Covers |
|------|--------|
| [`administration-manual/authentication-management/system-robot-accounts/federated-identity-provider`](https://container-registry.com/docs/2.16/administration-manual/authentication-management/system-robot-accounts/federated-identity-provider-for-workload-authentication/) | Configuring a Federated IDP, JWKS caching and rotation, enabling it |
| [`user-manual/images/authenticate-workload-federated-identity`](https://container-registry.com/docs/2.16/user-manual/images/authenticating-a-workload-with-federated-identity/) | Authenticating a workload with a JWT, generic troubleshooting |

Both are generic. Neither covers GitHub Actions, GitLab CI, or Kubernetes specifically, which is exactly the material this README is carrying.

## Inventory

Line ranges are against `README.md` as of this commit, 859 lines. Re-check them before cutting anything; they move whenever the README does.

| README section | Lines | Action | Destination |
|----------------|-------|--------|-------------|
| Title, intro | 1–4 | Rewrite as a short description of the component | — |
| Overview, Benefits | 5–15 | Trim to a paragraph, link out | `authenticating-a-workload-with-federated-identity` |
| Documentation | 16–24 | **Stays**, and grows as pages are added | — |
| Supported Identity Providers | 25–34 | Move | `federated-identity-provider-for-workload-authentication` |
| credential-provider-harbor, Installation | 35–235 | **Stays.** This is the component | — |
| Building from Source | 236–267 | **Stays** | — |
| Releases | 268–281 | **Stays** ([`RELEASES.md`](RELEASES.md)) | — |
| GitHub Actions Example, Key Points | 282–362 | Move | New: `.../github-actions` |
| Debugging JWT Tokens | 363–376 | Move | New: `.../troubleshooting` |
| GitHub Actions example JWT | 377–440 | Move | New: `.../github-actions` |
| GitLab CI Example, Key Points | 441–503 | Move | New: `.../gitlab-ci` |
| GitLab CI example JWT | 504–563 | Move | Same page |
| How It Works | 564–578 | Move | Extend the existing Federated IDP page |
| Kubernetes Setup, How It Works | 579–615 | Split. The concept moves, the commands stay | New: `.../kubernetes` |
| Kubernetes Prerequisites, Quick Start | 616–646 | **Stays** | — |
| Configuration Files | 647–748 | **Collapse.** Already duplicated in [`examples/kubernetes/`](examples/kubernetes/); replace with a link | — |
| Harbor Configuration | 749–765 | Move | New: `.../kubernetes` |
| Kubernetes example JWT | 766–805 | Move | Same page |
| Kubernetes Key Points, Troubleshooting | 806–823 | Split. Row 820 stays (node-side RBAC, already in `examples/kubernetes/`); rows 821–822 move | Troubleshooting page |
| Kubernetes References | 824–830 | **Stays.** KEP-4412 and the 1.34 notes are about the component | — |
| Security Considerations | 831–839 | Move, expand | Federated IDP page |
| References (GitHub Actions, GitLab CI) | 840–851 | Move | The two new provider pages |
| Project | 852–859 | **Stays** | — |

Three actions, not two. **Move** means the content leaves this repository. **Collapse** means it stays in the repository but stops being duplicated in the README, because `examples/kubernetes/` already carries it.

What is left is roughly 300 lines: the component, installation, the profile table, building, releases, the Kubernetes quick start, and links. Most of that is the Installation section, which is 200 lines on its own and is the part of this README that genuinely belongs here.

## Pages to Create

Four new pages, under the existing 2.16 tree. All four are content that exists today in this README and needs editing rather than writing.

**1. `user-manual/images/authenticate-workload-federated-identity/github-actions.md`**
Prerequisites, the `id-token: write` permission, a full workflow, the claim table, a real decoded token. From README 282–362 and 377–440.

**2. `user-manual/images/authenticate-workload-federated-identity/gitlab-ci.md`**
The same shape for GitLab `id_tokens`. From README 441–503 and 504–563.

**3. `user-manual/images/authenticate-workload-federated-identity/kubernetes.md`**
How a kubelet credential provider gets a token and hands it to the registry, the claim table for service account tokens, the Harbor-side configuration including the offline JWKS case, and a decoded token. From README 579–615, 749–765 and 766–805. Links to this repository for the installation itself, rather than repeating it.

**4. `administration-manual/authentication-management/system-robot-accounts/federated-identity-provider/troubleshooting.md`**
Decoding a token, reading the failure, the audience mismatch, the issuer mismatch, JWKS reachability, claim rules that match nothing. From README 363–376 and rows 821–822 of the Troubleshooting table, plus the failure modes already listed on the two existing pages, which are currently split across them. Row 820, `audience not found in pod spec volume`, is node-side RBAC and stays in this repository; do not copy it onto the docs site.

A `supported-providers` page is worth considering as a fifth, but the list is short enough to live on the existing Federated IDP page.

## Pages to Change

| Page | Change |
|------|--------|
| `.../federated-identity-provider` | Extend "How It Works" with the sequence from README 564–578. Add the security considerations from 831–839. Add a link to this repository for the Kubernetes component |
| `.../authenticate-workload-federated-identity` | Add links to the three new per-provider pages. The current "adapt to your CI system" paragraph becomes a pointer |

## Conventions the New Pages Need

From the existing pages in that repository:

- Frontmatter with `title`, `description`, `date`, `weight`, `docSummary`, and a `doc_agent` block (`schema_version`, `product_version`, `doc_type`, `audience`, `assumes`, `pairs_with`, `commands_tested`, `human_reviewed`, `edit_policy`).
- Internal links as `{{< relref "/docs/2.16/..." >}}`, not bare URLs.
- Pages are duplicated per product version. Anything added to `2.16` should be considered for `2.15`, which already carries both federated identity pages.
- Every page has a markdown twin at the same URL plus `/index.md`, so prose has to read correctly without the site chrome.
- URLs come from the page `title`, not the filename. `federated-identity-provider.md` with the title "Federated Identity Provider for Workload Authentication" is served at `.../federated-identity-provider-for-workload-authentication/`. Pick titles with the resulting URL in mind, and take link targets from the section's `index.md` rather than guessing from filenames.

## One Thing to Decide First

The docs site calls this "federated identity provider authentication". This repository calls it "Federated Robot Accounts", after the rename in #10. Those are the same feature under two names, and the migration will spread whichever one is chosen across a dozen pages.

Worth settling before writing any of them, not after.

## Sequence

1. Settle the name.
2. Open the documentation pull request against `container-registry.com` with the four new pages and the two edits. Nothing changes here yet, so nothing breaks.
3. Once those pages are live, open a pull request here that cuts the moved sections from the README and replaces them with links. Splitting it this way means the README never points at a page that does not exist.
4. Check the redirects. Anything linking to a README anchor that is about to disappear, including the Harbor UI and the two existing docs pages, needs updating in step 3.
