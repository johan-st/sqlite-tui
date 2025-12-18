package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

// cmdWhoami shows current user information.
func (h *Handler) cmdWhoami(ctx *CommandContext) {
	if ctx.User == nil {
		fmt.Fprintln(ctx.Out, "Not authenticated")
		return
	}

	format := ctx.GetFlag("format")
	if format == "json" {
		info := map[string]any{
			"name":       ctx.User.DisplayName(),
			"admin":      ctx.User.IsAdmin,
			"anonymous":  ctx.User.IsAnonymous,
			"session_id": ctx.GetSessionID(),
		}
		if ctx.User.PublicKeyFP != "" {
			info["public_key_fp"] = ctx.User.PublicKeyFP
		}
		printJSON(ctx.Out, info)
		return
	}

	fmt.Fprintf(ctx.Out, "User:\t%s\n", ctx.User.DisplayName())
	fmt.Fprintf(ctx.Out, "Admin:\t%v\n", ctx.User.IsAdmin)
	fmt.Fprintf(ctx.Out, "Anonymous:\t%v\n", ctx.User.IsAnonymous)
	if ctx.User.PublicKeyFP != "" {
		fmt.Fprintf(ctx.Out, "Key:\t%s\n", ctx.User.PublicKeyFP)
	}
	fmt.Fprintf(ctx.Out, "Session:\t%s\n", ctx.GetSessionID())
}

// cmdHelp shows help information.
func (h *Handler) cmdHelp(ctx *CommandContext) {
	args := ctx.GetPositionalArgs()

	if len(args) > 0 {
		h.showCommandHelp(ctx, args[0])
		return
	}

	fmt.Fprintln(ctx.Out, `sqlite-tui - Database Studio for SQLite

USAGE:
  ssh host command [arguments] [options]

DATABASE COMMANDS:
  ls, list                         List accessible databases
  info <database>                  Show database information
  tables <database>                List tables in database
  schema <database> <table>        Show table schema

QUERY COMMANDS:
  query <database> "<sql>"         Execute SQL query
  select <database> <table>        Browse table data
  count <database> <table>         Count rows in table

DATA COMMANDS (requires write access):
  insert <database> <table> --json='{"col":"val"}'
  update <database> <table> --where="id=1" --set='{"col":"val"}'
  delete <database> <table> --where="id=1" --confirm

EXPORT COMMANDS:
  export <database> <table>        Export table data
  download <database>              Download raw database file

SCHEMA COMMANDS (requires write access):
  create-table <database> <table>  Create new table
  add-column <database> <table>    Add column to table
  drop-table <database> <table>    Drop table (requires --confirm)

ADMIN COMMANDS (requires admin access):
  sessions                         List active sessions
  history                          View query history
  audit                            View audit log
  reload-config                    Reload configuration

UTILITY COMMANDS:
  whoami                           Show current user info
  help [command]                   Show help
  version                          Show version

COMMON OPTIONS:
  --format=json                    Output in JSON format
  --format=csv                     Output in CSV format
  --limit=N                        Limit number of rows
  --offset=N                       Skip N rows

Run 'help <command>' for detailed help on a specific command.`)
}

// showCommandHelp shows help for a specific command.
func (h *Handler) showCommandHelp(ctx *CommandContext, command string) {
	help := map[string]string{
		"ls": `ls, list - List accessible databases

USAGE:
  ls [--format=json]

OPTIONS:
  --format=json    Output in JSON format`,

		"query": `query - Execute SQL query

USAGE:
  query <database> "<sql>" [options]

OPTIONS:
  --format=json    Output results as JSON
  --format=csv     Output results as CSV
  --format=table   Output results as table (default)

EXAMPLES:
  query mydb "SELECT * FROM users"
  query mydb "SELECT * FROM users WHERE active=1" --format=json`,

		"select": `select - Browse table data

USAGE:
  select <database> <table> [options]

OPTIONS:
  --columns="col1,col2"    Select specific columns
  --where="condition"      Filter rows
  --limit=N                Limit rows (default: 100)
  --offset=N               Skip N rows
  --format=json            Output as JSON
  --format=csv             Output as CSV

EXAMPLES:
  select mydb users
  select mydb users --limit=10 --format=json
  select mydb users --where="active=1" --columns="id,name"`,

		"export": `export - Export table data

USAGE:
  export <database> <table> [options]

OPTIONS:
  --format=csv     Export as CSV (default)
  --format=json    Export as JSON

OUTPUT:
  Data is written to stdout. Redirect to a file:
  ssh host export mydb users --format=csv > users.csv`,

		"download": `download - Download raw database file

USAGE:
  download <database>

Streams the raw SQLite database file to stdout.
Requires at least read access to the database.

EXAMPLE:
  ssh host download mydb > mydb.db`,

		"insert": `insert - Insert a row

USAGE:
  insert <database> <table> --json='{"column":"value"}'

The --json flag should contain a JSON object mapping column names to values.

EXAMPLE:
  insert mydb users --json='{"name":"John","email":"john@example.com"}'`,

		"update": `update - Update rows

USAGE:
  update <database> <table> --where="condition" --set='{"column":"value"}'

Both --where and --set are required.

EXAMPLE:
  update mydb users --where="id=1" --set='{"name":"Jane"}'`,

		"delete": `delete - Delete rows

USAGE:
  delete <database> <table> --where="condition" --confirm

The --confirm or --force flag is required to prevent accidental deletes.

EXAMPLE:
  delete mydb users --where="id=1" --confirm`,
	}

	if h, ok := help[command]; ok {
		fmt.Fprintln(ctx.Out, h)
	} else {
		fmt.Fprintf(ctx.Out, "No detailed help available for '%s'\n", command)
	}
}

// cmdVersion shows version information.
func (h *Handler) cmdVersion(ctx *CommandContext) {
	format := ctx.GetFlag("format")
	if format == "json" {
		printJSON(ctx.Out, map[string]string{"version": h.version})
		return
	}
	fmt.Fprintf(ctx.Out, "sqlite-tui %s\n", h.version)
}

// printJSON writes JSON to a writer.
func printJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

// printCSV writes CSV-like output.
func printCSV(w io.Writer, headers []string, rows [][]string) {
	// Print headers
	for i, h := range headers {
		if i > 0 {
			fmt.Fprint(w, ",")
		}
		fmt.Fprint(w, escapeCSV(h))
	}
	fmt.Fprintln(w)

	// Print rows
	for _, row := range rows {
		for i, val := range row {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprint(w, escapeCSV(val))
		}
		fmt.Fprintln(w)
	}
}

// escapeCSV escapes a value for CSV output.
func escapeCSV(s string) string {
	needsQuotes := false
	for _, c := range s {
		if c == ',' || c == '"' || c == '\n' || c == '\r' {
			needsQuotes = true
			break
		}
	}
	if !needsQuotes {
		return s
	}
	// Escape quotes by doubling them
	escaped := ""
	for _, c := range s {
		if c == '"' {
			escaped += "\"\""
		} else {
			escaped += string(c)
		}
	}
	return "\"" + escaped + "\""
}
