# Load .env file if it exists
ifneq (,$(wildcard ./.env))
    include .env
    export
endif

# Build variables
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -w -s \
           -X github.com/ethpandaops/lab-backend/internal/version.Version=$(VERSION) \
           -X github.com/ethpandaops/lab-backend/internal/version.GitCommit=$(GIT_COMMIT) \
           -X github.com/ethpandaops/lab-backend/internal/version.BuildDate=$(BUILD_DATE)

# Colors for output (use printf for cross-platform compatibility)
CYAN := \033[0;36m
GREEN := \033[0;32m
YELLOW := \033[0;33m
RED := \033[0;31m
RESET := \033[0m

.PHONY: all build setup-frontend clean test run redis stop-redis help

all: build

# Frontend configuration
FRONTEND_SOURCE ?=
FRONTEND_BRANCH ?=
FRONTEND_TARGET ?= web/frontend
FRONTEND_VERSION_FILE ?= .tmp/frontend-version.txt
GITHUB_REPO ?= ethpandaops/lab

## build: Setup frontend and build the lab-backend binary
build: setup-frontend
	@printf "$(CYAN)==> Building lab-backend...$(RESET)\n"
	@mkdir -p bin
	@go build -ldflags "$(LDFLAGS)" -o bin/lab-backend ./cmd/server
	@printf "$(GREEN)✓ Build complete!$(RESET)\n"
	@printf "$(GREEN)  Backend binary: bin/lab-backend$(RESET)\n"

## setup-frontend: Setup frontend from source/release
.PHONY: setup-frontend
setup-frontend:
	@printf "$(CYAN)==> Setting up frontend...$(RESET)\n"
	@mkdir -p .tmp
	@# Check if FRONTEND_SOURCE is set (local copy mode)
	@if [ -n "$(FRONTEND_SOURCE)" ]; then \
		printf "$(YELLOW)Using local frontend source: $(FRONTEND_SOURCE)$(RESET)\n"; \
		if [ ! -d "$(FRONTEND_SOURCE)/dist" ]; then \
			printf "$(RED)Error: $(FRONTEND_SOURCE)/dist does not exist$(RESET)\n"; \
			exit 1; \
		fi; \
		rm -rf $(FRONTEND_TARGET); \
		mkdir -p $(FRONTEND_TARGET); \
		cp -r $(FRONTEND_SOURCE)/dist/* $(FRONTEND_TARGET)/; \
		printf "$(GREEN)✓ Copied $(FRONTEND_SOURCE)/dist -> $(FRONTEND_TARGET)$(RESET)\n"; \
	else \
		if [ -n "$(FRONTEND_BRANCH)" ]; then \
			printf "$(YELLOW)Frontend branch: $(FRONTEND_BRANCH)$(RESET)\n"; \
			RELEASE_TAG=$$(curl -s "https://api.github.com/repos/$(GITHUB_REPO)/releases" | \
				grep -o '"tag_name": *"[^"]*"' | \
				grep '"$(FRONTEND_BRANCH)-v' | \
				head -1 | \
				sed 's/"tag_name": *"\([^"]*\)"/\1/'); \
		else \
			printf "$(YELLOW)Using latest stable release$(RESET)\n"; \
			RELEASE_TAG=$$(curl -s "https://api.github.com/repos/$(GITHUB_REPO)/releases/latest" | \
				grep '"tag_name":' | \
				sed -E 's/.*"tag_name": *"([^"]+)".*/\1/'); \
		fi; \
		if [ -z "$$RELEASE_TAG" ]; then \
			printf "$(RED)Error: No release found$(RESET)\n"; \
			exit 1; \
		fi; \
		printf "$(CYAN)Found release: $$RELEASE_TAG$(RESET)\n"; \
		CURRENT_VERSION=$$(cat $(FRONTEND_VERSION_FILE) 2>/dev/null || echo ""); \
		if [ "$$RELEASE_TAG" = "$$CURRENT_VERSION" ] && [ -d "$(FRONTEND_TARGET)" ] && [ -f "$(FRONTEND_TARGET)/index.html" ]; then \
			printf "$(GREEN)✓ Frontend already up to date ($$RELEASE_TAG)$(RESET)\n"; \
		else \
			printf "$(CYAN)==> Downloading frontend $$RELEASE_TAG...$(RESET)\n"; \
			rm -rf .tmp/frontend-download .tmp/frontend-extract; \
			mkdir -p .tmp/frontend-download .tmp/frontend-extract; \
			ASSET_NAME="lab-$$RELEASE_TAG.tar.gz"; \
			ASSET_URL=$$(curl -s "https://api.github.com/repos/$(GITHUB_REPO)/releases/tags/$$RELEASE_TAG" | \
				grep "browser_download_url.*$$ASSET_NAME" | \
				cut -d '"' -f 4); \
			if [ -z "$$ASSET_URL" ]; then \
				printf "$(RED)Error: Release asset $$ASSET_NAME not found$(RESET)\n"; \
				exit 1; \
			fi; \
			curl -L "$$ASSET_URL" -o .tmp/frontend-download/release.tar.gz; \
			printf "$(CYAN)==> Extracting frontend...$(RESET)\n"; \
			tar -xzf .tmp/frontend-download/release.tar.gz -C .tmp/frontend-extract; \
			if [ ! -f ".tmp/frontend-extract/index.html" ]; then \
				printf "$(RED)Error: index.html not found in release asset$(RESET)\n"; \
				exit 1; \
			fi; \
			rm -rf $(FRONTEND_TARGET); \
			mkdir -p $(FRONTEND_TARGET); \
			cp -r .tmp/frontend-extract/* $(FRONTEND_TARGET)/; \
			echo "$$RELEASE_TAG" > $(FRONTEND_VERSION_FILE); \
			printf "$(GREEN)✓ Frontend updated to $$RELEASE_TAG$(RESET)\n"; \
			rm -rf .tmp/frontend-download .tmp/frontend-extract; \
		fi; \
	fi

## redis: Start Redis container for local development
redis:
	@docker rm -f lab-redis 2>/dev/null || true
	@docker run --name lab-redis -p 6379:6379 -d redis:7-alpine
	@printf "$(GREEN)✓ Redis started on localhost:6379$(RESET)\n"

## stop-redis: Stop and remove Redis container
stop-redis:
	@docker rm -f lab-redis 2>/dev/null || true
	@printf "$(GREEN)✓ Redis stopped$(RESET)\n"

## clean: Clean build artifacts and frontend
clean: stop-redis
	@printf "$(CYAN)==> Cleaning artifacts...$(RESET)\n"
	@rm -rf bin/ dist/ .tmp/ $(FRONTEND_TARGET)
	@go clean
	@printf "$(GREEN)✓ Clean complete$(RESET)\n"

## test: Run all tests
test:
	@printf "$(CYAN)==> Running tests...$(RESET)\n"
	go test -v -race -cover ./...

## run: Build and run the server locally
run: redis build
	@printf "$(CYAN)==> Starting server...$(RESET)\n"
	@./bin/lab-backend -config config.yaml

## help: Display this help message
help:
	@printf "$(CYAN)lab-backend Makefile$(RESET)\n"
	@printf "\n"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
	@printf "\n"
