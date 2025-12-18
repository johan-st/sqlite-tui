package cli

import (
	"fmt"
	"strconv"

	"github.com/johan-st/sqlite-tui/internal/database"
)

// cmdQuery executes a raw SQL query.
func (h *Handler) cmdQuery(ctx *CommandContext) {
	args := ctx.GetPositionalArgs()
	if len(args) < 2 {
		fmt.Fprintln(ctx.Err, "Usage: query <database> \"<sql>\"")
		ctx.Exit(1)
		return
	}

	dbName := args[0]
	sql := args[1]

	if !ctx.RequireRead(dbName) {
		return
	}

	// Check write access for non-SELECT queries
	if !isReadOnlyQuery(sql) && !ctx.RequireWrite(dbName) {
		return
	}

	result, err := h.dbManager.ExecuteQuery(dbName, ctx.User, ctx.GetSessionID(), sql)
	if err != nil {
		fmt.Fprintf(ctx.Err, "Query error: %v\n", err)
		ctx.Exit(1)
		return
	}

	format := ctx.GetFlag("format")
	formatQueryResult(ctx, result, format)
}

// cmdSelect browses table data.
func (h *Handler) cmdSelect(ctx *CommandContext) {
	args := ctx.GetPositionalArgs()
	if len(args) < 2 {
		fmt.Fprintln(ctx.Err, "Usage: select <database> <table> [--where=...] [--limit=N] [--offset=N]")
		ctx.Exit(1)
		return
	}

	dbName := args[0]
	tableName := args[1]

	if !ctx.RequireRead(dbName) {
		return
	}

	conn, err := h.dbManager.OpenConnection(dbName, ctx.User)
	if err != nil {
		fmt.Fprintf(ctx.Err, "Failed to open database: %v\n", err)
		ctx.Exit(1)
		return
	}

	opts := database.DefaultSelectOptions()

	if cols := ctx.GetFlag("columns"); cols != "" {
		opts.Columns = parseColumns(cols)
	}
	if where := ctx.GetFlag("where"); where != "" {
		opts.Where = where
	}
	if limit := ctx.GetFlag("limit"); limit != "" {
		if n, err := strconv.Atoi(limit); err == nil {
			opts.Limit = n
		}
	}
	if offset := ctx.GetFlag("offset"); offset != "" {
		if n, err := strconv.Atoi(offset); err == nil {
			opts.Offset = n
		}
	}

	result, err := database.Select(conn, tableName, opts)
	if err != nil {
		fmt.Fprintf(ctx.Err, "Query error: %v\n", err)
		ctx.Exit(1)
		return
	}

	format := ctx.GetFlag("format")
	formatQueryResult(ctx, result, format)
}

// cmdCount counts rows in a table.
func (h *Handler) cmdCount(ctx *CommandContext) {
	args := ctx.GetPositionalArgs()
	if len(args) < 2 {
		fmt.Fprintln(ctx.Err, "Usage: count <database> <table> [--where=...]")
		ctx.Exit(1)
		return
	}

	dbName := args[0]
	tableName := args[1]

	if !ctx.RequireRead(dbName) {
		return
	}

	conn, err := h.dbManager.OpenConnection(dbName, ctx.User)
	if err != nil {
		fmt.Fprintf(ctx.Err, "Failed to open database: %v\n", err)
		ctx.Exit(1)
		return
	}

	where := ctx.GetFlag("where")
	var query string
	if where != "" {
		query = fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", quoteIdentifier(tableName), where)
	} else {
		query = fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteIdentifier(tableName))
	}

	result, err := database.Query(conn, query)
	if err != nil {
		fmt.Fprintf(ctx.Err, "Query error: %v\n", err)
		ctx.Exit(1)
		return
	}

	// Output just the count
	if len(result.Rows) > 0 && len(result.Rows[0]) > 0 {
		format := ctx.GetFlag("format")
		if format == "json" {
			printJSON(ctx.Out, map[string]any{"count": result.Rows[0][0]})
		} else {
			fmt.Fprintln(ctx.Out, result.Rows[0][0])
		}
	}
}

// formatQueryResult formats and outputs a query result.
func formatQueryResult(ctx *CommandContext, result *database.QueryResult, format string) {
	switch format {
	case "json":
		// Convert to JSON-friendly format
		rows := make([]map[string]any, 0, len(result.Rows))
		for _, row := range result.Rows {
			m := make(map[string]any)
			for i, col := range result.Columns {
				if i < len(row) {
					m[col] = row[i]
				}
			}
			rows = append(rows, m)
		}
		printJSON(ctx.Out, rows)

	case "csv":
		// Convert rows to strings
		strRows := make([][]string, len(result.Rows))
		for i, row := range result.Rows {
			strRows[i] = make([]string, len(row))
			for j, v := range row {
				strRows[i][j] = database.FormatValue(v)
			}
		}
		printCSV(ctx.Out, result.Columns, strRows)

	default:
		// Table format
		if len(result.Columns) == 0 {
			if result.RowsAffected > 0 {
				fmt.Fprintf(ctx.Out, "Rows affected: %d\n", result.RowsAffected)
			}
			return
		}

		// Print headers
		for i, col := range result.Columns {
			if i > 0 {
				fmt.Fprint(ctx.Out, "\t")
			}
			fmt.Fprint(ctx.Out, col)
		}
		fmt.Fprintln(ctx.Out)

		// Print rows
		for _, row := range result.Rows {
			for i, v := range row {
				if i > 0 {
					fmt.Fprint(ctx.Out, "\t")
				}
				fmt.Fprint(ctx.Out, database.FormatValue(v))
			}
			fmt.Fprintln(ctx.Out)
		}
	}
}

// parseColumns splits a comma-separated column list.
func parseColumns(s string) []string {
	if s == "" {
		return nil
	}
	var cols []string
	for _, c := range splitTrim(s, ",") {
		if c != "" {
			cols = append(cols, c)
		}
	}
	return cols
}

// splitTrim splits a string and trims whitespace from each part.
func splitTrim(s, sep string) []string {
	parts := make([]string, 0)
	for _, p := range split(s, sep) {
		p = trim(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func split(s, sep string) []string {
	var result []string
	start := 0
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
		}
	}
	result = append(result, s[start:])
	return result
}

func trim(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// quoteIdentifier safely quotes a SQL identifier.
func quoteIdentifier(name string) string {
	// Replace double quotes with escaped double quotes
	escaped := ""
	for _, c := range name {
		if c == '"' {
			escaped += "\"\""
		} else {
			escaped += string(c)
		}
	}
	return `"` + escaped + `"`
}

// isReadOnlyQuery checks if a query is read-only.
func isReadOnlyQuery(query string) bool {
	upper := toUpper(trim(query))
	return hasPrefix(upper, "SELECT") ||
		hasPrefix(upper, "PRAGMA") ||
		hasPrefix(upper, "EXPLAIN") ||
		hasPrefix(upper, "WITH")
}

func toUpper(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		} else {
			b[i] = c
		}
	}
	return string(b)
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

