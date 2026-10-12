# ficus build. Requires Go (see go.mod) and initialized submodules.
#
#   make            build bin/ficus and bin/ficus-carapace for this machine
#   make test       run the tests
#   make dist       release archives and packages for every platform in dist/
#                   (needs GoReleaser; a v* tag push publishes them)
#   make install    copy both binaries to $(PREFIX)/bin and enable ficus in
#                   the startup file of every supported shell found here
#                   (SHELLS="zsh bash pwsh" to choose; see `ficus setup`)
#   make uninstall  undo install: binaries, startup-file blocks, cache

PREFIX   ?= $(HOME)/.local
SHELLS   ?=
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo develop)
CARAPACE := third_party/carapace-bin

# Static binaries run on any glibc/musl host (SSH targets); "release" selects
# carapace's release completer set, as its own releases do.
export CGO_ENABLED := 0
TAGS    := -tags release
LDFLAGS := -ldflags '-s -w -X main.version=$(VERSION)'

# carapace-bin generates its completer registry; the generated files are
# ignored by its .gitignore and must exist before it compiles.
GENERATED := $(CARAPACE)/pkg/actions/actions_generated.go

.PHONY: all build test dist install uninstall generate clean

all: build

SUBMODULES := $(CARAPACE)/go.mod third_party/fzf/go.mod

$(SUBMODULES):
	git submodule update --init --depth 1

$(GENERATED): $(SUBMODULES)
	cd $(CARAPACE) && go generate ./cmd/...

generate: $(GENERATED)

build: $(GENERATED)
	go build $(TAGS) $(LDFLAGS) -o bin/ficus ./cmd/ficus
	go build $(TAGS) $(LDFLAGS) -o bin/ficus-carapace ./cmd/ficus-carapace

test: $(GENERATED)
	go test $(TAGS) ./cmd/... ./internal/... ./shell/...

# Release files for every platform in dist/, as a tag push publishes them
# (.goreleaser.yaml), without publishing.
dist:
	goreleaser release --snapshot --clean

install: build
	install -d $(PREFIX)/bin
	install -m 755 bin/ficus bin/ficus-carapace $(PREFIX)/bin/
	$(PREFIX)/bin/ficus setup $(if $(SHELLS),--shells $(shell echo $(SHELLS) | tr ' ' ','))

# The cache lives where Go's os.UserCacheDir points, unless FICUS_CACHE_DIR.
CACHE := $(or $(FICUS_CACHE_DIR),$(if $(filter Darwin,$(shell uname -s)),$(HOME)/Library/Caches/ficus,$(or $(XDG_CACHE_HOME),$(HOME)/.cache)/ficus))

# Only ficus's own files: FICUS_CACHE_DIR may point at a shared directory.
uninstall:
	@if [ -x $(PREFIX)/bin/ficus ]; then $(PREFIX)/bin/ficus setup --remove; \
	elif [ -x bin/ficus ]; then bin/ficus setup --remove; \
	else echo "ficus: no ficus binary to remove the startup-file blocks with (make build first)" >&2; exit 1; fi
	rm -f $(PREFIX)/bin/ficus $(PREFIX)/bin/ficus-carapace
	rm -f '$(CACHE)'/*.json '$(CACHE)'/*.lock '$(CACHE)'/.tmp-* '$(CACHE)'/.pruned
	-rmdir '$(CACHE)' 2>/dev/null

clean:
	rm -rf bin dist
