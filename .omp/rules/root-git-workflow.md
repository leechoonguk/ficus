---
description: Git publishing — commit/push only when the current user turn asks, through make commit / make push, with the whole verified worktree
globs:
  - Makefile
  - .omp/skills/commit/**
alwaysApply: false
---

# Git Workflow

| Gate | Hook | Blocks |
|---|---|---|
| Consent | `strict-publish-consent.ts` | `git commit`/`git push`/`make commit`/`make push` when the current user turn does not ask for it, or asks and forbids in the same message |
| Verified tree | `strict-verified-git.ts` | a raw `git commit` with unstaged or untracked files, or any commit/push while `make verify` fails |

- Publish with `make commit m="type(scope): subject"` (or `f=<file>` for a body)
  and `make push`; both run `verify` first and `commit` stages the whole worktree.
- To commit less than the worktree, `git stash -k` the rest first.
- Subjects are release notes (rule://release): `feat:`, `fix:`, `docs:`, `test:`,
  `ci:` … with a subject a user can read.
- `bin/`, `dist/` and `.omp/state/` are gitignored — rebuilt, not versioned.
- Procedure: the `commit` skill. Releases: rule://release.
