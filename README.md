# Knuckle — personal fork

A fork of **[projectbluefin/knuckle](https://github.com/projectbluefin/knuckle)**, kept for
building a locally adapted installer ISO.

All credit for Knuckle goes to the **projectbluefin authors and contributors**. The upstream
project is where the design, the code, and the documentation come from — this fork adds a small
number of local changes on top and nothing else.

**For anything about what Knuckle is, how to use it, how it works, or how to configure it, read
the [upstream README](https://github.com/projectbluefin/knuckle/blob/main/README.md) and
[upstream docs](https://github.com/projectbluefin/knuckle/tree/main/docs).** That documentation is
not duplicated here, so it cannot drift out of date here.

## Changes in this fork

_TBD — to be filled in as changes land._

## Scope of this fork

- **Goal:** build an adapted installer ISO locally.
- **Almost no CI.** Every upstream workflow except `ci.yml` has had its automatic triggers
  (`push`, `pull_request`, `schedule`, tag pushes, issue events) stripped in favour of
  `workflow_dispatch` only, so nothing fires on its own. `ci.yml` is left intact and still runs on
  pushes and PRs to `main`. Everything else is manual, from the Actions tab.
- **No releases.** ISOs are built on the dev machine, not published. `release.yml` no longer fires
  on `v*` tags, so merging an upstream release tag cannot trigger one here.
- **Not a place to file upstream bugs.** Issues and PRs about Knuckle itself belong
  [upstream](https://github.com/projectbluefin/knuckle/issues).

## Development environment

Development runs inside the **devcontainer** in [`.devcontainer/`](.devcontainer/) — open the repo
in VS Code and reopen in container, or use the `devcontainer` CLI. The image is Go 1.26 on Debian
bookworm with `just` and the full ISO build toolchain (`xorriso`, `mtools`, `cpio`,
`systemd-boot-efi`, `squashfs-tools`, `gnupg`) preinstalled, so no host setup is needed beyond a
container runtime.

The Go module cache is a named volume, so rebuilds survive container recreation.

### Building an ISO

```bash
just                     # list all recipes
just build               # binary → bin/knuckle
just iso                 # installer ISO → output/knuckle-installer-stable-amd64.iso
just iso beta            # pick a different Flatcar channel
KNUCKLE_ARCH=arm64 just iso
```

Before pushing anything to `main`, run the same gate CI would have run:

```bash
just ci                  # tidy + fmt + vet + lint + vuln + test-race + cover + headless-e2e + build
```

QEMU-based recipes (`just vm`, `just boot-iso`, `just e2e`) need KVM and OVMF on the **host** —
they are not usable from inside the devcontainer without extra device passthrough.

## Keeping the fork in sync

This fork has **two permanent branches**, both hosted on this repo and tracked locally:

| Branch | Purpose |
|---|---|
| `upstream-main` | A 1:1 mirror of `projectbluefin/knuckle@main`. **Never commit here.** |
| `main` | This fork's line — upstream plus the local changes. Default branch. |

There is deliberately **no `upstream` git remote**. Both local branches track `origin`
(this fork), and upstream commits enter the repo through GitHub's fork sync, not through a
second remote.

### Integrating upstream changes

**1. Sync `upstream-main` on GitHub.**

On this repo's GitHub page, switch to the `upstream-main` branch. The branch bar compares it
against `projectbluefin/knuckle:main` and shows how far behind it is — use that to review what is
incoming. Then **Sync fork → Update branch**.

Because `upstream-main` never diverges, this is always a fast-forward.

**2. Pull it down.**

```bash
git checkout upstream-main
git pull --ff-only
```

`--ff-only` is the guard on the mirror invariant: if `upstream-main` ever picked up a commit of
its own, the pull fails loudly instead of quietly creating a merge commit.

**3. Merge into `main` and push.**

```bash
git checkout main
git merge upstream-main
# resolve any conflicts, then:
just ci
git push origin main
```

### Notes

- GitHub also offers **Sync fork** on `main`, since it compares every branch to the upstream
  default branch. Do not use it there — it merges upstream straight into `main` on the server,
  bypassing `upstream-main` and leaving a merge commit you did not make locally.
- Feature work goes on topic branches off `main`, and merges back into `main`.

## License

Apache 2.0, unchanged from upstream. See [`LICENSE`](LICENSE).
