---
description: Harness conventions — post checks are pure check() functions on the shared scaffold, pre hooks block at tool_call, every check names a rule, skills cite real hooks
globs:
  - .omp/**
alwaysApply: false
---

# Harness Conventions

Ported from `~/Workspace/RL/.omp` (itself from fast-api-standard). Machine-checked by
`check-omp-hooks`, `check-omp-rules`, `check-omp-skills`.

## Hooks

| Kind | Shape | Runs |
|---|---|---|
| Pre (`.omp/hooks/pre/strict-<name>.ts`) | `export default function (pi)` handling `"tool_call"`, returning `{ block, reason }` | live, every tool call — blocking cannot be done after the fact |
| Post (`.omp/hooks/post/check-<name>.ts`) | `check(): string[]` + `registerPostCheck(pi, spec)` + trailing `runCheckCli(spec)`; `spec.name` = file name | `make check-<name>` and `make verify`; per-call run disabled in `config.yml` |
| Lib (`.omp/hooks/lib/<name>.ts`) | shared logic, imported by at least one hook | — |

- `registerPostCheck` runs the check inside `guarded()`: a crash becomes a visible
  notice and a line in `.omp/state/hook-crash.log`, never a silent dead check.
- A hook never throws on a missing file — it reports it as an issue (fail-open
  for pre hooks, issue for post checks).
- Hooks are TypeScript run by node (24+) directly; no package.json under `.omp/`.

## Rules

- Frontmatter with `description` and one channel: `globs` (rulebook, body read via
  `rule://<name>`), `alwaysApply: true` (max 2), or `condition` (TTSR).
- Every post check maps to a rule: `check-<x>` → `<x>.md`, or an entry in
  `RULE_NAME_EXCEPTIONS` (hook-rule-hint.ts). A failing check prints that rule.

## Skills

`.omp/skills/<name>/SKILL.md` with frontmatter `name: <name>`, `description`, and
`enforced-by: [hooks]` naming hooks that exist.
