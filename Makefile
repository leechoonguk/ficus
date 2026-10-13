# ficus. Requires git and make; Go is found or fetched by scripts/go.sh. `make
# help` lists every target. The command index with notes is .omp/rules/root-makefile.md.

PREFIX   ?= $(HOME)/.local
SHELLS   ?=
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo develop)
CARAPACE := third_party/carapace-bin

# Static binaries run on any glibc/musl host (SSH targets); "release" selects
# carapace's release completer set, as its own releases do.
export CGO_ENABLED := 0
TAGS    := -tags release
LDFLAGS := -ldflags '-s -w -X main.version=$(VERSION)'
GO_DIRS := cmd internal shell

# The go command, resolved on first use (targets that build nothing never
# fetch Go): Go 1.21+ from PATH, else go.mod's version downloaded into .cache/.
# GOTOOLCHAIN=auto lets an older Go 1.21+ switch to the go.mod version itself;
# distributions that ship GOTOOLCHAIN=local in go.env (Fedora, Alpine) would
# refuse. A GOTOOLCHAIN set in the environment or on the command line wins.
GO = $(eval GO := $$(or $$(shell scripts/go.sh),$$(error no Go to build with: see the message above)))$(GO)
ifeq ($(origin GOTOOLCHAIN),undefined)
export GOTOOLCHAIN := auto
endif

# carapace-bin generates its completer registry; the generated files are
# ignored by its .gitignore and must exist before it compiles.
GENERATED  := $(CARAPACE)/pkg/actions/actions_generated.go
SUBMODULES := $(CARAPACE)/go.mod third_party/fzf/go.mod

.PHONY: all help build generate test lint verify dist demo install uninstall clean \
	check-makefiles check-omp-hooks check-omp-rules check-omp-skills check-release check-english hook-report \
	commit push release unrelease

all: build ## Build bin/ficus and bin/ficus-carapace (same as make build)

help: ## List targets with their descriptions
	@awk -F':.*## ' '/^[a-z][a-z-]*:.*## / { printf "  %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# fzf-tab is reference only and not needed to build.
$(SUBMODULES):
	git submodule update --init --depth 1 $(CARAPACE) third_party/fzf

$(GENERATED): $(SUBMODULES)
	cd $(CARAPACE) && $(GO) generate ./cmd/...

generate: $(GENERATED) ## Generate carapace's completer registry (needed before any Go build)

build: $(GENERATED) ## Build bin/ficus and bin/ficus-carapace for this machine
	$(GO) build $(TAGS) $(LDFLAGS) -o bin/ficus ./cmd/ficus
	$(GO) build $(TAGS) $(LDFLAGS) -o bin/ficus-carapace ./cmd/ficus-carapace

test: $(GENERATED) ## Run the Go tests (engine, config, setup, shell integrations)
	$(GO) test $(TAGS) ./cmd/... ./internal/... ./shell/...

lint: $(GENERATED) ## gofmt (no unformatted file) and go vet
	@unformatted=$$("$$($(GO) env GOROOT)/bin/gofmt" -l $(GO_DIRS)); if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	$(GO) vet $(TAGS) ./cmd/... ./internal/... ./shell/...

# Harness checks first: they read the tree in milliseconds, so a wiring slip is
# reported before the Go build and tests run.
verify: check-makefiles check-omp-hooks check-omp-rules check-omp-skills check-release check-english lint test ## The gate: harness checks, lint, tests (fail-fast)

# Release files for every platform in dist/, as a tag push publishes them
# (.goreleaser.yaml), without publishing.
dist: ## Build every release file into dist/ without publishing (needs GoReleaser)
	goreleaser release --snapshot --clean

# The README's recording: a scripted zsh session (docs/demo.tape) in a throwaway
# repository under /tmp (docs/demo-setup.sh), never the user's own startup files.
demo: build ## Re-record docs/demo.gif from docs/demo.tape (needs vhs: brew install vhs)
	@command -v vhs >/dev/null 2>&1 || { echo 'make demo needs vhs: brew install vhs' >&2; exit 1; }
	docs/demo-setup.sh "$(CURDIR)/bin/ficus"
	vhs docs/demo.tape

# Install never builds: `make build && make install` installs your build from
# bin/, a plain `make install` the latest release (install.sh, curl only; v=
# pins a tag). Either way it says which. As install.sh, with no supported shell
# installed yet (a bare Alpine has only ash) the binaries stay installed and
# setup is left for later; named SHELLS must succeed.
install: ## Install bin/ from make build, else the latest release (v=vX.Y.Z), into $(PREFIX)/bin; then ficus setup (SHELLS="zsh bash")
	@if [ -x bin/ficus ] && [ -x bin/ficus-carapace ]; then \
		echo "ficus: installing the build in bin/ ($$(bin/ficus version)) into $(PREFIX)/bin"; \
		install -d $(PREFIX)/bin && install -m 755 bin/ficus bin/ficus-carapace $(PREFIX)/bin/ || exit 1; \
		$(if $(SHELLS),$(PREFIX)/bin/ficus setup --shells $(shell echo $(SHELLS) | tr ' ' ','),$(PREFIX)/bin/ficus setup || echo "ficus: run '$(PREFIX)/bin/ficus setup' once zsh 5.9+ or bash 4+ or PowerShell is installed" >&2); \
	else \
		echo "ficus: nothing built in bin/; installing the $(or $(v),latest) release (make build first to install your own)"; \
		FICUS_INSTALL_DIR='$(PREFIX)/bin' FICUS_VERSION='$(v)' FICUS_SHELLS='$(shell echo $(SHELLS) | tr ' ' ',')' sh install.sh; \
	fi

# The cache lives where Go's os.UserCacheDir points, unless FICUS_CACHE_DIR.
CACHE := $(or $(FICUS_CACHE_DIR),$(if $(filter Darwin,$(shell uname -s)),$(HOME)/Library/Caches/ficus,$(or $(XDG_CACHE_HOME),$(HOME)/.cache)/ficus))

# Only ficus's own files: FICUS_CACHE_DIR may point at a shared directory.
uninstall: ## Undo install: startup-file blocks, binaries, cache
	@if [ -x $(PREFIX)/bin/ficus ]; then $(PREFIX)/bin/ficus setup --remove; \
	elif [ -x bin/ficus ]; then bin/ficus setup --remove; \
	else echo "ficus: no ficus binary to remove the startup-file blocks with (make build first)" >&2; exit 1; fi
	rm -f $(PREFIX)/bin/ficus $(PREFIX)/bin/ficus-carapace
	rm -f '$(CACHE)'/*.json '$(CACHE)'/*.lock '$(CACHE)'/.tmp-* '$(CACHE)'/.pruned
	-rmdir '$(CACHE)' 2>/dev/null

clean: ## Remove bin/ and dist/
	rm -rf bin dist

# ── Harness checks (.omp/hooks/post) ─────────────────────────────────────────

check-makefiles: ## Check Makefile targets, .PHONY, verify membership and root-makefile.md agree
	node .omp/hooks/post/check-makefiles.ts

check-omp-hooks: ## Check hook shapes, relative imports and config.yml disabledExtensions
	node .omp/hooks/post/check-omp-hooks.ts

check-omp-rules: ## Check rule frontmatter, delivery channels and the check→rule mapping
	node .omp/hooks/post/check-omp-rules.ts

check-omp-skills: ## Check skills are discoverable and cite real hooks
	node .omp/hooks/post/check-omp-skills.ts

check-release: ## Check release config, release script, installers and workflows name the same files
	node .omp/hooks/post/check-release.ts

check-english: ## Check documentation and code comments are English (proper nouns: rule://english)
	node .omp/hooks/post/check-english.ts

hook-report: ## Report what the post hooks cost and which never warned
	node .omp/hooks/lib/hook-metrics.ts

# ── Publish (only when the user asks — strict-publish-consent) ───────────────

commit: verify ## Commit the verified worktree (usage: make commit m="type(scope): subject" | make commit f=path/to/message)
	@test -n '$(m)$(f)' || { echo 'usage: make commit m="type(scope): subject" | make commit f=<message file>' >&2; exit 2; }
	@test -z '$(m)' -o -z '$(f)' || { echo 'usage: pass m= or f=, not both' >&2; exit 2; }
	@test -z '$(f)' -o -f '$(f)' || { echo 'no such message file: $(f)' >&2; exit 2; }
	@git add -A
	@$(if $(f),git commit -q -F '$(f)',git commit -q -m '$(m)')
	@git --no-pager log --oneline -1

push: verify ## Push the current branch after the gate passes
	@git push origin "$$(git rev-parse --abbrev-ref HEAD)"
	@git --no-pager log --oneline -1 "origin/$$(git rev-parse --abbrev-ref HEAD)"

release: verify ## Tag the next version and publish it (usage: make release bump=patch|minor|major [DRY_RUN=1])
	@scripts/release.sh '$(bump)'

unrelease: ## Delete a published release and its tag (usage: make unrelease v=vX.Y.Z)
	@scripts/release.sh --unrelease '$(v)'
