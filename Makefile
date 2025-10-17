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

.PHONY: all build build-all clean test run run-all redis stop-redis help

all: build

# Frontend configuration
FRONTEND_REPO_URL ?= https://github.com/ethpandaops/lab.git
FRONTEND_REF ?= release/frontend
FRONTEND_CLONE_DIR ?= .tmp/lab-frontend
FRONTEND_TARGET ?= web/frontend

## build: Build the lab-backend binary
build:
	@mkdir -p bin
	@# Ensure placeholder exists for dev mode (if no full frontend)
	@if [ ! -f web/frontend/index.html ]; then \
		if [ -f web/frontend/.placeholder.html ]; then \
			cp web/frontend/.placeholder.html web/frontend/index.html; \
		fi; \
	fi
	@go build -ldflags "$(LDFLAGS)" -o bin/lab-backend ./cmd/server

## build-all: Clone frontend, build it, embed it, and build backend
build-all:
	@printf "$(CYAN)==> Building complete lab-backend with frontend...$(RESET)\n"
	@printf "$(YELLOW)Frontend repo: $(FRONTEND_REPO_URL)$(RESET)\n"
	@printf "$(YELLOW)Frontend ref:  $(FRONTEND_REF)$(RESET)\n"
	@printf "\n"

	@# Clone or update frontend repo
	@if [ -d "$(FRONTEND_CLONE_DIR)" ]; then \
		printf "$(CYAN)==> Updating existing frontend clone...$(RESET)\n"; \
		cd $(FRONTEND_CLONE_DIR) && git fetch origin; \
	else \
		printf "$(CYAN)==> Cloning frontend repo...$(RESET)\n"; \
		git clone $(FRONTEND_REPO_URL) $(FRONTEND_CLONE_DIR); \
	fi

	@# Checkout specified ref
	@printf "$(CYAN)==> Checking out $(FRONTEND_REF)...$(RESET)\n"
	@cd $(FRONTEND_CLONE_DIR) && git checkout $(FRONTEND_REF)
	@cd $(FRONTEND_CLONE_DIR) && git pull origin $(FRONTEND_REF) 2>/dev/null || true

	@# Build frontend
	@printf "$(CYAN)==> Cleaning previous build...$(RESET)\n"
	@cd $(FRONTEND_CLONE_DIR) && rm -rf build 2>/dev/null || true
	@printf "$(CYAN)==> Installing frontend dependencies...$(RESET)\n"
	@cd $(FRONTEND_CLONE_DIR) && npm install
	@printf "$(CYAN)==> Building frontend (Vite plugin auto-generates routes)...$(RESET)\n"
	@cd $(FRONTEND_CLONE_DIR) && npx vite build

	@# Copy frontend build output to web/frontend (preserve .gitkeep and .placeholder.html)
	@printf "$(CYAN)==> Copying frontend to $(FRONTEND_TARGET)...$(RESET)\n"
	@mkdir -p $(FRONTEND_TARGET)
	@rsync -av --delete --exclude='.gitkeep' --exclude='.placeholder.html' $(FRONTEND_CLONE_DIR)/build/frontend/ $(FRONTEND_TARGET)/

	@# Build backend with embedded frontend
	@printf "$(CYAN)==> Building lab-backend with embedded frontend...$(RESET)\n"
	@$(MAKE) build
	@printf "$(GREEN)✓ Complete build finished!$(RESET)\n"
	@printf "$(GREEN)  Backend binary: bin/lab-backend$(RESET)\n"
	@printf "$(GREEN)  Frontend embedded from: $(FRONTEND_REF)$(RESET)\n"

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
	@rm -rf bin/ dist/ $(FRONTEND_CLONE_DIR)
	@find $(FRONTEND_TARGET) -mindepth 1 ! -name '.gitkeep' ! -name '.placeholder.html' -delete 2>/dev/null || true
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

## run-all: Build with frontend and run the server locally
run-all: redis build-all
	@printf "$(CYAN)==> Starting server with embedded frontend...$(RESET)\n"
	@./bin/lab-backend -config config.yaml

## help: Display this help message
help:
	@printf "$(CYAN)lab-backend Makefile$(RESET)\n"
	@printf "\n"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
	@printf "\n"
