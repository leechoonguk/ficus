---
description: Versions and releases — the git tag is the version; release only through make release bump=…, only when asked; release config, script, installers and workflows stay in step
globs:
  - .goreleaser.yaml
  - scripts/release.sh
  - install.sh
  - install.ps1
  - .github/workflows/**
  - .omp/skills/release/**
alwaysApply: false
---

# Release

| Gate | Hook | Blocks |
|---|---|---|
| Consent | `strict-publish-consent.ts` | `make release` / `make unrelease` when the current user turn does not ask for a release (릴리스, 배포, 태그, release, tag …), or asks and forbids it; `make release … DRY_RUN=1` is exempt |
| Route | `strict-release-route.ts` | raw `git tag` writes, pushing a version tag, `gh release create/delete/edit/upload`, `gh workflow run release.yml`, publishing `goreleaser release` |
| Consistency | `check-release.ts` (in `make verify`) | the release files, `scripts/release.sh`, the installers and the workflows naming different files or repositories |

## Versions

- The version **is** the annotated git tag `vMAJOR.MINOR.PATCH`. There is no version
  file: `ficus version`, the release files and the release notes all come from the tag.
- `bump=patch` — fixes, no change a user must learn. `bump=minor` — new behaviour or
  settings, old ones keep working. `bump=major` — a user must change something
  (removed setting, changed key, different install path). Before 1.0, breaking
  changes may go in a minor bump; say so in the release notes.
- Release notes are the commit subjects since the last tag (`docs:`/`test:` are
  left out), so write subjects a user can read.

## Releasing

`make release bump=patch|minor|major` (procedure: the `release` skill):

1. `make verify`.
2. `scripts/release.sh` refuses unless: on `main`, the worktree is clean, HEAD is
   `origin/main`, the `ci` workflow passed for that exact commit (it waits while it
   runs), the commit is not already released, the next tag does not exist.
3. Tags the next version, pushes the tag, waits for the `release` workflow.
4. Checks the release has every file `.goreleaser.yaml` publishes.
5. Runs `verify-release` for the version: install.sh on Ubuntu and macOS, the .deb,
   install.ps1 in PowerShell 7 and Windows PowerShell 5.1 — install, setup,
   completion, removal.

`DRY_RUN=1` runs every check of step 2 and prints the version and commits, without
tagging. It publishes nothing, so it needs no release request — run it first when unsure.

## When it fails

| Where | State | What to do |
|---|---|---|
| Steps 1–2 | nothing published | fix what it names; run again |
| Step 3 (workflow failed) | tag pushed, release missing or partial | fix, then `make release bump=patch` (next version), or `make unrelease v=vX.Y.Z` |
| Steps 4–5 | release public | fix and release the next version; `make unrelease v=vX.Y.Z` only if the release harms users — `releases/latest` falls back to the previous one |

Never move or reuse a published tag: installers and caches key on it.

## Consistency (`check-release`)

`.goreleaser.yaml` decides the file names (`ficus_<os>_<arch>.tar.gz|zip`,
`ficus_<arch>.deb|rpm`, `checksums.txt`, `install.sh`, `install.ps1`). The asset list
in `scripts/release.sh`, the downloads in `install.sh`/`install.ps1`/`verify-release.yml`
and the repository slug in all of them must match it. Change them together.
