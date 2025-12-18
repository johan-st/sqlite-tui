package cli

import (
	"fmt"

	"github.com/johan-st/sqlite-tui/internal/database"
)

// cmdExport exports table data to stdout.
func (h *Handler) cmdExport(ctx *CommandContext) {
	args := ctx.GetPositionalArgs()
	if len(args) < 2 {
		fmt.Fprintln(ctx.Err, "Usage: export <database> <table> [--format=csv|json]")
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

	// No limit for export - get all rows
	opts := database.SelectOptions{Limit: 0}
	if where := ctx.GetFlag("where"); where != "" {
		opts.Where = where
	}

	result, err := database.Select(conn, tableName, opts)
	if err != nil {
		fmt.Fprintf(ctx.Err, "Query error: %v\n", err)
		ctx.Exit(1)
		return
	}

	format := ctx.GetFlag("format")
	if format == "" {
		format = "csv" // Default to CSV for export
	}

	switch format {
	case "json":
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
		strRows := make([][]string, len(result.Rows))
		for i, row := range result.Rows {
			strRows[i] = make([]string, len(row))
			for j, v := range row {
				strRows[i][j] = database.FormatValue(v)
			}
		}
		printCSV(ctx.Out, result.Columns, strRows)

	default:
		fmt.Fprintf(ctx.Err, "Unknown format: %s (use csv or json)\n", format)
		ctx.Exit(1)
	}
}

// cmdDownload streams the raw database file.
func (h *Handler) cmdDownload(ctx *CommandContext) {
	args := ctx.GetPositionalArgs()
	if len(args) < 1 {
		fmt.Fprintln(ctx.Err, "Usage: download <database>")
		ctx.Exit(1)
		return
	}

	dbName := args[0]

	if !ctx.RequireRead(dbName) {
		return
	}

	if err := h.dbManager.StreamDatabase(dbName, ctx.User, ctx.Out); err != nil {
		fmt.Fprintf(ctx.Err, "Download error: %v\n", err)
		ctx.Exit(1)
		return
	}
}

