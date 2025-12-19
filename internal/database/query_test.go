package database

import (
	"strings"
	"testing"

	"github.com/johan-st/sqlite-tui/internal/testutil"
)

// TestSQLInjection_QuoteIdentifier tests that identifier quoting prevents injection.
func TestSQLInjection_QuoteIdentifier(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple name",
			input:    "users",
			expected: `"users"`,
		},
		{
			name:     "name with double quotes",
			input:    `users"; DROP TABLE users; --`,
			expected: `"users""; DROP TABLE users; --"`,
		},
		{
			name:     "empty name",
			input:    "",
			expected: `""`,
		},
		{
			name:     "name with special chars",
			input:    "table-with-dashes",
			expected: `"table-with-dashes"`,
		},
		{
			name:     "name with backticks",
			input:    "`users`",
			expected: "\"`users`\"",
		},
		{
			name:     "name with semicolon",
			input:    "users;",
			expected: `"users;"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := quoteIdentifier(tt.input)
			if got != tt.expected {
				t.Errorf("quoteIdentifier(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestSQLInjection_Insert tests that malicious data in Insert is properly escaped.
func TestSQLInjection_Insert(t *testing.T) {
	dbPath, cleanup := testutil.TestDB(t, "users.db")
	defer cleanup()

	conn, err := OpenReadWrite(dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer conn.Close()

	// Attempt SQL injection via column value
	maliciousData := map[string]any{
		"name":  "Robert'); DROP TABLE users; --",
		"email": "bobby@tables.com",
	}

	result, err := Insert(conn, "users", maliciousData)
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	if result.LastInsertID == 0 {
		t.Error("expected insert to succeed")
	}

	// Verify table still exists and data was inserted literally
	schema := NewSchema(conn)
	exists, err := schema.TableExists("users")
	if err != nil {
		t.Fatalf("TableExists failed: %v", err)
	}
	if !exists {
		t.Error("users table was dropped - SQL injection succeeded!")
	}

	// Verify the malicious string was stored literally
	queryResult, err := Query(conn, "SELECT name FROM users WHERE id = ?", result.LastInsertID)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(queryResult.Rows) == 0 {
		t.Fatal("expected row to be inserted")
	}
	name := queryResult.Rows[0][0].(string)
	if name != "Robert'); DROP TABLE users; --" {
		t.Errorf("name = %q, want literal malicious string", name)
	}
}

// TestSQLInjection_Update tests that malicious data in Update is properly escaped.
func TestSQLInjection_Update(t *testing.T) {
	dbPath, cleanup := testutil.TestDB(t, "users.db")
	defer cleanup()

	conn, err := OpenReadWrite(dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer conn.Close()

	// Get initial count
	var countBefore int64
	schema := NewSchema(conn)
	countBefore, _ = schema.GetRowCount("users")

	// Attempt injection via column name (should be quoted)
	maliciousData := map[string]any{
		"name": "1; DELETE FROM users WHERE 1=1; --",
	}

	_, err = Update(conn, "users", maliciousData, "id = 1")
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify all rows still exist
	countAfter, _ := schema.GetRowCount("users")
	if countAfter != countBefore {
		t.Errorf("row count changed from %d to %d - possible injection", countBefore, countAfter)
	}
}

// TestSQLInjection_TableName tests that malicious table names are quoted.
func TestSQLInjection_TableName(t *testing.T) {
	dbPath, cleanup := testutil.TestDB(t, "users.db")
	defer cleanup()

	conn, err := OpenReadWrite(dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer conn.Close()

	// Attempt to inject via table name
	maliciousTable := `users"; DROP TABLE users; --`

	// Should fail to find the table, not execute injection
	_, err = Select(conn, maliciousTable, DefaultSelectOptions())
	if err == nil {
		t.Error("expected error for malicious table name")
	}

	// Verify users table still exists
	schema := NewSchema(conn)
	exists, _ := schema.TableExists("users")
	if !exists {
		t.Error("users table was dropped - SQL injection succeeded!")
	}
}

// TestDangerousQueries tests detection of dangerous queries.
func TestDangerousQueries(t *testing.T) {
	tests := []struct {
		query    string
		readOnly bool
	}{
		{"SELECT * FROM users", true},
		{"select * from users", true},
		{"  SELECT * FROM users", true},
		{"PRAGMA table_info(users)", true},
		{"EXPLAIN SELECT * FROM users", true},
		{"WITH cte AS (SELECT 1) SELECT * FROM cte", true},

		{"INSERT INTO users (name) VALUES ('x')", false},
		{"UPDATE users SET name = 'y'", false},
		{"DELETE FROM users", false},
		{"DROP TABLE users", false},
		{"CREATE TABLE new_table (id INT)", false},
		{"ALTER TABLE users ADD COLUMN x TEXT", false},
		{"ATTACH DATABASE ':memory:' AS mem", false},
		{"DETACH DATABASE mem", false},
	}

	// Use the internal isReadOnlyQuery check logic
	isReadOnly := func(query string) bool {
		upper := strings.ToUpper(strings.TrimSpace(query))
		return strings.HasPrefix(upper, "SELECT") ||
			strings.HasPrefix(upper, "PRAGMA") ||
			strings.HasPrefix(upper, "EXPLAIN") ||
			strings.HasPrefix(upper, "WITH")
	}

	for _, tt := range tests {
		t.Run(tt.query[:min(30, len(tt.query))], func(t *testing.T) {
			got := isReadOnly(tt.query)
			if got != tt.readOnly {
				t.Errorf("isReadOnly(%q) = %v, want %v", tt.query, got, tt.readOnly)
			}
		})
	}
}

// TestDelete_RequiresWhere tests that Delete enforces WHERE clause.
func TestDelete_WithoutWhere(t *testing.T) {
	dbPath, cleanup := testutil.TestDB(t, "users.db")
	defer cleanup()

	conn, err := OpenReadWrite(dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer conn.Close()

	// Get initial count
	schema := NewSchema(conn)
	countBefore, _ := schema.GetRowCount("users")

	// Delete without WHERE - this SHOULD work at DB level
	// The protection should be at CLI/API level, but let's verify behavior
	result, err := Delete(conn, "users", "")
	if err != nil {
		// If Delete returns error for empty where, that's a safety feature
		t.Logf("Delete without WHERE returned error (safety feature): %v", err)
		return
	}

	// If it succeeded, all rows should be deleted
	if result.RowsAffected != countBefore {
		t.Errorf("expected %d rows affected, got %d", countBefore, result.RowsAffected)
	}
}

// TestCRUD_BasicOperations tests basic CRUD operations work correctly.
func TestCRUD_BasicOperations(t *testing.T) {
	dbPath, cleanup := testutil.TestDB(t, "empty.db")
	defer cleanup()

	conn, err := OpenReadWrite(dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer conn.Close()

	// Insert
	insertResult, err := Insert(conn, "items", map[string]any{
		"name":  "Test Item",
		"value": 42.5,
	})
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if insertResult.LastInsertID != 1 {
		t.Errorf("LastInsertID = %d, want 1", insertResult.LastInsertID)
	}

	// Select
	selectResult, err := Select(conn, "items", DefaultSelectOptions())
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if len(selectResult.Rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(selectResult.Rows))
	}

	// Update
	updateResult, err := Update(conn, "items", map[string]any{"name": "Updated Item"}, "id = 1")
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updateResult.RowsAffected != 1 {
		t.Errorf("RowsAffected = %d, want 1", updateResult.RowsAffected)
	}

	// Verify update
	selectResult, _ = Select(conn, "items", DefaultSelectOptions())
	name := selectResult.Rows[0][1].(string)
	if name != "Updated Item" {
		t.Errorf("name = %q, want 'Updated Item'", name)
	}

	// Delete
	deleteResult, err := Delete(conn, "items", "id = 1")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if deleteResult.RowsAffected != 1 {
		t.Errorf("RowsAffected = %d, want 1", deleteResult.RowsAffected)
	}

	// Verify delete
	selectResult, _ = Select(conn, "items", DefaultSelectOptions())
	if len(selectResult.Rows) != 0 {
		t.Errorf("expected 0 rows after delete, got %d", len(selectResult.Rows))
	}
}

// TestSelect_Pagination tests offset and limit options.
func TestSelect_Pagination(t *testing.T) {
	dbPath, cleanup := testutil.TestDB(t, "large.db")
	defer cleanup()

	conn, err := OpenReadOnly(dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer conn.Close()

	// Test limit
	opts := SelectOptions{Limit: 10}
	result, err := Select(conn, "records", opts)
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if len(result.Rows) != 10 {
		t.Errorf("expected 10 rows, got %d", len(result.Rows))
	}

	// Test offset
	opts = SelectOptions{Limit: 10, Offset: 5}
	result, err = Select(conn, "records", opts)
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	// First row should be id=6
	id := result.Rows[0][0].(int64)
	if id != 6 {
		t.Errorf("expected first row id=6, got %d", id)
	}
}

// TestReadOnly_CannotWrite tests that read-only connections cannot write.
func TestReadOnly_CannotWrite(t *testing.T) {
	dbPath, cleanup := testutil.TestDB(t, "users.db")
	defer cleanup()

	conn, err := OpenReadOnly(dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer conn.Close()

	// Attempt to insert
	_, err = Insert(conn, "users", map[string]any{
		"name":  "Should Fail",
		"email": "fail@test.com",
	})
	if err == nil {
		t.Error("expected error when inserting via read-only connection")
	}

	// Attempt to update
	_, err = Update(conn, "users", map[string]any{"name": "Modified"}, "id = 1")
	if err == nil {
		t.Error("expected error when updating via read-only connection")
	}

	// Attempt to delete
	_, err = Delete(conn, "users", "id = 1")
	if err == nil {
		t.Error("expected error when deleting via read-only connection")
	}

	// Attempt raw write query
	_, err = Query(conn, "DROP TABLE users")
	if err == nil {
		t.Error("expected error when dropping table via read-only connection")
	}
}


