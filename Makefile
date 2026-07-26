BINARY  := kirobuff
PKG     := ./cmd/kirobuff
BIN_DIR := bin
PREFIX  ?= $(HOME)/.local/bin
MODULE  := github.com/AlleyBo55/KiroBuff

# Version comes from the nearest git tag, so a plain `make build` in a checkout
# reports something meaningful instead of "dev".
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
# Must match the package that declares Version, Commit and Date. Go silently
# ignores -X for an unknown symbol, so a stale path here produces binaries
# that report "dev" with no error anywhere. TestLdflagsTargetsARealPackage
# guards it.
VPKG    := $(MODULE)/semver
LDFLAGS := -s -w \
	-X '$(VPKG).Version=$(VERSION)' \
	-X '$(VPKG).Commit=$(COMMIT)' \
	-X '$(VPKG).Date=$(DATE)' 

.PHONY: all build test check cover lint fmt vet install uninstall clean

all: check build

build:
	@mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(PKG)
	@echo "built $(BIN_DIR)/$(BINARY)"

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

# Fails on unformatted files rather than silently rewriting them, so CI catches
# what a local `make fmt` would have fixed.
check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed:"; echo "$$unformatted"; exit 1; \
	fi
	go vet ./...
	go test -race ./...

# Coverage is measured on the library packages only, and cmd/ is excluded on
# purpose. cmd/ is argument parsing and output formatting: thin glue where a
# test asserts that a printf still prints. Including it produced a 53% total
# that said nothing about whether the logic works, while the packages holding
# the logic sit at 87%.
#
# Raise the floor as coverage improves. Never lower it to make a build pass -
# that is the same move as deleting a failing test, which this project blocks
# at the tool level.
COVERAGE_FLOOR ?= 85
COVER_PKGS     := $(shell go list ./... | grep -v '/cmd/')

cover:
	go test -coverprofile=coverage.out $(COVER_PKGS)
	@go tool cover -func=coverage.out | tail -1
	@total=$$(go tool cover -func=coverage.out | tail -1 | awk '{print $$3}' | tr -d '%'); \
	awk -v t="$$total" -v f="$(COVERAGE_FLOOR)" 'BEGIN { \
		if (t+0 < f+0) { printf "library coverage %.1f%% is below the %s%% floor\n", t, f; exit 1 } \
		else { printf "library coverage %.1f%% meets the %s%% floor\n", t, f } }'

lint:
	@command -v golangci-lint >/dev/null || { \
		echo "golangci-lint not installed:"; \
		echo "  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"; \
		exit 1; }
	golangci-lint run

install: build
	@mkdir -p $(PREFIX)
	cp $(BIN_DIR)/$(BINARY) $(PREFIX)/$(BINARY)
	@echo "installed $(PREFIX)/$(BINARY)"
	@case ":$$PATH:" in \
		*":$(PREFIX):"*) ;; \
		*) echo ""; \
		   echo "WARNING: $(PREFIX) is not on your PATH."; \
		   echo "The statusline and guard hooks re-invoke $(BINARY) by name,"; \
		   echo "so they will not run until it is. Add this to your shell profile:"; \
		   echo ""; \
		   echo "  export PATH=\"$(PREFIX):\$$PATH\"";; \
	esac

uninstall:
	rm -f $(PREFIX)/$(BINARY)
	@echo "removed $(PREFIX)/$(BINARY)"
	@echo "note: config written by 'kirobuff install' is left in place"

clean:
	rm -rf $(BIN_DIR)
