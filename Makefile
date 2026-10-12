# ficus. Requires Go (see go.mod) and initialized submodules; `make help` lists
# every target. The command index with notes is .omp/rules/root-makefile.md.

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

# carapace-bin generates its completer registry; the generated files are
# ignored by its .gitignore and must exist before it compiles.
GENERATED  := $(CARAPACE)/pkg/actions/actions_generated.go
SUBMODULES := $(CARAPACE)/go.mod third_party/fzf/go.mod

.PHONY: all help build generate test lint verify dist install uninstall clean \
	check-makefiles check-omp-hooks check-omp-rules check-omp-skills check-release hook-report \
	commit push release unrelease

all: build ## Build bin/ficus and bin/ficus-carapace (same as make build)

help: ## List targets with their descriptions
	@awk -F':.*## ' '/^[a-z][a-z-]*:.*## / { printf "  %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

$(SUBMODULES):
	git submodule update --init --depth 1

$(GENERATED): $(SUBMODULES)
	cd $(CARAPACE) && go generate ./cmd/...

generate: $(GENERATED) ## Generate carapace's completer registry (needed before any Go build)

build: $(GENERATED) ## Build bin/ficus and bin/ficus-carapace for this machine
	go build $(TAGS) $(LDFLAGS) -o bin/ficus ./cmd/ficus
	go build $(TAGS) $(LDFLAGS) -o bin/ficus-carapace ./cmd/ficus-carapace

test: $(GENERATED) ## Run the Go tests (engine, config, setup, shell integrations)
	go test $(TAGS) ./cmd/... ./internal/... ./shell/...

lint: $(GENERATED) ## gofmt (no unformatted file) and go vet
	@unformatted=$$(gofmt -l $(GO_DIRS)); if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	go vet $(TAGS) ./cmd/... ./internal/... ./shell/...

# Harness checks first: they read the tree in milliseconds, so a wiring slip is
# reported before the Go build and tests run.
verify: check-makefiles check-omp-hooks check-omp-rules check-omp-skills check-release lint test ## The gate: harness checks, lint, tests (fail-fast)

# Release files for every platform in dist/, as a tag push publishes them
# (.goreleaser.yaml), without publishing.
dist: ## Build every release file into dist/ without publishing (needs GoReleaser)
	goreleaser release --snapshot --clean

install: build ## Copy both binaries to $(PREFIX)/bin and run ficus setup (SHELLS="zsh bash" to choose)
	install -d $(PREFIX)/bin
	install -m 755 bin/ficus bin/ficus-carapace $(PREFIX)/bin/
	$(PREFIX)/bin/ficus setup $(if $(SHELLS),--shells $(shell echo $(SHELLS) | tr ' ' ','))

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
