.PHONY: build run test clean install dev ssh local

# Build variables
BINARY_NAME=sqlite-tui
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)"

# Default target
all: build

# Build the binary
build:
	@mkdir -p bin
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/sqlite-tui

# Build for all platforms
build-all:
	@mkdir -p bin
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o ./bin/$(BINARY_NAME)-linux-amd64 ./cmd/sqlite-tui
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o ./bin/$(BINARY_NAME)-linux-arm64 ./cmd/sqlite-tui
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o ./bin/$(BINARY_NAME)-darwin-amd64 ./cmd/sqlite-tui
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o ./bin/$(BINARY_NAME)-darwin-arm64 ./cmd/sqlite-tui
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o ./bin/$(BINARY_NAME)-windows-amd64.exe ./cmd/sqlite-tui

# Run the application
run: build
	./bin/$(BINARY_NAME)

# Run in local mode only
local: build
	./bin/$(BINARY_NAME) -local

# Run SSH server only
ssh: build
	./bin/$(BINARY_NAME) -ssh

# Run with hot-reloading (requires air: go install github.com/air-verse/air@latest)
dev:
	air

# Run tests
test:
	go test -v ./...

# Run tests with short output
test-short:
	go test ./...

# Run tests with coverage
test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Run only access control tests (critical security tests)
test-access:
	go test -v ./internal/access/... ./internal/database/... -run "Access|ReadOnly|Permission"

# Run SQL injection tests
test-injection:
	go test -v ./internal/database/... -run "Injection"

# Run CLI tests
test-cli:
	go test -v ./internal/cli/...

# Run tests with race detection
test-race:
	go test -race ./...

# Generate test fixtures (run when fixture schema changes)
test-fixtures:
	cd testdata/fixtures && go run gen_fixtures.go

# Update golden files (run when expected output changes)
test-golden-update:
	GOLDEN_UPDATE=1 go test -v ./...

# Clean build artifacts
clean:
	rm -f $(BINARY_NAME) $(BINARY_NAME)-*
	rm -rf bin/
	rm -f coverage.out coverage.html
	rm -rf .sqlite-tui/

# Install to GOPATH/bin
install:
	go install $(LDFLAGS) ./cmd/sqlite-tui

# Format code
fmt:
	go fmt ./...

# Lint code (requires golangci-lint)
lint:
	golangci-lint run

# Download dependencies
deps:
	go mod download
	go mod tidy

# Generate test database
test-db:
	@mkdir -p testdata
	@echo "Creating test database..."
	@sqlite3 testdata/test.db "CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY, name TEXT, email TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);"
	@sqlite3 testdata/test.db "INSERT OR IGNORE INTO users (id, name, email) VALUES (1, 'Alice', 'alice@example.com'), (2, 'Bob', 'bob@example.com'), (3, 'Charlie', 'charlie@example.com');"
	@sqlite3 testdata/test.db "CREATE TABLE IF NOT EXISTS posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id), title TEXT, content TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);"
	@sqlite3 testdata/test.db "INSERT OR IGNORE INTO posts (id, user_id, title, content) VALUES (1, 1, 'Hello World', 'My first post'), (2, 1, 'Another Post', 'More content here'), (3, 2, 'Bob''s Post', 'Bob writes stuff');"
	@echo "Test database created at testdata/test.db"

# Show help
help:
	@echo "sqlite-tui Makefile"
	@echo ""
	@echo "Build:"
	@echo "  make build              - Build the binary"
	@echo "  make build-all          - Build for all platforms (linux, darwin, windows)"
	@echo ""
	@echo "Run:"
	@echo "  make run                - Build and run"
	@echo "  make local              - Run in local mode (no SSH)"
	@echo "  make ssh                - Run SSH server only"
	@echo "  make dev                - Run with hot-reloading (requires air)"
	@echo ""
	@echo "Testing:"
	@echo "  make test               - Run all tests"
	@echo "  make test-short         - Run tests (short output)"
	@echo "  make test-coverage      - Run tests with coverage report"
	@echo "  make test-access        - Run access control tests"
	@echo "  make test-injection     - Run SQL injection tests"
	@echo "  make test-cli           - Run CLI tests"
	@echo "  make test-race          - Run tests with race detection"
	@echo "  make test-fixtures      - Regenerate test fixtures"
	@echo "  make test-golden-update - Update golden files"
	@echo "  make test-db            - Generate test database"
	@echo ""
	@echo "Other:"
	@echo "  make clean              - Clean build artifacts"
	@echo "  make install            - Install to GOPATH/bin"
	@echo "  make deps               - Download dependencies"
	@echo "  make lint               - Run linter (requires golangci-lint)"
	@echo "  make fmt                - Format code"
