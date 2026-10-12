---
description: Makefile is the command index — every target has ## help and a row here, every post check is a verify prerequisite, harness docs only cite real targets
globs:
  - Makefile
  - .omp/**/*.md
alwaysApply: false
---

# Makefile

Every command an agent or a human runs is a target. Machine-checked by
`check-makefiles` (target table both ways, `.PHONY`, `## help`, verify membership,
`make <target>` references in `.omp/`).

| Target | What it does |
|---|---|
| `make all` | Same as `make build` (the default target) |
| `make help` | List targets with their `##` descriptions |
| `make generate` | Generate carapace's completer registry (every Go build needs it) |
| `make build` | `bin/ficus`, `bin/ficus-carapace` for this machine, version from `git describe` |
| `make test` | Go tests: engine, config, setup, termq, shell integrations |
| `make lint` | gofmt (no unformatted file) + `go vet` |
| `make verify` | The gate: check-makefiles + check-omp-hooks + check-omp-rules + check-omp-skills + check-release + check-english + lint + test |
| `make dist` | Every release file into `dist/` without publishing (GoReleaser snapshot) |
| `make install` | Copy both binaries to `$(PREFIX)/bin` (default `~/.local/bin`) and run `ficus setup` (`SHELLS="zsh bash"`) |
| `make uninstall` | `ficus setup --remove`, delete the binaries and the candidate cache |
| `make clean` | Remove `bin/` and `dist/` |
| `make check-makefiles` | This table, `.PHONY`, verify membership |
| `make check-omp-hooks` | Hook shapes, imports, `disabledExtensions` (rule://omp-conventions) |
| `make check-omp-rules` | Rule frontmatter and check→rule mapping |
| `make check-omp-skills` | Skill frontmatter and `enforced-by` |
| `make check-release` | Release config, release script, installers, workflows agree (rule://release) |
| `make check-english` | Documentation and code comments are English (rule://english) |
| `make hook-report` | What the post hooks cost and which never warned |
| `make commit` | `verify`, then commit the whole worktree (`m=` or `f=`) — only when asked |
| `make push` | `verify`, then push the current branch — only when asked |
| `make release` | `verify`, then tag `bump=patch\|minor\|major`, publish, check the files, install on 3 OSes (`DRY_RUN=1` to only check) — only when asked |
| `make unrelease` | Delete a published release and its tag (`v=vX.Y.Z`) — only when asked |

## Conventions

- A new command is a target with a `## help` line, a `.PHONY` entry and a row above.
- A new post check `.omp/hooks/post/check-<x>.ts` gets `check-<x>: ## …` running
  `node .omp/hooks/post/check-<x>.ts`, joins `verify`'s prerequisites and the
  `make verify` row, and is listed in `.omp/config.yml` `disabledExtensions`.
- Never pipe a gate (`make verify | tail`): the pipe's exit code replaces the gate's.
