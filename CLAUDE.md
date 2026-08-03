# CLAUDE.md

Fork-specific instructions. For everything about the *code* — architecture, package boundaries,
safety invariants, testing bar — read [`AGENTS.md`](AGENTS.md), which is upstream's agent context
and still applies in full. This file only covers what is different because this is a fork.

## This repo is a fork

`hirnidrin/knuckle` is a personal fork of `projectbluefin/knuckle`. Upstream owns the project;
this fork carries a small set of local changes and exists to build an adapted installer ISO
locally.

Consequences for how you work here:

- **Prefer minimal divergence.** Every line that differs from upstream is a line that has to be
  reconciled on the next sync. When there is a choice between a local patch and an upstream-shaped
  change, pick the one that conflicts less.
- **Do not "fix" upstream code that is not part of the current task.** Drive-by refactors,
  reformatting, and unrelated cleanups create merge conflicts for no benefit.
- **Fork-local files** (`README.md`, `CLAUDE.md`, `.devcontainer/`) are expected to diverge
  permanently and will conflict on every upstream touch. That is fine — resolve in favour of the
  fork's version unless told otherwise.

## Branch model

| Branch | Rule |
|---|---|
| `upstream-main` | 1:1 mirror of `projectbluefin/knuckle@main`. **Never commit to it, never merge into it, never push to it.** It is updated only by GitHub fork sync followed by `git pull --ff-only`. |
| `main` | The fork's line. Default branch. Upstream plus local changes. |

Both branches track `origin` (the fork). There is **no `upstream` git remote** — do not add one
without being asked.

Upstream changes are integrated by the user, per the procedure in
[`README.md`](README.md#integrating-upstream-changes). Do not run that procedure unprompted.

## Pushing — ask first, always

**Never push to `main` yourself.** When work is ready, stop and tell the user, so the diff can be
reviewed before it lands.

Specifically:

- Do not run `git push` to `main` under any circumstances, including after a "looks good" on the
  code itself — approval of a change is not approval to push it.
- Do not run `git push --force` anywhere.
- Committing locally is fine when asked; pushing is the user's call.
- The same applies to anything that publishes: creating PRs, creating tags, creating releases.

More generally, the user runs git commands that modify remotes, branches, or repo config
themselves. Give the exact command as text rather than invoking it. Read-only inspection
(`git status`, `git log`, `git diff`) is fine to run directly.

## CI is mostly off

Every workflow under `.github/workflows/` except `ci.yml` has had its automatic triggers replaced
with `workflow_dispatch:` only — they never fire on their own and are marked with a `# FORK:`
comment recording what the upstream trigger was. `ci.yml` is deliberately left untouched and still
runs on pushes and PRs to `main`.

So: `security.yml`, `nightly.yml`, `post-merge-smoke.yml`, `vm-e2e.yml`, `release.yml`, and
`bonedigger.yml` are not a source of truth here. Do not assume one ran, and do not cite a workflow
as evidence that something passed.

When upstream edits any of those `on:` blocks, the sync will conflict — keep the fork's
`workflow_dispatch:`-only version and update the `# FORK:` comment to match the new upstream
triggers.

The local gate is:

```bash
just ci
```

Run it before declaring work done, and report the actual result. If it fails, say so with the
output.
