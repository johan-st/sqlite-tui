package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// QueryResult holds the results of a query execution.
type QueryResult struct {
	Columns      []string
	Rows         [][]any
	RowsAffected int64
	LastInsertID int64
	Duration     time.Duration
	IsSelect     bool
	Error        string
}

// Query executes a query and returns structured results.
func Query(conn *Connection, query string, args ...any) (*QueryResult, error) {
	start := time.Now()
	trimmed := strings.TrimSpace(strings.ToUpper(query))

	// Determine if this is a SELECT-like query
	isSelect := strings.HasPrefix(trimmed, "SELECT") ||
		strings.HasPrefix(trimmed, "PRAGMA") ||
		strings.HasPrefix(trimmed, "EXPLAIN") ||
		strings.HasPrefix(trimmed, "WITH")

	if isSelect {
		return executeSelect(conn, query, args, start)
	}
	return executeExec(conn, query, args, start)
}

// executeSelect runs a query that returns rows.
func executeSelect(conn *Connection, query string, args []any, start time.Time) (*QueryResult, error) {
	rows, err := conn.Query(query, args...)
	if err != nil {
		return &QueryResult{
			Duration: time.Since(start),
			IsSelect: true,
			Error:    err.Error(),
		}, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	result := &QueryResult{
		Columns:  columns,
		Rows:     make([][]any, 0),
		Duration: 0,
		IsSelect: true,
	}

	for rows.Next() {
		// Create scan destinations
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Convert []byte to string for readability
		row := make([]any, len(columns))
		for i, v := range values {
			switch val := v.(type) {
			case []byte:
				row[i] = string(val)
			default:
				row[i] = val
			}
		}
		result.Rows = append(result.Rows, row)
	}

	result.Duration = time.Since(start)

	if err := rows.Err(); err != nil {
		result.Error = err.Error()
		return result, err
	}

	return result, nil
}

// executeExec runs a query that modifies data.
func executeExec(conn *Connection, query string, args []any, start time.Time) (*QueryResult, error) {
	sqlResult, err := conn.Execute(query, args...)
	if err != nil {
		return &QueryResult{
			Duration: time.Since(start),
			IsSelect: false,
			Error:    err.Error(),
		}, err
	}

	result := &QueryResult{
		Duration: time.Since(start),
		IsSelect: false,
	}

	result.RowsAffected, _ = sqlResult.RowsAffected()
	result.LastInsertID, _ = sqlResult.LastInsertId()

	return result, nil
}

// SelectOptions configures a SELECT query.
type SelectOptions struct {
	Columns []string
	Where   string
	OrderBy string
	Limit   int
	Offset  int
	Args    []any
}

// DefaultSelectOptions returns default options for browsing.
func DefaultSelectOptions() SelectOptions {
	return SelectOptions{
		Limit:  100,
		Offset: 0,
	}
}

// Select retrieves rows from a table with options.
func Select(conn *Connection, tableName string, opts SelectOptions) (*QueryResult, error) {
	// Build column list
	cols := "*"
	if len(opts.Columns) > 0 {
		quoted := make([]string, len(opts.Columns))
		for i, c := range opts.Columns {
			quoted[i] = quoteIdentifier(c)
		}
		cols = strings.Join(quoted, ", ")
	}

	// Build query
	query := fmt.Sprintf("SELECT %s FROM %s", cols, quoteIdentifier(tableName))

	args := make([]any, 0)
	if opts.Where != "" {
		query += " WHERE " + opts.Where
		args = append(args, opts.Args...)
	}

	if opts.OrderBy != "" {
		query += " ORDER BY " + opts.OrderBy
	}

	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	if opts.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", opts.Offset)
	}

	return Query(conn, query, args...)
}

// Insert inserts a row into a table.
func Insert(conn *Connection, tableName string, data map[string]any) (*QueryResult, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("no data to insert")
	}

	columns := make([]string, 0, len(data))
	placeholders := make([]string, 0, len(data))
	values := make([]any, 0, len(data))

	for col, val := range data {
		columns = append(columns, quoteIdentifier(col))
		placeholders = append(placeholders, "?")
		values = append(values, val)
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quoteIdentifier(tableName),
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "))

	return Query(conn, query, values...)
}

// Update updates rows in a table.
func Update(conn *Connection, tableName string, data map[string]any, where string, whereArgs ...any) (*QueryResult, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("no data to update")
	}

	setParts := make([]string, 0, len(data))
	values := make([]any, 0, len(data)+len(whereArgs))

	for col, val := range data {
		setParts = append(setParts, fmt.Sprintf("%s = ?", quoteIdentifier(col)))
		values = append(values, val)
	}

	values = append(values, whereArgs...)

	query := fmt.Sprintf("UPDATE %s SET %s",
		quoteIdentifier(tableName),
		strings.Join(setParts, ", "))

	if where != "" {
		query += " WHERE " + where
	}

	return Query(conn, query, values...)
}

// Delete deletes rows from a table.
func Delete(conn *Connection, tableName string, where string, whereArgs ...any) (*QueryResult, error) {
	query := fmt.Sprintf("DELETE FROM %s", quoteIdentifier(tableName))

	if where != "" {
		query += " WHERE " + where
	}

	return Query(conn, query, whereArgs...)
}

// UpdateCell updates a single cell value.
func UpdateCell(conn *Connection, tableName, pkColumn string, pkValue any, column string, newValue any) (*QueryResult, error) {
	return Update(conn, tableName,
		map[string]any{column: newValue},
		fmt.Sprintf("%s = ?", quoteIdentifier(pkColumn)),
		pkValue)
}

// GetPrimaryKeyColumn returns the primary key column name(s) for a table.
func GetPrimaryKeyColumn(conn *Connection, tableName string) ([]string, error) {
	schema := NewSchema(conn)
	columns, err := schema.GetColumns(tableName)
	if err != nil {
		return nil, err
	}

	var pks []string
	for _, col := range columns {
		if col.PrimaryKey > 0 {
			pks = append(pks, col.Name)
		}
	}

	// If no explicit PK, SQLite uses rowid
	if len(pks) == 0 {
		pks = []string{"rowid"}
	}

	return pks, nil
}

// FormatValue formats a value for display.
func FormatValue(v any) string {
	if v == nil {
		return "NULL"
	}
	switch val := v.(type) {
	case []byte:
		return string(val)
	case string:
		return val
	case int64:
		return fmt.Sprintf("%d", val)
	case float64:
		return fmt.Sprintf("%g", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case sql.NullString:
		if val.Valid {
			return val.String
		}
		return "NULL"
	case sql.NullInt64:
		if val.Valid {
			return fmt.Sprintf("%d", val.Int64)
		}
		return "NULL"
	case sql.NullFloat64:
		if val.Valid {
			return fmt.Sprintf("%g", val.Float64)
		}
		return "NULL"
	case sql.NullBool:
		if val.Valid {
			if val.Bool {
				return "true"
			}
			return "false"
		}
		return "NULL"
	default:
		return fmt.Sprintf("%v", val)
	}
}
