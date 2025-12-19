package database

import (
	"strings"
	"testing"

	"github.com/johan-st/sqlite-tui/internal/access"
	"github.com/johan-st/sqlite-tui/internal/config"
	"github.com/johan-st/sqlite-tui/internal/testutil"
)

// TestManager_AccessControl tests that the manager enforces access control.
func TestManager_AccessControl(t *testing.T) {
	dbPath, cleanup := testutil.TestDB(t, "users.db")
	defer cleanup()

	cfg := &config.Config{
		Databases: []config.DatabaseSource{
			{Path: dbPath, Alias: "test"},
		},
		AnonymousAccess: "none",
		Users: []config.User{
			{Name: "reader", Access: []config.AccessRule{{Pattern: "*", Level: "read-only"}}},
			{Name: "writer", Access: []config.AccessRule{{Pattern: "*", Level: "read-write"}}},
			{Name: "admin", Admin: true},
		},
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	if err := manager.Start(); err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}
	defer manager.Stop()

	// Test access levels
	tests := []struct {
		name      string
		user      *access.UserInfo
		wantLevel access.Level
	}{
		{
			name:      "admin user",
			user:      &access.UserInfo{Name: "admin", IsAdmin: true},
			wantLevel: access.Admin,
		},
		{
			name:      "read-only user",
			user:      &access.UserInfo{Name: "reader"},
			wantLevel: access.ReadOnly,
		},
		{
			name:      "read-write user",
			user:      &access.UserInfo{Name: "writer"},
			wantLevel: access.ReadWrite,
		},
		{
			name:      "anonymous user",
			user:      &access.UserInfo{Name: "anon", IsAnonymous: true},
			wantLevel: access.None,
		},
		{
			name:      "unknown user",
			user:      &access.UserInfo{Name: "unknown"},
			wantLevel: access.None,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level := manager.GetAccessLevel(tt.user, "test")
			if level != tt.wantLevel {
				t.Errorf("GetAccessLevel() = %v, want %v", level, tt.wantLevel)
			}
		})
	}
}

// TestManager_ReadOnlyConnection tests that read-only users get read-only connections.
func TestManager_ReadOnlyConnection(t *testing.T) {
	dbPath, cleanup := testutil.TestDB(t, "users.db")
	defer cleanup()

	cfg := &config.Config{
		Databases: []config.DatabaseSource{
			{Path: dbPath, Alias: "test"},
		},
		AnonymousAccess: "read-only",
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	if err := manager.Start(); err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}
	defer manager.Stop()

	user := &access.UserInfo{Name: "anon", IsAnonymous: true}
	conn, err := manager.OpenConnection("test", user)
	if err != nil {
		t.Fatalf("failed to open connection: %v", err)
	}

	// Connection should be read-only
	if !conn.ReadOnly {
		t.Error("expected read-only connection for read-only user")
	}

	// Should not be able to write
	_, err = conn.Execute("INSERT INTO users (name, email) VALUES ('x', 'x@x.com')")
	if err == nil {
		t.Error("expected error when writing via read-only connection")
	}
}

// TestManager_ExecuteQuery_AccessDenied tests that write queries are denied for read-only users.
func TestManager_ExecuteQuery_AccessDenied(t *testing.T) {
	dbPath, cleanup := testutil.TestDB(t, "users.db")
	defer cleanup()

	cfg := &config.Config{
		Databases: []config.DatabaseSource{
			{Path: dbPath, Alias: "test"},
		},
		AnonymousAccess: "read-only",
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	if err := manager.Start(); err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}
	defer manager.Stop()

	user := &access.UserInfo{Name: "anon", IsAnonymous: true}

	// SELECT should work
	result, err := manager.ExecuteQuery("test", user, "", "SELECT * FROM users")
	if err != nil {
		t.Errorf("SELECT query failed: %v", err)
	}
	if len(result.Rows) == 0 {
		t.Error("expected rows from SELECT")
	}

	// INSERT should be denied
	_, err = manager.ExecuteQuery("test", user, "", "INSERT INTO users (name, email) VALUES ('x', 'x@x.com')")
	if err == nil {
		t.Error("expected INSERT to be denied for read-only user")
	}
	if !strings.Contains(err.Error(), "access denied") && !strings.Contains(err.Error(), "write permission") {
		t.Errorf("expected access denied error, got: %v", err)
	}

	// UPDATE should be denied
	_, err = manager.ExecuteQuery("test", user, "", "UPDATE users SET name = 'y'")
	if err == nil {
		t.Error("expected UPDATE to be denied for read-only user")
	}

	// DELETE should be denied
	_, err = manager.ExecuteQuery("test", user, "", "DELETE FROM users")
	if err == nil {
		t.Error("expected DELETE to be denied for read-only user")
	}

	// DROP should be denied
	_, err = manager.ExecuteQuery("test", user, "", "DROP TABLE users")
	if err == nil {
		t.Error("expected DROP to be denied for read-only user")
	}
}

// TestManager_ListDatabases_Filtered tests that users only see accessible databases.
func TestManager_ListDatabases_Filtered(t *testing.T) {
	dbPath, cleanup := testutil.TestDB(t, "users.db")
	defer cleanup()

	cfg := &config.Config{
		Databases: []config.DatabaseSource{
			{Path: dbPath, Alias: "test"},
		},
		AnonymousAccess: "none",
		Users: []config.User{
			{Name: "reader", Access: []config.AccessRule{{Pattern: "test", Level: "read-only"}}},
		},
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	if err := manager.Start(); err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}
	defer manager.Stop()

	// Reader should see the database
	reader := &access.UserInfo{Name: "reader"}
	dbs := manager.ListDatabases(reader)
	if len(dbs) != 1 {
		t.Errorf("expected 1 database for reader, got %d", len(dbs))
	}

	// Anonymous should not see any databases
	anon := &access.UserInfo{Name: "anon", IsAnonymous: true}
	dbs = manager.ListDatabases(anon)
	if len(dbs) != 0 {
		t.Errorf("expected 0 databases for anonymous, got %d", len(dbs))
	}
}

// TestManager_UnknownDatabase tests accessing non-existent database.
func TestManager_UnknownDatabase(t *testing.T) {
	dbPath, cleanup := testutil.TestDB(t, "users.db")
	defer cleanup()

	cfg := &config.Config{
		Databases: []config.DatabaseSource{
			{Path: dbPath, Alias: "test"},
		},
		AnonymousAccess: "admin",
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	if err := manager.Start(); err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}
	defer manager.Stop()

	user := &access.UserInfo{Name: "admin", IsAdmin: true}

	// Try to access non-existent database
	_, err = manager.OpenConnection("nonexistent", user)
	if err == nil {
		t.Error("expected error for non-existent database")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}

	_, err = manager.ExecuteQuery("nonexistent", user, "", "SELECT 1")
	if err == nil {
		t.Error("expected error for non-existent database")
	}
}

// TestManager_WriteOperations tests that write operations work for authorized users.
func TestManager_WriteOperations(t *testing.T) {
	dbPath, cleanup := testutil.TestDB(t, "users.db")
	defer cleanup()

	cfg := &config.Config{
		Databases: []config.DatabaseSource{
			{Path: dbPath, Alias: "test"},
		},
		Users: []config.User{
			{Name: "admin", Admin: true},
		},
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	if err := manager.Start(); err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}
	defer manager.Stop()

	admin := &access.UserInfo{Name: "admin", IsAdmin: true}

	// Insert
	result, err := manager.ExecuteQuery("test", admin, "sess1",
		"INSERT INTO users (name, email) VALUES ('NewUser', 'new@test.com')")
	if err != nil {
		t.Fatalf("INSERT failed: %v", err)
	}
	if result.RowsAffected != 1 {
		t.Errorf("expected 1 row affected, got %d", result.RowsAffected)
	}

	// Update
	result, err = manager.ExecuteQuery("test", admin, "sess1",
		"UPDATE users SET name = 'UpdatedUser' WHERE email = 'new@test.com'")
	if err != nil {
		t.Fatalf("UPDATE failed: %v", err)
	}
	if result.RowsAffected != 1 {
		t.Errorf("expected 1 row affected, got %d", result.RowsAffected)
	}

	// Verify update
	result, err = manager.ExecuteQuery("test", admin, "",
		"SELECT name FROM users WHERE email = 'new@test.com'")
	if err != nil {
		t.Fatalf("SELECT failed: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != "UpdatedUser" {
		t.Errorf("expected 'UpdatedUser', got %v", result.Rows)
	}

	// Delete
	result, err = manager.ExecuteQuery("test", admin, "sess1",
		"DELETE FROM users WHERE email = 'new@test.com'")
	if err != nil {
		t.Fatalf("DELETE failed: %v", err)
	}
	if result.RowsAffected != 1 {
		t.Errorf("expected 1 row affected, got %d", result.RowsAffected)
	}
}

// TestIsReadOnlyQuery tests the read-only query detection.
func TestIsReadOnlyQuery(t *testing.T) {
	tests := []struct {
		query    string
		readOnly bool
	}{
		{"SELECT * FROM users", true},
		{"select * from users", true},
		{"  SELECT * FROM users", true},
		{"\n\tSELECT * FROM users", true},
		{"PRAGMA table_info(users)", true},
		{"pragma table_info(users)", true},
		{"EXPLAIN SELECT * FROM users", true},
		{"WITH cte AS (SELECT 1) SELECT * FROM cte", true},

		{"INSERT INTO users VALUES (1)", false},
		{"insert into users values (1)", false},
		{"UPDATE users SET x = 1", false},
		{"DELETE FROM users", false},
		{"DROP TABLE users", false},
		{"CREATE TABLE t (id INT)", false},
		{"ALTER TABLE users ADD x INT", false},
		{"VACUUM", false},
		{"REINDEX", false},
	}

	for _, tt := range tests {
		t.Run(tt.query[:min(30, len(tt.query))], func(t *testing.T) {
			got := isReadOnlyQuery(tt.query)
			if got != tt.readOnly {
				t.Errorf("isReadOnlyQuery(%q) = %v, want %v", tt.query, got, tt.readOnly)
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
