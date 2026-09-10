.PHONY: build typecheck install vet dist dist-check test test-capped

GOMAXPROCS      ?= 4
GOBUILD_P       ?= 4
GOTEST_P        ?= 2
GOTEST_PARALLEL ?= 2
GOTEST_MEM_HIGH ?= 8G
GOTEST_MEM_MAX  ?= 10G
GOTEST_ARGS     ?=
INSTALL_DIR     ?= $(HOME)/.local/bin

# GOTEST_ARGS reaches the recipe shell via the environment, never as shell text.
export GOTEST_ARGS

# See doc/memory-guards.md — CAPPED rationale, GOMAXPROCS, ManagedOOMPreference=avoid, -count=1.
# Set HERDR_MSB_ALLOW_UNCAPPED=1 in CI (no user systemd instance). Fails closed otherwise.
define CAPPED
	@set -e; \
	if [ -n "$$HERDR_MSB_ALLOW_UNCAPPED" ]; then \
		echo "make: HERDR_MSB_ALLOW_UNCAPPED set — running WITHOUT a memory cap." >&2; \
		exec choom -n 1000 -- env GOMAXPROCS=$(GOMAXPROCS) $(1); \
	fi; \
	exec systemd-run --user --scope -q \
		-p MemoryHigh=$(GOTEST_MEM_HIGH) -p MemoryMax=$(GOTEST_MEM_MAX) \
		-p ManagedOOMPreference=avoid \
		-- choom -n 1000 -- env GOMAXPROCS=$(GOMAXPROCS) $(1)
endef

build:
	$(call CAPPED,go build -p $(GOBUILD_P) -o herdr-plugin-msb ./cmd/herdr-plugin-msb)
	$(call CAPPED,go build -p $(GOBUILD_P) -o herdr-plugin-msb-agent ./cmd/herdr-plugin-msb-agent)

typecheck:
	$(call CAPPED,go build -p $(GOBUILD_P) ./...)

install: build
	@mkdir -p $(INSTALL_DIR)
	cp herdr-plugin-msb $(INSTALL_DIR)/herdr-plugin-msb.new
	mv -f $(INSTALL_DIR)/herdr-plugin-msb.new $(INSTALL_DIR)/herdr-plugin-msb
	cp herdr-plugin-msb-agent $(INSTALL_DIR)/herdr-plugin-msb-agent.new
	mv -f $(INSTALL_DIR)/herdr-plugin-msb-agent.new $(INSTALL_DIR)/herdr-plugin-msb-agent
	@echo "OK: installed herdr-plugin-msb and herdr-plugin-msb-agent → $(INSTALL_DIR)"

dist:
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -o dist/herdr-plugin-msb-agent-darwin-arm64 ./cmd/herdr-plugin-msb-agent
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -o dist/herdr-plugin-msb-agent-darwin-amd64 ./cmd/herdr-plugin-msb-agent
	go build -trimpath -o dist/herdr-plugin-msb-linux-amd64 ./cmd/herdr-plugin-msb
	go run ./tools/distcheck . --write

dist-check:
	go run ./tools/distcheck .

vet:
	go vet -p $(GOBUILD_P) ./...
	go run ./tools/importban .
	go run ./tools/distcheck .

test:
	@scripts/test-session.sh $(MAKE) test-capped

test-capped:
	$(call CAPPED,go test -race -p $(GOTEST_P) -parallel $(GOTEST_PARALLEL) -count=1 $$GOTEST_ARGS ./...)
