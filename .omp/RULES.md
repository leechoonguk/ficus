# Sticky Rules

## Publishing

Never commit, push, tag or release unless the user explicitly asks in the current
turn. This is mechanical: `strict-publish-consent.ts` reads the session's own entries
and blocks the command, `strict-verified-git.ts` runs `make verify` before any
commit/push, and `strict-release-route.ts` refuses raw `git tag`, tag pushes and
`gh release` writes. Publish with `make commit m="…"` / `make push` (the `commit`
skill); release with `make release bump=patch|minor|major` (the `release` skill).

## Harness Integrity

Never modify `.omp/` files without explicit user request. When modifying the harness
(`.omp/` files, hooks, rules), read the OMP documentation (`omp://`) first.

## Workflow

- Documentation and code comments are English (rule://english; `check-english` in
  `make verify`). A proper noun that must keep its script goes on that rule's
  allowed list.

- Run `make verify` from the project root before declaring work done. Never truncate
  gate output with `head`/`tail`/`grep` — a pipe returns the filter's exit code.
- An additive task must not wipe existing code: targeted edits over full-file
  rewrites, never truncate a file with `>`.
