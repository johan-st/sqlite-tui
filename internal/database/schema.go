package database

import (
	"database/sql"
	"fmt"
	"strings"
)

// TableInfo contains information about a database table.
type TableInfo struct {
	Name       string
	SQL        string
	Columns    []ColumnInfo
	RowCount   int64
	PrimaryKey []string
}

// ColumnInfo contains information about a table column.
type ColumnInfo struct {
	CID          int
	Name         string
	Type         string
	NotNull      bool
	DefaultValue sql.NullString
	PrimaryKey   int // 0 if not PK, otherwise position in composite PK
}

// IndexInfo contains information about an index.
type IndexInfo struct {
	Name    string
	Unique  bool
	Columns []string
}

// ForeignKeyInfo contains information about a foreign key.
type ForeignKeyInfo struct {
	ID       int
	Table    string
	From     string
	To       string
	OnUpdate string
	OnDelete string
}

// Schema provides methods for introspecting database schema.
type Schema struct {
	conn *Connection
}

// NewSchema creates a new Schema introspector.
func NewSchema(conn *Connection) *Schema {
	return &Schema{conn: conn}
}

// ListTables returns all user tables in the database.
func (s *Schema) ListTables() ([]string, error) {
	rows, err := s.conn.Query(`
		SELECT name FROM sqlite_master 
		WHERE type = 'table' 
		AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan table name: %w", err)
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

// GetTableInfo returns detailed information about a table.
func (s *Schema) GetTableInfo(tableName string) (*TableInfo, error) {
	// Get table SQL
	var tableSql sql.NullString
	err := s.conn.QueryRow(`
		SELECT sql FROM sqlite_master 
		WHERE type = 'table' AND name = ?
	`, tableName).Scan(&tableSql)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("table %q not found", tableName)
		}
		return nil, fmt.Errorf("failed to get table SQL: %w", err)
	}

	info := &TableInfo{
		Name: tableName,
		SQL:  tableSql.String,
	}

	// Get columns
	columns, err := s.GetColumns(tableName)
	if err != nil {
		return nil, err
	}
	info.Columns = columns

	// Extract primary keys
	for _, col := range columns {
		if col.PrimaryKey > 0 {
			info.PrimaryKey = append(info.PrimaryKey, col.Name)
		}
	}

	// Get row count
	count, err := s.GetRowCount(tableName)
	if err != nil {
		return nil, err
	}
	info.RowCount = count

	return info, nil
}

// GetColumns returns column information for a table.
func (s *Schema) GetColumns(tableName string) ([]ColumnInfo, error) {
	rows, err := s.conn.Query(fmt.Sprintf("PRAGMA table_info(%s)", quoteIdentifier(tableName)))
	if err != nil {
		return nil, fmt.Errorf("failed to get column info: %w", err)
	}
	defer rows.Close()

	var columns []ColumnInfo
	for rows.Next() {
		var col ColumnInfo
		if err := rows.Scan(&col.CID, &col.Name, &col.Type, &col.NotNull, &col.DefaultValue, &col.PrimaryKey); err != nil {
			return nil, fmt.Errorf("failed to scan column info: %w", err)
		}
		columns = append(columns, col)
	}
	return columns, rows.Err()
}

// GetIndexes returns index information for a table.
func (s *Schema) GetIndexes(tableName string) ([]IndexInfo, error) {
	rows, err := s.conn.Query(fmt.Sprintf("PRAGMA index_list(%s)", quoteIdentifier(tableName)))
	if err != nil {
		return nil, fmt.Errorf("failed to get index list: %w", err)
	}

	// Collect index info first, then close rows before making nested queries
	// (SQLite with MaxOpenConns=1 will block if we try to query while iterating)
	type indexMeta struct {
		name   string
		unique bool
	}
	var metas []indexMeta

	for rows.Next() {
		var seq int
		var name string
		var unique int
		var origin string
		var partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan index info: %w", err)
		}
		metas = append(metas, indexMeta{name: name, unique: unique == 1})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	// Now fetch column info for each index
	var indexes []IndexInfo
	for _, meta := range metas {
		colRows, err := s.conn.Query(fmt.Sprintf("PRAGMA index_info(%s)", quoteIdentifier(meta.name)))
		if err != nil {
			return nil, fmt.Errorf("failed to get index columns: %w", err)
		}

		var columns []string
		for colRows.Next() {
			var seqno, cid int
			var colName string
			if err := colRows.Scan(&seqno, &cid, &colName); err != nil {
				colRows.Close()
				return nil, fmt.Errorf("failed to scan index column: %w", err)
			}
			columns = append(columns, colName)
		}
		colRows.Close()

		indexes = append(indexes, IndexInfo{
			Name:    meta.name,
			Unique:  meta.unique,
			Columns: columns,
		})
	}
	return indexes, nil
}

// GetForeignKeys returns foreign key information for a table.
func (s *Schema) GetForeignKeys(tableName string) ([]ForeignKeyInfo, error) {
	rows, err := s.conn.Query(fmt.Sprintf("PRAGMA foreign_key_list(%s)", quoteIdentifier(tableName)))
	if err != nil {
		return nil, fmt.Errorf("failed to get foreign keys: %w", err)
	}
	defer rows.Close()

	var fks []ForeignKeyInfo
	for rows.Next() {
		var fk ForeignKeyInfo
		var seq int
		var match string
		if err := rows.Scan(&fk.ID, &seq, &fk.Table, &fk.From, &fk.To, &fk.OnUpdate, &fk.OnDelete, &match); err != nil {
			return nil, fmt.Errorf("failed to scan foreign key: %w", err)
		}
		fks = append(fks, fk)
	}
	return fks, rows.Err()
}

// GetRowCount returns the number of rows in a table.
func (s *Schema) GetRowCount(tableName string) (int64, error) {
	var count int64
	err := s.conn.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteIdentifier(tableName))).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count rows: %w", err)
	}
	return count, nil
}

// TableExists checks if a table exists.
func (s *Schema) TableExists(tableName string) (bool, error) {
	var count int
	err := s.conn.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master 
		WHERE type = 'table' AND name = ?
	`, tableName).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check table existence: %w", err)
	}
	return count > 0, nil
}

// ListViews returns all views in the database.
func (s *Schema) ListViews() ([]string, error) {
	rows, err := s.conn.Query(`
		SELECT name FROM sqlite_master 
		WHERE type = 'view' 
		AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list views: %w", err)
	}
	defer rows.Close()

	var views []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan view name: %w", err)
		}
		views = append(views, name)
	}
	return views, rows.Err()
}

// quoteIdentifier safely quotes a SQL identifier.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
