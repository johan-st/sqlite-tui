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
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/sqlite-tui

# Build for all platforms
build-all:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-linux-amd64 ./cmd/sqlite-tui
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY_NAME)-linux-arm64 ./cmd/sqlite-tui
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-darwin-amd64 ./cmd/sqlite-tui
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY_NAME)-darwin-arm64 ./cmd/sqlite-tui
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-windows-amd64.exe ./cmd/sqlite-tui

# Run the application
run: build
	./$(BINARY_NAME)

# Run in local mode only
local: build
	./$(BINARY_NAME) -local

# Run SSH server only
ssh: build
	./$(BINARY_NAME) -ssh

# Run with hot-reloading (requires air: go install github.com/air-verse/air@latest)
dev:
	air

# Run tests
test:
	go test -v ./...

# Run tests with coverage
test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Clean build artifacts
clean:
	rm -f $(BINARY_NAME) $(BINARY_NAME)-*
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
	@echo "Usage:"
	@echo "  make build      - Build the binary"
	@echo "  make run        - Build and run"
	@echo "  make local      - Run in local mode (no SSH)"
	@echo "  make ssh        - Run SSH server only"
	@echo "  make dev        - Run with hot-reloading"
	@echo "  make test       - Run tests"
	@echo "  make test-db    - Create test database"
	@echo "  make clean      - Clean build artifacts"
	@echo "  make install    - Install to GOPATH/bin"
	@echo "  make deps       - Download dependencies"
	@echo "  make help       - Show this help"
