BINARY := mga
BIN_DIR := bin
PKG := ./cmd/mga
ARGS ?=
PREFIX ?= $(HOME)/.local
INSTALL_DIR ?= $(PREFIX)/bin
INSTALL_PATH := $(INSTALL_DIR)/$(BINARY)

.DEFAULT_GOAL := build

.PHONY: help build run dev test test-race vet fmt lint check tidy install uninstall clean

help: ## Show the targets
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

build: ## Build bin/mga
	go build -o $(BIN_DIR)/$(BINARY) $(PKG)

run: build ## Build and run mga; pass flags with ARGS="--model ollama:qwen3"
	./$(BIN_DIR)/$(BINARY) $(ARGS)

dev: ## Run from source without a build step; pass flags with ARGS
	go run $(PKG) $(ARGS)

test: ## Run the tests
	go test ./...

test-race: ## Run the tests with the race detector
	go test -race -count=1 ./...

vet: ## Run go vet
	go vet ./...

fmt: ## Format the code
	gofmt -w .

lint: ## Fail when a file is not formatted or vet reports a problem
	@test -z "$$(gofmt -l .)" || (echo "Files need gofmt:"; gofmt -l .; exit 1)
	go vet ./...

check: lint test-race ## Run lint and the race tests

tidy: ## Tidy go.mod and go.sum
	go mod tidy

install: test ## Test, build, and install mga to ~/.local/bin (no sudo); change it with PREFIX=... or INSTALL_DIR=...
	@set -e; \
	go build -trimpath -ldflags "-s -w" -o $(BIN_DIR)/$(BINARY) $(PKG); \
	if ! ./$(BIN_DIR)/$(BINARY) --help >/dev/null 2>&1; then \
		echo "The new binary does not run. Nothing was installed."; exit 1; \
	fi; \
	mkdir -p "$(INSTALL_DIR)"; \
	if [ ! -w "$(INSTALL_DIR)" ]; then \
		echo "You cannot write to $(INSTALL_DIR). Use a directory you own, e.g. make install PREFIX=$$HOME/.local"; exit 1; \
	fi; \
	if [ -L "$(INSTALL_PATH)" ] || { [ -e "$(INSTALL_PATH)" ] && [ ! -f "$(INSTALL_PATH)" ]; }; then \
		echo "$(INSTALL_PATH) exists and is not a regular file. Remove it yourself, then run make install again."; exit 1; \
	fi; \
	if [ -f "$(INSTALL_PATH)" ]; then \
		cp -p "$(INSTALL_PATH)" "$(INSTALL_PATH).bak"; \
		echo "Backed up the old binary to $(INSTALL_PATH).bak"; \
	fi; \
	install -m 0755 $(BIN_DIR)/$(BINARY) "$(INSTALL_PATH).tmp.$$$$"; \
	mv -f "$(INSTALL_PATH).tmp.$$$$" "$(INSTALL_PATH)"; \
	"$(INSTALL_PATH)" --help >/dev/null 2>&1; \
	echo "Installed $(INSTALL_PATH)"; \
	case ":$$PATH:" in \
		*":$(INSTALL_DIR):"*) ;; \
		*) echo "Note: $(INSTALL_DIR) is not on your PATH. Add this line to ~/.zshrc or ~/.bashrc, then open a new terminal:"; \
		   echo "  export PATH=\"$(INSTALL_DIR):\$$PATH\"";; \
	esac; \
	found="$$(command -v $(BINARY) || true)"; \
	if [ -n "$$found" ] && [ "$$found" != "$(INSTALL_PATH)" ]; then \
		echo "Warning: '$(BINARY)' on your PATH is $$found, not $(INSTALL_PATH). Fix the PATH order to use the new one."; \
	fi

uninstall: ## Remove the installed mga (keeps ~/.mga config, keys, and sessions)
	@if [ -f "$(INSTALL_PATH)" ]; then rm -f "$(INSTALL_PATH)"; echo "Removed $(INSTALL_PATH)"; else echo "Nothing to remove at $(INSTALL_PATH)"; fi
	@if [ -f "$(INSTALL_PATH).bak" ]; then echo "A backup is still at $(INSTALL_PATH).bak"; fi

clean: ## Remove build output
	rm -rf $(BIN_DIR)
