---
name: commit
description: Create and push Git commits through the verified repository workflow. Use when the user asks to commit, push, publish changes, or prepare a verified commit.
enforced-by: [strict-publish-consent, strict-verified-git]
---

# Verified Commit and Push

## Only When Asked

A publish needs two facts: the tree is green, and the user asked in the current
turn. `strict-publish-consent` blocks a commit or push the current user turn does
not request — including a turn that names the verb while forbidding it. If the
work is done and nobody asked, report it and stop. Rules: rule://root-git-workflow.

## Workflow

1. Inspect the whole worktree (`git status`) and separate unrelated user changes
   before publishing.
2. Publish through the Make targets — `verify` is a prerequisite of both, so the
   published tree is the tree the gate read:

```bash
make commit m="fix(engine): keep the ellipsis inside the column"   # one-line message
make commit f=/tmp/commit-message                                  # message with a body
make push
```

3. `make commit` stages everything (`git add -A`). To commit less, `git stash -k`
   the rest first and run the target again.
4. The subject becomes a release-note line (rule://release): `feat:`, `fix:`,
   `docs:`, `test:`, `ci:`, `chore:` and a subject a user understands.
5. Confirm with `git --no-pager log --oneline -1`. After `make push`, CI runs on
   Ubuntu, macOS and Windows; a release needs that run green.

## When The Gate Disagrees

```bash
make verify; status=$?; printf 'MAKE_VERIFY_EXIT=%s\n' "$status"
```

`MAKE_VERIFY_EXIT=0` is the only pass condition. Never pipe the gate.
