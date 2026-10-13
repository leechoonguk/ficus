# ficus — Project Guide

As-you-type completion menu for zsh, bash and PowerShell, drawn in the terminal
(README.md has the user-facing description). Go module `ficus`; carapace-bin and fzf
are git submodules under `third_party/` (do not edit them).

## Layout

```
cmd/ficus/            CLI: init, complete, accept, pick, fill, setup, doctor, update
cmd/ficus-carapace/   unmodified carapace-bin build (the argument backend)
internal/engine/      parse the line, fetch/cache candidates, rank, render
internal/config/      settings: defaults < config.toml < FICUS_* env
internal/setup/       startup-file blocks (`ficus setup`)
internal/termq/       terminal background query (theme = auto)
shell/                ficus.zsh, ficus.bash, ficus.ps1 (embedded by `ficus init`)
install.sh install.ps1    user installers (download a release, run `ficus setup`)
.goreleaser.yaml      what a release builds and publishes
scripts/release.sh    `make release` / `make unrelease`
scripts/go.sh         the go command `make` builds with (fetches the go.mod version if needed)
.github/workflows/    ci (tests on 3 OSes), release (tag → GoReleaser), verify-release
Makefile              every command; `make help`; table in rule://root-makefile
.omp/                 harness: hooks (pre blocks, post checks), rules, skills
```

## Commands

`make build`, `make test`, `make verify` (the gate). `make install` never builds: it
installs `bin/` after `make build`, else the latest release, so install your changes
with `make build && make install`. Publishing only when asked: `make commit` /
`make push` (rule://root-git-workflow), `make release bump=…` (rule://release).

## Versions and releases

The annotated tag `vX.Y.Z` is the version; nothing else stores it. `make release
bump=patch|minor|major` checks the commit (main, clean, pushed, CI green), tags it,
waits for GoReleaser, checks the uploaded files and installs the release on Ubuntu,
macOS and Windows. Procedure and failure handling: the `release` skill.

## Testing changes to the shell integrations

Unit tests do not cover drawing. Build, then drive a real shell in a throwaway tmux
server with a throwaway `ZDOTDIR`/rcfile — never the user's own startup files:
`tmux -L test new -d …`, `tmux send-keys`, `tmux capture-pane -p`.
