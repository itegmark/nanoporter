.PHONY: build run clean install deps test help

# Binary name
BINARY_NAME=nanoporter

# Build directory
BUILD_DIR=.

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Build the application
build:
	@echo "Building $(BINARY_NAME)..."
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) -v

# Run the application
run: build
	@echo "Running $(BINARY_NAME)..."
	./$(BINARY_NAME)

# Run with custom config
run-config: build
	@echo "Running $(BINARY_NAME) with custom config..."
	./$(BINARY_NAME) -config $(CONFIG)

# Run with verbose logging
run-verbose: build
	@echo "Running $(BINARY_NAME) in verbose mode..."
	./$(BINARY_NAME) -verbose

# Install dependencies
deps:
	@echo "Installing dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -f $(BUILD_DIR)/$(BINARY_NAME)
	rm -f $(BUILD_DIR)/$(BINARY_NAME)-*

# Install globally
install: build
	@echo "Installing $(BINARY_NAME)..."
	$(GOCMD) install

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

# Build for multiple platforms
build-all: clean
	@echo "Building for multiple platforms..."
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64
	GOOS=darwin GOARCH=amd64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64
	GOOS=darwin GOARCH=arm64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64
	GOOS=windows GOARCH=amd64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe

# Show help
help:
	@echo "Available targets:"
	@echo "  build        - Build the application"
	@echo "  run          - Build and run the application"
	@echo "  run-config   - Run with custom config (use CONFIG=/path/to/config.yaml)"
	@echo "  run-verbose  - Run with verbose logging"
	@echo "  deps         - Install dependencies"
	@echo "  clean        - Remove build artifacts"
	@echo "  install      - Install globally"
	@echo "  test         - Run tests"
	@echo "  build-all    - Build for multiple platforms"
	@echo "  help         - Show this help message"