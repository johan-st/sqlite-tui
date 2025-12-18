package cli

import (
	"fmt"
	"strings"
)

// cmdCreateTable creates a new table.
func (h *Handler) cmdCreateTable(ctx *CommandContext) {
	args := ctx.GetPositionalArgs()
	if len(args) < 2 {
		fmt.Fprintln(ctx.Err, "Usage: create-table <database> <table> --columns=\"col:type[:pk|notnull],..\"")
		fmt.Fprintln(ctx.Err, "   or: create-table <database> <table> --sql=\"CREATE TABLE ...\"")
		ctx.Exit(1)
		return
	}

	dbName := args[0]
	tableName := args[1]

	if !ctx.RequireWrite(dbName) {
		return
	}

	rawSQL := ctx.GetFlag("sql")
	colSpec := ctx.GetFlag("columns")

	var sql string
	if rawSQL != "" {
		sql = rawSQL
	} else if colSpec != "" {
		sql = buildCreateTableSQL(tableName, colSpec)
	} else {
		fmt.Fprintln(ctx.Err, "Error: --columns or --sql is required")
		ctx.Exit(1)
		return
	}

	result, err := h.dbManager.ExecuteQuery(dbName, ctx.User, ctx.GetSessionID(), sql)
	if err != nil {
		fmt.Fprintf(ctx.Err, "Error creating table: %v\n", err)
		ctx.Exit(1)
		return
	}

	format := ctx.GetFlag("format")
	if format == "json" {
		printJSON(ctx.Out, map[string]any{"created": tableName, "rows_affected": result.RowsAffected})
	} else {
		fmt.Fprintf(ctx.Out, "Table '%s' created successfully\n", tableName)
	}

	// Log to audit
	if h.historyStore != nil {
		h.historyStore.RecordAuditSimple(ctx.GetSessionID(), "CREATE_TABLE", dbName, tableName, map[string]any{"sql": sql})
	}
}

// cmdAddColumn adds a column to a table.
func (h *Handler) cmdAddColumn(ctx *CommandContext) {
	args := ctx.GetPositionalArgs()
	if len(args) < 4 {
		fmt.Fprintln(ctx.Err, "Usage: add-column <database> <table> <column> <type> [--default=...] [--notnull]")
		ctx.Exit(1)
		return
	}

	dbName := args[0]
	tableName := args[1]
	colName := args[2]
	colType := args[3]

	if !ctx.RequireWrite(dbName) {
		return
	}

	sql := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
		quoteIdentifier(tableName),
		quoteIdentifier(colName),
		colType)

	if ctx.HasFlag("notnull") {
		sql += " NOT NULL"
	}
	if defaultVal := ctx.GetFlag("default"); defaultVal != "" {
		sql += " DEFAULT " + defaultVal
	}

	_, err := h.dbManager.ExecuteQuery(dbName, ctx.User, ctx.GetSessionID(), sql)
	if err != nil {
		fmt.Fprintf(ctx.Err, "Error adding column: %v\n", err)
		ctx.Exit(1)
		return
	}

	format := ctx.GetFlag("format")
	if format == "json" {
		printJSON(ctx.Out, map[string]any{"added": colName, "table": tableName, "type": colType})
	} else {
		fmt.Fprintf(ctx.Out, "Column '%s' added to table '%s'\n", colName, tableName)
	}

	// Log to audit
	if h.historyStore != nil {
		h.historyStore.RecordAuditSimple(ctx.GetSessionID(), "ADD_COLUMN", dbName, tableName, map[string]any{"sql": sql})
	}
}

// cmdDropTable drops a table.
func (h *Handler) cmdDropTable(ctx *CommandContext) {
	args := ctx.GetPositionalArgs()
	if len(args) < 2 {
		fmt.Fprintln(ctx.Err, "Usage: drop-table <database> <table> --confirm")
		ctx.Exit(1)
		return
	}

	dbName := args[0]
	tableName := args[1]

	if !ctx.RequireWrite(dbName) {
		return
	}

	if !ctx.HasFlag("confirm") {
		fmt.Fprintln(ctx.Err, "Error: --confirm is required to drop a table")
		fmt.Fprintln(ctx.Err, "This will permanently delete the table and all its data.")
		ctx.Exit(1)
		return
	}

	sql := fmt.Sprintf("DROP TABLE %s", quoteIdentifier(tableName))

	_, err := h.dbManager.ExecuteQuery(dbName, ctx.User, ctx.GetSessionID(), sql)
	if err != nil {
		fmt.Fprintf(ctx.Err, "Error dropping table: %v\n", err)
		ctx.Exit(1)
		return
	}

	format := ctx.GetFlag("format")
	if format == "json" {
		printJSON(ctx.Out, map[string]any{"dropped": tableName})
	} else {
		fmt.Fprintf(ctx.Out, "Table '%s' dropped\n", tableName)
	}

	// Log to audit
	if h.historyStore != nil {
		h.historyStore.RecordAuditSimple(ctx.GetSessionID(), "DROP_TABLE", dbName, tableName, nil)
	}
}

// buildCreateTableSQL builds a CREATE TABLE statement from a column spec.
// Format: "col:type[:modifier],..." where modifier can be pk, notnull, unique, default=val
func buildCreateTableSQL(tableName, colSpec string) string {
	var colDefs []string

	for _, col := range splitTrim(colSpec, ",") {
		parts := splitTrim(col, ":")
		if len(parts) < 2 {
			continue
		}

		name := parts[0]
		typ := parts[1]
		def := quoteIdentifier(name) + " " + typ

		// Parse modifiers
		for i := 2; i < len(parts); i++ {
			mod := strings.ToLower(parts[i])
			switch {
			case mod == "pk":
				def += " PRIMARY KEY"
			case mod == "notnull":
				def += " NOT NULL"
			case mod == "unique":
				def += " UNIQUE"
			case strings.HasPrefix(mod, "default="):
				def += " DEFAULT " + mod[8:]
			}
		}

		colDefs = append(colDefs, def)
	}

	return fmt.Sprintf("CREATE TABLE %s (%s)", quoteIdentifier(tableName), strings.Join(colDefs, ", "))
}

