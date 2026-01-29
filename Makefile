BIN_DIR ?= dist
ENGRAM_BIN := $(BIN_DIR)/engram

.PHONY: build clean test test-integration lint lint-openapi run fmt vet ci

# Build Engram central service binary
build:
	@mkdir -p $(BIN_DIR)
	go build -o $(ENGRAM_BIN) ./cmd/engram

# Clean build artifacts
clean:
	rm -rf $(BIN_DIR)
	rm -rf data/*.db

# Run unit tests
test:
	go test -v ./...

# Run integration tests
test-integration:
	go test -v -tags=integration ./...

# Run linter
lint:
	golangci-lint run ./...

# Lint OpenAPI specification
lint-openapi:
	npx @stoplight/spectral-cli lint docs/openapi.yaml

# Run Engram locally
run: build
	$(ENGRAM_BIN)

# Format code
fmt:
	go fmt ./...

# Vet code
vet:
	go vet ./...

# All checks (for CI)
ci: fmt vet lint test build
