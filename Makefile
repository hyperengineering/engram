BIN_DIR ?= dist
ENGRAM_BIN := $(BIN_DIR)/engram

.PHONY: build clean test test-integration test-server-integration e2e-setup test-e2e test-e2e-recall lint lint-openapi run fmt vet ci

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

# Run server integration tests (no binaries needed)
test-server-integration:
	go test -v ./test/e2e/ -run "TestSync_" -timeout 30s

# Download pre-built binaries for E2E tests
e2e-setup:
	@mkdir -p bin/e2e
	-gh release download --repo hyperengineering/recall -p "*linux_amd64.tar.gz" -D bin/e2e --clobber
	-tar -xzf bin/e2e/recall_*_linux_amd64.tar.gz -C bin/e2e
	-gh release download --repo hyperengineering/tract -p "*linux_amd64.tar.gz" -D bin/e2e --clobber
	-tar -xzf bin/e2e/tract_*_linux_amd64.tar.gz -C bin/e2e

# Run full E2E tests with real binaries
test-e2e: build e2e-setup
	ENGRAM_BIN=./$(ENGRAM_BIN) RECALL_BIN=./bin/e2e/recall TRACT_BIN=./bin/e2e/tract \
	go test -v -tags=e2e ./test/e2e/ -timeout 300s

# Run E2E tests (Recall only — no Tract binary needed)
test-e2e-recall: build
	ENGRAM_BIN=./$(ENGRAM_BIN) RECALL_BIN=./bin/e2e/recall \
	go test -v -tags=e2e -run "Recall|Multi_TwoRecall|Resilience" ./test/e2e/ -timeout 300s

# All checks (for CI)
ci: fmt vet lint test test-server-integration build
