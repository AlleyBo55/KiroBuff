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
VPKG    := $(MODULE)/internal/version
LDFLAGS := -s -w \
	-X '$(VPKG).Version=$(VERSION)' \
	-X '$(VPKG).Commit=$(COMMIT)' \
	-X '$(VPKG).Date=$(DATE)' 

.PHONY: all build test check fmt vet install uninstall clean

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
	go test ./...

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
