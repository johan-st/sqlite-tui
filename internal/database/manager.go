package database

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/johan-st/sqlite-tui/internal/access"
	"github.com/johan-st/sqlite-tui/internal/config"
)

// Manager manages database connections and access.
type Manager struct {
	discovery   *Discovery
	connections map[string]*Connection
	lockManager *LockManager
	resolver    *access.Resolver
	mu          sync.RWMutex
}

// NewManager creates a new database manager.
func NewManager(cfg *config.Config) (*Manager, error) {
	discovery, err := NewDiscovery(cfg.Databases)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery: %w", err)
	}

	m := &Manager{
		discovery:   discovery,
		connections: make(map[string]*Connection),
		lockManager: NewLockManager(),
		resolver:    cfg.BuildResolver(),
	}

	return m, nil
}

// Start starts the database manager and discovery.
func (m *Manager) Start() error {
	return m.discovery.Start()
}

// Stop stops the database manager.
func (m *Manager) Stop() {
	m.discovery.Stop()

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, conn := range m.connections {
		conn.Close()
	}
	m.connections = make(map[string]*Connection)
}

// GetDiscovery returns the discovery service.
func (m *Manager) GetDiscovery() *Discovery {
	return m.discovery
}

// GetLockManager returns the lock manager.
func (m *Manager) GetLockManager() *LockManager {
	return m.lockManager
}

// UpdateResolver updates the access resolver (called on config reload).
func (m *Manager) UpdateResolver(resolver *access.Resolver) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resolver = resolver
}

// ListDatabases returns all databases accessible by the user.
func (m *Manager) ListDatabases(user *access.UserInfo) []*DatabaseInfo {
	databases := m.discovery.GetDatabases()
	result := make([]*DatabaseInfo, 0, len(databases))

	m.mu.RLock()
	resolver := m.resolver
	m.mu.RUnlock()

	for _, db := range databases {
		level := resolver.Resolve(user, db.Path, db.Alias)
		if level.CanRead() {
			result = append(result, &DatabaseInfo{
				Path:        db.Path,
				Alias:       db.Alias,
				Description: db.Description,
				Size:        db.Size,
				ModTime:     db.ModTime,
				AccessLevel: level,
			})
		}
	}

	return result
}

// DatabaseInfo contains information about a database for listing.
type DatabaseInfo struct {
	Path        string
	Alias       string
	Description string
	Size        int64
	ModTime     int64
	AccessLevel access.Level
}

// GetDatabase returns a discovered database by path or alias.
func (m *Manager) GetDatabase(pathOrAlias string) *DiscoveredDatabase {
	return m.discovery.GetDatabase(pathOrAlias)
}

// GetAccessLevel returns the access level for a user to a database.
func (m *Manager) GetAccessLevel(user *access.UserInfo, pathOrAlias string) access.Level {
	db := m.discovery.GetDatabase(pathOrAlias)
	if db == nil {
		return access.None
	}

	m.mu.RLock()
	resolver := m.resolver
	m.mu.RUnlock()

	return resolver.Resolve(user, db.Path, db.Alias)
}

// OpenConnection opens or returns an existing connection to a database.
func (m *Manager) OpenConnection(pathOrAlias string, user *access.UserInfo) (*Connection, error) {
	db := m.discovery.GetDatabase(pathOrAlias)
	if db == nil {
		return nil, fmt.Errorf("database not found: %s", pathOrAlias)
	}

	// Check access
	level := m.GetAccessLevel(user, pathOrAlias)
	if !level.CanRead() {
		return nil, fmt.Errorf("access denied to database: %s", pathOrAlias)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Return existing connection if available
	if conn, ok := m.connections[db.Path]; ok {
		return conn, nil
	}

	// Open new connection
	// Open as read-only if user doesn't have write access
	opts := DefaultOpenOptions()
	opts.ReadOnly = !level.CanWrite()

	conn, err := Open(db.Path, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	m.connections[db.Path] = conn
	return conn, nil
}

// CloseConnection closes a connection to a database.
func (m *Manager) CloseConnection(pathOrAlias string) error {
	db := m.discovery.GetDatabase(pathOrAlias)
	if db == nil {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if conn, ok := m.connections[db.Path]; ok {
		delete(m.connections, db.Path)
		return conn.Close()
	}

	return nil
}

// ExecuteQuery executes a query on a database.
func (m *Manager) ExecuteQuery(pathOrAlias string, user *access.UserInfo, sessionID string, query string) (*QueryResult, error) {
	db := m.discovery.GetDatabase(pathOrAlias)
	if db == nil {
		return nil, fmt.Errorf("database not found: %s", pathOrAlias)
	}

	level := m.GetAccessLevel(user, pathOrAlias)

	// Check if query requires write access
	if !isReadOnlyQuery(query) && !level.CanWrite() {
		return nil, fmt.Errorf("access denied: write permission required")
	}

	conn, err := m.OpenConnection(pathOrAlias, user)
	if err != nil {
		return nil, err
	}

	// For write queries, acquire lock
	if !isReadOnlyQuery(query) {
		if err := m.lockManager.TryLock(db.Path, user.DisplayName(), sessionID); err != nil {
			return nil, err
		}
		defer m.lockManager.Unlock(db.Path, sessionID)
	}

	result, err := Query(conn, query)
	if err != nil {
		// Check if it's a WAL lock error
		if IsWALLockError(err) {
			LogWALError(db.Path, err)
		}
		return nil, err
	}

	return result, nil
}

// StreamDatabase streams the raw database file to a writer.
func (m *Manager) StreamDatabase(pathOrAlias string, user *access.UserInfo, w io.Writer) error {
	db := m.discovery.GetDatabase(pathOrAlias)
	if db == nil {
		return fmt.Errorf("database not found: %s", pathOrAlias)
	}

	level := m.GetAccessLevel(user, pathOrAlias)
	if !level.CanDownload() {
		return fmt.Errorf("access denied: download permission required")
	}

	// Open the file directly for streaming
	f, err := os.Open(db.Path)
	if err != nil {
		return fmt.Errorf("failed to open database file: %w", err)
	}
	defer f.Close()

	_, err = io.Copy(w, f)
	return err
}

// isReadOnlyQuery checks if a query is read-only.
func isReadOnlyQuery(query string) bool {
	// Simple heuristic - in production you'd want proper SQL parsing
	upper := trimToUpper(query)
	return hasPrefix(upper, "SELECT") ||
		hasPrefix(upper, "PRAGMA") ||
		hasPrefix(upper, "EXPLAIN") ||
		hasPrefix(upper, "WITH")
}

func trimToUpper(s string) string {
	// Trim whitespace and convert to uppercase (first 20 chars)
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	end := start + 20
	if end > len(s) {
		end = len(s)
	}
	result := make([]byte, end-start)
	for i := start; i < end; i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			result[i-start] = c - 32
		} else {
			result[i-start] = c
		}
	}
	return string(result)
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// Refresh refreshes the database discovery.
func (m *Manager) Refresh() error {
	return m.discovery.Refresh()
}

// OnDatabaseChange registers a callback for database changes.
func (m *Manager) OnDatabaseChange(callback func(added, removed []*DiscoveredDatabase)) {
	m.discovery.OnChange(callback)
}
