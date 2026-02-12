# MClaw Build System
# ==================

APP_NAME    := mclaw
MODULE      := github.com/ntminh611/mclaw
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_DIR   := dist
CMD_DIR     := ./cmd/mclaw

# Build flags
LDFLAGS     := -s -w -X main.version=$(VERSION)
GO          := go
GOFLAGS     :=

# Platforms
PLATFORMS   := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 android/arm64

# ─── Targets ───────────────────────────────────────────────

.PHONY: build run test clean dist all help

## build: Build for current platform
build:
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(APP_NAME) $(CMD_DIR)
	@echo "✓ Built $(APP_NAME)"

## run: Build and run
run: build
	./$(APP_NAME) start

## test: Run all tests
test:
	$(GO) test ./... -v

## clean: Remove build artifacts
clean:
	rm -f $(APP_NAME)
	rm -rf $(BUILD_DIR)
	@echo "✓ Cleaned"

## dist: Cross-compile for all platforms
dist: clean
	@mkdir -p $(BUILD_DIR)
	@for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*} GOARCH=$${platform#*/} \
		$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" \
			-o $(BUILD_DIR)/$(APP_NAME)-$${platform%/*}-$${platform#*/}$$([ "$${platform%/*}" = "windows" ] && echo ".exe") \
			$(CMD_DIR); \
		echo "  ✓ $(APP_NAME)-$${platform%/*}-$${platform#*/}"; \
	done
	@cp config.example.json $(BUILD_DIR)/config.json
	@echo "✓ All platforms built → $(BUILD_DIR)/"

## help: Show this help
help:
	@echo "MClaw Build Targets:"
	@echo ""
	@grep -E '^## ' Makefile | sed 's/## /  /' | column -t -s ':'
	@echo ""
	@echo "Examples:"
	@echo "  make build          Build for current OS"
	@echo "  make dist           Cross-compile all platforms"
	@echo "  make run            Build & start server"

.DEFAULT_GOAL := build
