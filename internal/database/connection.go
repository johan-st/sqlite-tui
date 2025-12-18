// Package database handles SQLite database connections and operations.
package database

import (
	"database/sql"
	"fmt"
	"sync"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

// Connection wraps a database connection with metadata.
type Connection struct {
	DB       *sql.DB
	Path     string
	ReadOnly bool
	mu       sync.Mutex
}

// OpenOptions configures how a database connection is opened.
type OpenOptions struct {
	ReadOnly    bool
	BusyTimeout int // milliseconds
}

// DefaultOpenOptions returns sensible defaults for opening a database.
func DefaultOpenOptions() OpenOptions {
	return OpenOptions{
		ReadOnly:    false,
		BusyTimeout: 5000, // 5 seconds
	}
}

// Open opens a database connection with the given options.
func Open(path string, opts OpenOptions) (*Connection, error) {
	mode := "rwc"
	if opts.ReadOnly {
		mode = "ro"
	}

	dsn := fmt.Sprintf("file:%s?mode=%s&_busy_timeout=%d&_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=ON",
		path, mode, opts.BusyTimeout)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Configure connection pool for SQLite
	db.SetMaxOpenConns(1) // SQLite doesn't handle concurrent writes well
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // Don't close idle connections

	return &Connection{
		DB:       db,
		Path:     path,
		ReadOnly: opts.ReadOnly,
	}, nil
}

// OpenReadOnly opens a database in read-only mode.
func OpenReadOnly(path string) (*Connection, error) {
	opts := DefaultOpenOptions()
	opts.ReadOnly = true
	return Open(path, opts)
}

// OpenReadWrite opens a database in read-write mode.
func OpenReadWrite(path string) (*Connection, error) {
	opts := DefaultOpenOptions()
	opts.ReadOnly = false
	return Open(path, opts)
}

// Close closes the database connection.
func (c *Connection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.DB != nil {
		return c.DB.Close()
	}
	return nil
}

// Execute runs a query that doesn't return rows (INSERT, UPDATE, DELETE).
func (c *Connection) Execute(query string, args ...any) (sql.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.DB.Exec(query, args...)
}

// Query runs a query that returns rows.
func (c *Connection) Query(query string, args ...any) (*sql.Rows, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.DB.Query(query, args...)
}

// QueryRow runs a query that returns at most one row.
func (c *Connection) QueryRow(query string, args ...any) *sql.Row {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.DB.QueryRow(query, args...)
}

// Begin starts a new transaction.
func (c *Connection) Begin() (*sql.Tx, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.DB.Begin()
}

// WithTransaction executes a function within a transaction.
func (c *Connection) WithTransaction(fn func(*sql.Tx) error) error {
	tx, err := c.Begin()
	if err != nil {
		return err
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}
