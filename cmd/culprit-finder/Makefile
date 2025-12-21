.PHONY: build install clean test run example help

# Build variables
BINARY_NAME=culprit-finder
INSTALL_PATH=/usr/local/bin

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@go build -o $(BINARY_NAME) .
	@echo "✓ Build complete: ./$(BINARY_NAME)"

# Build with version info
build-release:
	@echo "Building $(BINARY_NAME) (release)..."
	@go build -ldflags="-s -w" -o $(BINARY_NAME) .
	@echo "✓ Release build complete: ./$(BINARY_NAME)"

# Install to system path
install: build
	@echo "Installing $(BINARY_NAME) to $(INSTALL_PATH)..."
	@sudo mv $(BINARY_NAME) $(INSTALL_PATH)/
	@echo "✓ Installed: $(INSTALL_PATH)/$(BINARY_NAME)"

# Uninstall from system path
uninstall:
	@echo "Uninstalling $(BINARY_NAME)..."
	@sudo rm -f $(INSTALL_PATH)/$(BINARY_NAME)
	@echo "✓ Uninstalled"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -f $(BINARY_NAME)
	@rm -rf .culprit-finder
	@echo "✓ Clean complete"

# Run tests
test:
	@echo "Running tests..."
	@go test ./...
	@echo "✓ Tests complete"

# Run with example arguments
run: build
	@./$(BINARY_NAME) --help

# Show example usage
example: build
	@./example.sh

# Development mode - rebuild on file changes (requires entr)
dev:
	@echo "Watching for changes... (Ctrl+C to stop)"
	@find . -name '*.go' | entr -c make build

# Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...
	@echo "✓ Format complete"

# Lint code (requires golangci-lint)
lint:
	@echo "Linting code..."
	@golangci-lint run ./...
	@echo "✓ Lint complete"

# Show help
help:
	@echo "Culprit Finder - Makefile Commands"
	@echo ""
	@echo "Build:"
	@echo "  make build          - Build the binary"
	@echo "  make build-release  - Build optimized release binary"
	@echo "  make install        - Install to $(INSTALL_PATH)"
	@echo "  make uninstall      - Remove from $(INSTALL_PATH)"
	@echo ""
	@echo "Development:"
	@echo "  make test           - Run tests"
	@echo "  make fmt            - Format code"
	@echo "  make lint           - Lint code"
	@echo "  make dev            - Watch and rebuild on changes"
	@echo ""
	@echo "Usage:"
	@echo "  make run            - Show help"
	@echo "  make example        - Show example usage"
	@echo "  make clean          - Clean build artifacts"
	@echo ""
	@echo "Quick Start:"
	@echo "  make build"
	@echo "  ./$(BINARY_NAME) start <good-ref> <bad-ref> -- <test-command>"
	@echo "  ./$(BINARY_NAME) run"
