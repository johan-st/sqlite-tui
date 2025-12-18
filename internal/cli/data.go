package cli

import (
	"encoding/json"
	"fmt"

	"github.com/johan-st/sqlite-tui/internal/database"
)

// cmdInsert inserts a row into a table.
func (h *Handler) cmdInsert(ctx *CommandContext) {
	args := ctx.GetPositionalArgs()
	if len(args) < 2 {
		fmt.Fprintln(ctx.Err, "Usage: insert <database> <table> --json='{\"col\":\"val\"}'")
		ctx.Exit(1)
		return
	}

	dbName := args[0]
	tableName := args[1]

	if !ctx.RequireWrite(dbName) {
		return
	}

	jsonData := ctx.GetFlag("json")
	if jsonData == "" {
		fmt.Fprintln(ctx.Err, "Error: --json flag is required")
		ctx.Exit(1)
		return
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		fmt.Fprintf(ctx.Err, "Error parsing JSON: %v\n", err)
		ctx.Exit(1)
		return
	}

	conn, err := h.dbManager.OpenConnection(dbName, ctx.User)
	if err != nil {
		fmt.Fprintf(ctx.Err, "Failed to open database: %v\n", err)
		ctx.Exit(1)
		return
	}

	result, err := database.Insert(conn, tableName, data)
	if err != nil {
		fmt.Fprintf(ctx.Err, "Insert error: %v\n", err)
		ctx.Exit(1)
		return
	}

	format := ctx.GetFlag("format")
	if format == "json" {
		printJSON(ctx.Out, map[string]any{
			"last_insert_id": result.LastInsertID,
			"rows_affected":  result.RowsAffected,
		})
	} else {
		fmt.Fprintf(ctx.Out, "Inserted row with ID: %d\n", result.LastInsertID)
	}

	// Log to audit if history store is available
	if h.historyStore != nil {
		h.historyStore.RecordAuditSimple(ctx.GetSessionID(), "INSERT", dbName, tableName, map[string]any{"data": jsonData})
	}
}

// cmdUpdate updates rows in a table.
func (h *Handler) cmdUpdate(ctx *CommandContext) {
	args := ctx.GetPositionalArgs()
	if len(args) < 2 {
		fmt.Fprintln(ctx.Err, "Usage: update <database> <table> --where=\"...\" --set='{\"col\":\"val\"}'")
		ctx.Exit(1)
		return
	}

	dbName := args[0]
	tableName := args[1]

	if !ctx.RequireWrite(dbName) {
		return
	}

	where := ctx.GetFlag("where")
	if where == "" {
		fmt.Fprintln(ctx.Err, "Error: --where is required to prevent accidental full-table updates")
		ctx.Exit(1)
		return
	}

	setData := ctx.GetFlag("set")
	if setData == "" {
		fmt.Fprintln(ctx.Err, "Error: --set flag is required")
		ctx.Exit(1)
		return
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(setData), &data); err != nil {
		fmt.Fprintf(ctx.Err, "Error parsing JSON: %v\n", err)
		ctx.Exit(1)
		return
	}

	conn, err := h.dbManager.OpenConnection(dbName, ctx.User)
	if err != nil {
		fmt.Fprintf(ctx.Err, "Failed to open database: %v\n", err)
		ctx.Exit(1)
		return
	}

	result, err := database.Update(conn, tableName, data, where)
	if err != nil {
		fmt.Fprintf(ctx.Err, "Update error: %v\n", err)
		ctx.Exit(1)
		return
	}

	format := ctx.GetFlag("format")
	if format == "json" {
		printJSON(ctx.Out, map[string]any{"rows_affected": result.RowsAffected})
	} else {
		fmt.Fprintf(ctx.Out, "Updated %d row(s)\n", result.RowsAffected)
	}

	// Log to audit
	if h.historyStore != nil {
		h.historyStore.RecordAuditSimple(ctx.GetSessionID(), "UPDATE", dbName, tableName,
			map[string]any{"where": where, "set": setData})
	}
}

// cmdDelete deletes rows from a table.
func (h *Handler) cmdDelete(ctx *CommandContext) {
	args := ctx.GetPositionalArgs()
	if len(args) < 2 {
		fmt.Fprintln(ctx.Err, "Usage: delete <database> <table> --where=\"...\" --confirm")
		ctx.Exit(1)
		return
	}

	dbName := args[0]
	tableName := args[1]

	if !ctx.RequireWrite(dbName) {
		return
	}

	if !ctx.HasFlag("confirm") && !ctx.HasFlag("force") {
		fmt.Fprintln(ctx.Err, "Error: --confirm is required to prevent accidental deletes")
		ctx.Exit(1)
		return
	}

	where := ctx.GetFlag("where")
	if where == "" {
		fmt.Fprintln(ctx.Err, "Error: --where is required to prevent accidental full-table deletes")
		ctx.Exit(1)
		return
	}

	conn, err := h.dbManager.OpenConnection(dbName, ctx.User)
	if err != nil {
		fmt.Fprintf(ctx.Err, "Failed to open database: %v\n", err)
		ctx.Exit(1)
		return
	}

	result, err := database.Delete(conn, tableName, where)
	if err != nil {
		fmt.Fprintf(ctx.Err, "Delete error: %v\n", err)
		ctx.Exit(1)
		return
	}

	format := ctx.GetFlag("format")
	if format == "json" {
		printJSON(ctx.Out, map[string]any{"rows_affected": result.RowsAffected})
	} else {
		fmt.Fprintf(ctx.Out, "Deleted %d row(s)\n", result.RowsAffected)
	}

	// Log to audit
	if h.historyStore != nil {
		h.historyStore.RecordAuditSimple(ctx.GetSessionID(), "DELETE", dbName, tableName,
			map[string]any{"where": where})
	}
}

