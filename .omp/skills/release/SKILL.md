---
name: release
description: Publish a new ficus version (tag, GitHub release, installers) through make release bump=patch|minor|major, or roll one back with make unrelease. Use when the user asks to release, deploy, tag a version, or undo a release.
enforced-by: [strict-publish-consent, strict-release-route, check-release]
---

# Release

The git tag is the version; `make release` is the only way to make one
(rule://release). `strict-release-route` blocks raw `git tag`, tag pushes, `gh release`
writes and a publishing `goreleaser release`; `strict-publish-consent` blocks
`make release`/`make unrelease` unless the current user turn asks for a release.

## Before

1. The change is committed and pushed (`commit` skill), and CI for that commit is
   green: `gh run list --workflow ci.yml --limit 1`.
2. Pick the bump from what changed since the last tag
   (`git log --oneline $(git describe --tags --abbrev=0)..HEAD`): `patch` fixes,
   `minor` new behaviour or settings, `major` a user must change something. If the
   user did not say, propose one and why — do not guess silently.
3. The README's recording (`docs/demo.gif`) must match the menu users will get.
   If what the menu shows or how keys behave changed since the last tag, re-record
   it (needs vhs; about 40 s; a throwaway repository under /tmp, never the user's
   own startup files):

```bash
git diff --stat $(git describe --tags --abbrev=0)..HEAD -- shell internal/engine/render.go internal/engine/theme.go internal/engine/output.go
make demo
```

   Look at the result before committing it — extract frames and view them:
   `ffmpeg -ss 5 -i docs/demo.gif -frames:v 1 /tmp/demo-5s.png` (every few
   seconds). If a scene no longer matches the keys (a new default, a changed key),
   fix `docs/demo.tape` and record again. Commit the recording (`commit` skill) so
   it is part of the released commit. The dry run below prints a note when the
   drawing code changed and the recording did not.
4. Dry run — every check, no tag:

```bash
make release bump=patch DRY_RUN=1
```

## Release

```bash
make release bump=patch
```

It prints each step (`==>`): CI, `vA -> vB`, the commits going out, the tag push,
the release workflow (about 8 minutes), the uploaded files, then `verify-release`
installing the version on Ubuntu, macOS and Windows (about 3 minutes). Report the
release URL and the verify-release job lines it prints.

## When It Fails

Read the `release:` line — it names the step and the run URL.

- Before the tag (branch, dirty tree, unpushed, CI red/missing, tag exists): nothing
  was published. Fix it and run again.
- Release workflow failed: the tag is pushed but the release is missing or partial.
  `gh run view <id> --log-failed`, fix, then release the next version. Remove the
  broken one only if the user agrees: `make unrelease v=vX.Y.Z`.
- Files missing or verify-release failed: the release is public. Report it; fix and
  release the next version, or `make unrelease v=vX.Y.Z` if it harms users
  (`releases/latest` then points at the previous release).

Never re-tag or move a published version; release the next one.

## Changing What Is Released

Archive names, packages, platforms and the repository live in `.goreleaser.yaml`;
`scripts/release.sh`, `install.sh`, `install.ps1` and `verify-release.yml` must
follow. `make check-release` (part of `make verify`) reports any mismatch. Check
the result without publishing with `make dist` (needs GoReleaser).
