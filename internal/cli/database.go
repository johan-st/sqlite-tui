package cli

import (
	"fmt"
	"path/filepath"

	"github.com/dustin/go-humanize"
	"github.com/johan-st/sqlite-tui/internal/database"
)

// cmdList lists accessible databases.
func (h *Handler) cmdList(ctx *CommandContext) {
	format := ctx.GetFlag("format")
	databases := h.dbManager.ListDatabases(ctx.User)

	if format == "json" {
		printJSON(ctx.Out, databases)
		return
	}

	if len(databases) == 0 {
		fmt.Fprintln(ctx.Out, "No accessible databases found.")
		return
	}

	// Simple tab-separated output
	fmt.Fprintln(ctx.Out, "ALIAS\tPATH\tSIZE\tACCESS")
	for _, db := range databases {
		fmt.Fprintf(ctx.Out, "%s\t%s\t%s\t%s\n",
			db.Alias,
			db.Path,
			humanize.Bytes(uint64(db.Size)),
			db.AccessLevel.String())
	}
}

// cmdInfo shows information about a specific database.
func (h *Handler) cmdInfo(ctx *CommandContext) {
	dbName, ok := ctx.RequireArg(0, "database")
	if !ok {
		return
	}

	if !ctx.RequireRead(dbName) {
		return
	}

	db := h.dbManager.GetDatabase(dbName)
	if db == nil {
		fmt.Fprintf(ctx.Err, "Database not found: %s\n", dbName)
		ctx.Exit(1)
		return
	}

	format := ctx.GetFlag("format")
	if format == "json" {
		info := map[string]any{
			"alias":       db.Alias,
			"path":        db.Path,
			"description": db.Description,
			"size":        db.Size,
			"mod_time":    db.ModTime,
			"access":      h.dbManager.GetAccessLevel(ctx.User, dbName).String(),
		}
		printJSON(ctx.Out, info)
		return
	}

	fmt.Fprintf(ctx.Out, "Alias:\t%s\n", db.Alias)
	fmt.Fprintf(ctx.Out, "Path:\t%s\n", db.Path)
	if db.Description != "" {
		fmt.Fprintf(ctx.Out, "Description:\t%s\n", db.Description)
	}
	fmt.Fprintf(ctx.Out, "Size:\t%s\n", humanize.Bytes(uint64(db.Size)))
	fmt.Fprintf(ctx.Out, "Access:\t%s\n", h.dbManager.GetAccessLevel(ctx.User, dbName).String())

	// Get table count
	conn, err := h.dbManager.OpenConnection(dbName, ctx.User)
	if err == nil {
		schema := database.NewSchema(conn)
		tables, err := schema.ListTables()
		if err == nil {
			fmt.Fprintf(ctx.Out, "Tables:\t%d\n", len(tables))
		}
	}
}

// cmdTables lists tables in a database.
func (h *Handler) cmdTables(ctx *CommandContext) {
	dbName, ok := ctx.RequireArg(0, "database")
	if !ok {
		return
	}

	if !ctx.RequireRead(dbName) {
		return
	}

	conn, err := h.dbManager.OpenConnection(dbName, ctx.User)
	if err != nil {
		fmt.Fprintf(ctx.Err, "Failed to open database: %v\n", err)
		ctx.Exit(1)
		return
	}

	schema := database.NewSchema(conn)
	tables, err := schema.ListTables()
	if err != nil {
		fmt.Fprintf(ctx.Err, "Failed to list tables: %v\n", err)
		ctx.Exit(1)
		return
	}

	format := ctx.GetFlag("format")
	if format == "json" {
		result := make([]map[string]any, 0, len(tables))
		for _, table := range tables {
			info, _ := schema.GetTableInfo(table)
			if info != nil {
				result = append(result, map[string]any{
					"name":    info.Name,
					"columns": len(info.Columns),
					"rows":    info.RowCount,
				})
			}
		}
		printJSON(ctx.Out, result)
		return
	}

	if len(tables) == 0 {
		fmt.Fprintln(ctx.Out, "No tables found.")
		return
	}

	fmt.Fprintln(ctx.Out, "TABLE\tCOLUMNS\tROWS")
	for _, table := range tables {
		info, err := schema.GetTableInfo(table)
		if err != nil {
			fmt.Fprintf(ctx.Out, "%s\t?\t?\n", table)
			continue
		}
		fmt.Fprintf(ctx.Out, "%s\t%d\t%d\n", info.Name, len(info.Columns), info.RowCount)
	}
}

// cmdSchema shows the schema of a table.
func (h *Handler) cmdSchema(ctx *CommandContext) {
	args := ctx.GetPositionalArgs()
	if len(args) < 2 {
		fmt.Fprintln(ctx.Err, "Usage: schema <database> <table>")
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

	schema := database.NewSchema(conn)
	info, err := schema.GetTableInfo(tableName)
	if err != nil {
		fmt.Fprintf(ctx.Err, "Failed to get table info: %v\n", err)
		ctx.Exit(1)
		return
	}

	format := ctx.GetFlag("format")
	if format == "json" {
		result := map[string]any{
			"name":        info.Name,
			"sql":         info.SQL,
			"columns":     info.Columns,
			"primary_key": info.PrimaryKey,
			"row_count":   info.RowCount,
		}

		// Get indexes
		indexes, _ := schema.GetIndexes(tableName)
		result["indexes"] = indexes

		// Get foreign keys
		fks, _ := schema.GetForeignKeys(tableName)
		result["foreign_keys"] = fks

		printJSON(ctx.Out, result)
		return
	}

	fmt.Fprintf(ctx.Out, "Table: %s\n", info.Name)
	fmt.Fprintf(ctx.Out, "Rows: %d\n\n", info.RowCount)

	fmt.Fprintln(ctx.Out, "Columns:")
	fmt.Fprintln(ctx.Out, "NAME\tTYPE\tNULLABLE\tDEFAULT\tPK")
	for _, col := range info.Columns {
		nullable := "YES"
		if col.NotNull {
			nullable = "NO"
		}
		defaultVal := ""
		if col.DefaultValue.Valid {
			defaultVal = col.DefaultValue.String
		}
		pk := ""
		if col.PrimaryKey > 0 {
			pk = fmt.Sprintf("%d", col.PrimaryKey)
		}
		fmt.Fprintf(ctx.Out, "%s\t%s\t%s\t%s\t%s\n",
			col.Name, col.Type, nullable, defaultVal, pk)
	}

	// Get indexes
	indexes, err := schema.GetIndexes(tableName)
	if err == nil && len(indexes) > 0 {
		fmt.Fprintln(ctx.Out, "\nIndexes:")
		fmt.Fprintln(ctx.Out, "NAME\tUNIQUE\tCOLUMNS")
		for _, idx := range indexes {
			unique := "NO"
			if idx.Unique {
				unique = "YES"
			}
			fmt.Fprintf(ctx.Out, "%s\t%s\t%s\n",
				idx.Name, unique, joinStrings(idx.Columns, ", "))
		}
	}

	// Get foreign keys
	fks, err := schema.GetForeignKeys(tableName)
	if err == nil && len(fks) > 0 {
		fmt.Fprintln(ctx.Out, "\nForeign Keys:")
		fmt.Fprintln(ctx.Out, "FROM\tTO\tON_UPDATE\tON_DELETE")
		for _, fk := range fks {
			fmt.Fprintf(ctx.Out, "%s\t%s.%s\t%s\t%s\n",
				fk.From, fk.Table, fk.To, fk.OnUpdate, fk.OnDelete)
		}
	}

	if info.SQL != "" {
		fmt.Fprintf(ctx.Out, "\nDDL:\n%s\n", info.SQL)
	}
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}

// getDBFileName extracts the filename from a database path.
func getDBFileName(path string) string {
	return filepath.Base(path)
}
