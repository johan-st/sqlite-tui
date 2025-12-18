# SQLite TUI SSH Integration Plan

## Usage Modes

### Local Mode (single user, admin)

```bash
# TUI mode - interactive
sqlite-tui mydb.db
sqlite-tui ./databases/
sqlite-tui "./data/*.db"

# CLI mode - run command and exit
sqlite-tui mydb.db ls
sqlite-tui mydb.db tables mydb
sqlite-tui mydb.db query mydb "SELECT * FROM users"
sqlite-tui ./data/*.db export mydb users --format=csv > users.csv
```

- User is automatically **admin** with full read-write access
- No SSH, no authentication
- Path argument is required (file, directory, or glob)
- No config file needed for basic usage
- If command args follow path → CLI mode (run and exit)
- If no command args → TUI mode (interactive)

### SSH Server Mode (multi-user)

```bash
# Start SSH server with config
sqlite-tui -ssh -config config.yaml
```

- Multi-user with public key authentication
- Per-user access control (none/read-only/read-write/admin)
- Supports both TUI (interactive) and CLI (commands)
- Config file defines databases, users, permissions

---

## Current State

The project has solid foundations already in place:

| Component | Location | Status |
|-----------|----------|--------|
| SSH Server | `internal/server/server.go` | Wish-based, middleware chain, routing ready |
| Authentication | `internal/server/auth.go` | Public key + keyboard-interactive |
| CLI Router | `internal/cli/cli.go` | 20 commands routed, 7 implemented |
| Database Layer | `internal/database/` | Discovery, connections, locking, CRUD working |
| History Store | `internal/history/store.go` | Sessions, queries, audit log schema ready |
| Config | `internal/config/config.go` | YAML parsing, hot-reload, access resolver |
| TUI Skeleton | `internal/tui/` | Empty directories only |

---

## Phase 1: Fix Module Path and Wire SSH Server

### 1.1 Update Module Path

Change `go.mod` from `github.com/johan-st/sqlite-tui` to `github.com/johan-st/sqlite-tui`.

**Files requiring import updates (13 total):**

| File | Imports to Update |
|------|-------------------|
| `cmd/sqlite-tui/main.go` | access, config, database, history |
| `internal/cli/cli.go` | access, database, history, server |
| `internal/cli/database.go` | database |
| `internal/config/config.go` | access |
| `internal/config/access.go` | access |
| `internal/database/manager.go` | access, config |
| `internal/database/discovery.go` | config |
| `internal/server/server.go` | config, database, history |
| `internal/server/auth.go` | access, config, history |
| `internal/server/middleware.go` | access, database, history |
| `internal/server/session.go` | access, history |
| `internal/history/session.go` | access |
| `go.mod` | module declaration |

### 1.2 Wire RunSSHServer() in main.go

Replace stub with actual server wiring:

```go
// cmd/sqlite-tui/main.go

func (a *App) RunSSHServer() error {
    // Create CLI handler
    cliHandler := cli.NewHandler(a.DBManager, a.HistoryStore, version)
    
    // Create SSH server
    sshServer := server.NewServer(a.Config, a.DBManager, a.HistoryStore)
    sshServer.SetCLIHandler(cliHandler.Handle)
    // sshServer.SetTUIHandler(tui.Handler(...)) // Phase 4
    
    return sshServer.ListenAndServe()
}
```

---

## Phase 2: Implement Remaining CLI Commands

### Local CLI Support

Add to `internal/cli/cli.go`:

```go
// LocalContext wraps command execution for local (non-SSH) mode
type LocalContext struct {
    User  *access.UserInfo
    Args  []string
    Out   io.Writer
    Err   io.Writer
}

func NewLocalContext(user *access.UserInfo, args []string, out, err io.Writer) *LocalContext {
    return &LocalContext{User: user, Args: args, Out: out, Err: err}
}

// HandleLocal processes a command in local mode (no SSH session)
func (h *Handler) HandleLocal(ctx *LocalContext) {
    if len(ctx.Args) == 0 {
        fmt.Fprintln(ctx.Out, "No command specified. Run 'help' for usage.")
        return
    }
    
    // Create CommandContext compatible with existing handlers
    cmdCtx := &CommandContext{
        Session:      nil,  // No SSH session
        User:         ctx.User,
        SessionInfo:  nil,
        DBManager:    h.dbManager,
        HistoryStore: h.historyStore,
        Args:         ctx.Args[1:],
        Out:          ctx.Out,
        Err:          ctx.Err,
    }
    
    // Route command (same switch as Handle)
    h.routeCommand(ctx.Args[0], cmdCtx)
}

// routeCommand is shared between SSH and local handlers
func (h *Handler) routeCommand(cmd string, ctx *CommandContext) {
    switch cmd {
    case "ls", "list":
        h.cmdList(ctx)
    // ... rest of switch cases
    }
}
```

### Existing Infrastructure

The `database` package already provides these functions:

| Function | File | Purpose |
|----------|------|---------|
| `Query(conn, sql, args...)` | `query.go` | Execute any SQL, return `*QueryResult` |
| `Select(conn, table, opts)` | `query.go` | SELECT with WHERE/LIMIT/OFFSET |
| `Insert(conn, table, data)` | `query.go` | INSERT from `map[string]any` |
| `Update(conn, table, data, where, args...)` | `query.go` | UPDATE with WHERE clause |
| `Delete(conn, table, where, args...)` | `query.go` | DELETE with WHERE clause |
| `NewSchema(conn).ListTables()` | `schema.go` | Get table list |
| `NewSchema(conn).GetTableInfo(name)` | `schema.go` | Get columns, PKs, row count |

### 2.1 Query Commands

Create `internal/cli/query.go`:

#### cmdQuery
```go
func (h *Handler) cmdQuery(ctx *CommandContext) {
    // Args: <database> "<sql>"
    dbName, ok := ctx.RequireArg(0, "database")
    sql, ok := ctx.RequireArg(1, "sql")
    if !ok { return }
    
    if !ctx.RequireRead(dbName) { return }
    
    // Check write access for non-SELECT
    if !database.IsReadOnlyQuery(sql) && !ctx.RequireWrite(dbName) { return }
    
    result, err := h.dbManager.ExecuteQuery(dbName, ctx.User, ctx.GetSessionID(), sql)
    // Handle error, format output (json/csv/table)
    
    // Log to history
    h.historyStore.LogQuery(ctx.GetSessionID(), dbPath, sql, result.Duration, result.Error)
}
```

#### cmdSelect
```go
func (h *Handler) cmdSelect(ctx *CommandContext) {
    // Args: <database> <table>
    // Flags: --columns, --where, --limit, --offset, --format
    
    opts := database.DefaultSelectOptions()
    opts.Columns = parseColumns(ctx.GetFlag("columns"))
    opts.Where = ctx.GetFlag("where")
    opts.Limit = parseIntFlag(ctx.GetFlag("limit"), 100)
    opts.Offset = parseIntFlag(ctx.GetFlag("offset"), 0)
    
    conn, _ := h.dbManager.OpenConnection(dbName, ctx.User)
    result, _ := database.Select(conn, tableName, opts)
    // Format and output
}
```

#### cmdCount
```go
func (h *Handler) cmdCount(ctx *CommandContext) {
    // Args: <database> <table> [--where="..."]
    // Example: count mydb users --where="active=1"
    
    where := ctx.GetFlag("where")
    
    var query string
    if where != "" {
        query = fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", 
            quoteIdentifier(tableName), where)
    } else {
        query = fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteIdentifier(tableName))
    }
    
    result, _ := h.dbManager.ExecuteQuery(dbName, ctx.User, sessionID, query)
    // Output: just the count number
}
```

### 2.2 Data Modification Commands

Create `internal/cli/data.go`:

#### cmdInsert
```go
func (h *Handler) cmdInsert(ctx *CommandContext) {
    // Args: <database> <table> --json='{"col":"val"}'
    jsonData := ctx.GetFlag("json")
    
    var data map[string]any
    json.Unmarshal([]byte(jsonData), &data)
    
    conn, _ := h.dbManager.OpenConnection(dbName, ctx.User)
    result, _ := database.Insert(conn, tableName, data)
    
    // Audit log
    h.historyStore.LogAudit(sessionID, "INSERT", dbPath, tableName, jsonData)
}
```

#### cmdUpdate
```go
func (h *Handler) cmdUpdate(ctx *CommandContext) {
    // Args: <database> <table> --where="..." --set='{"col":"val"}'
    where := ctx.GetFlag("where")
    if where == "" {
        fmt.Fprintln(ctx.Err, "Error: --where is required to prevent accidental full-table updates")
        return
    }
    
    setData := ctx.GetFlag("set")
    var data map[string]any
    json.Unmarshal([]byte(setData), &data)
    
    result, _ := database.Update(conn, tableName, data, where)
}
```

#### cmdDelete
```go
func (h *Handler) cmdDelete(ctx *CommandContext) {
    // Args: <database> <table> --where="..." --confirm
    if !ctx.HasFlag("confirm") && !ctx.HasFlag("force") {
        fmt.Fprintln(ctx.Err, "Error: --confirm required")
        return
    }
    
    where := ctx.GetFlag("where")
    result, _ := database.Delete(conn, tableName, where)
}
```

### 2.3 Export Commands

Create `internal/cli/export.go`:

#### cmdExport
```go
func (h *Handler) cmdExport(ctx *CommandContext) {
    // Stream table data as CSV or JSON
    format := ctx.GetFlag("format")
    if format == "" { format = "csv" }
    
    opts := database.SelectOptions{Limit: 0} // No limit
    result, _ := database.Select(conn, tableName, opts)
    
    switch format {
    case "json":
        // Stream JSON array
    case "csv":
        printCSV(ctx.Out, result.Columns, result.Rows)
    }
}
```

#### cmdDownload
```go
func (h *Handler) cmdDownload(ctx *CommandContext) {
    // Stream raw .db file using existing StreamDatabase
    err := h.dbManager.StreamDatabase(dbName, ctx.User, ctx.Out)
}
```

### 2.4 Schema Commands

Create `internal/cli/schema_cmd.go`:

#### cmdCreateTable
```go
func (h *Handler) cmdCreateTable(ctx *CommandContext) {
    // Args: <database> <table> --columns="col:type[:pk|notnull],..."
    // Example: create-table mydb users --columns="id:integer:pk,name:text:notnull,email:text"
    //
    // Or use raw SQL:
    // Example: create-table mydb users --sql="CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)"
    
    colSpec := ctx.GetFlag("columns")
    rawSQL := ctx.GetFlag("sql")
    
    var sql string
    if rawSQL != "" {
        sql = rawSQL
    } else if colSpec != "" {
        // Parse columns spec: "id:integer:pk,name:text:notnull,email:text"
        columns := parseColumnSpec(colSpec)
        sql = buildCreateTableSQL(tableName, columns)
    } else {
        fmt.Fprintln(ctx.Err, "Error: --columns or --sql required")
        return
    }
    
    result, _ := h.dbManager.ExecuteQuery(dbName, ctx.User, sessionID, sql)
}

// Column spec format: "name:type[:modifier,...]"
// Modifiers: pk (primary key), notnull, unique, default=value
// Example: "id:integer:pk,name:text:notnull,count:integer:default=0"
func parseColumnSpec(spec string) []ColumnDef { ... }
```

#### cmdAddColumn
```go
func (h *Handler) cmdAddColumn(ctx *CommandContext) {
    // Args: <database> <table> <column> <type> [--default=val] [--notnull]
    // Example: add-column mydb users age INTEGER
    // Example: add-column mydb users status TEXT --default="active"
    
    args := ctx.GetPositionalArgs()
    if len(args) < 4 {
        fmt.Fprintln(ctx.Err, "Usage: add-column <database> <table> <column> <type> [--default=val]")
        return
    }
    
    dbName, tableName, colName, colType := args[0], args[1], args[2], args[3]
    defaultVal := ctx.GetFlag("default")
    notNull := ctx.HasFlag("notnull")
    
    sql := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
        quoteIdentifier(tableName),
        quoteIdentifier(colName),
        colType)
    
    if notNull {
        sql += " NOT NULL"
    }
    if defaultVal != "" {
        sql += " DEFAULT " + defaultVal
    }
    
    result, _ := h.dbManager.ExecuteQuery(dbName, ctx.User, sessionID, sql)
}
```

#### cmdDropTable
```go
func (h *Handler) cmdDropTable(ctx *CommandContext) {
    // Args: <database> <table> --confirm
    // Example: drop-table mydb old_users --confirm
    
    if !ctx.HasFlag("confirm") {
        fmt.Fprintln(ctx.Err, "Error: --confirm required to drop table")
        fmt.Fprintln(ctx.Err, "This will permanently delete the table and all its data.")
        return
    }
    
    sql := fmt.Sprintf("DROP TABLE %s", quoteIdentifier(tableName))
    result, _ := h.dbManager.ExecuteQuery(dbName, ctx.User, sessionID, sql)
}
```

### 2.5 Admin Commands

Create `internal/cli/admin.go`:

#### cmdSessions
```go
func (h *Handler) cmdSessions(ctx *CommandContext) {
    if !ctx.RequireAdmin() { return }
    
    // Get from session manager via middleware context
    sessionMgr := server.GetSessionMgrFromSSH(ctx.Session)
    sessions := sessionMgr.ListActiveSessions()
    
    // Format: ID, User, RemoteAddr, Duration, Idle
}
```

#### cmdHistory
```go
func (h *Handler) cmdHistory(ctx *CommandContext) {
    if !ctx.RequireAdmin() { return }
    
    limit := parseIntFlag(ctx.GetFlag("limit"), 50)
    queries, _ := h.historyStore.GetRecentQueries(limit)
    // Or filter by --user, --database
}
```

#### cmdAudit
```go
func (h *Handler) cmdAudit(ctx *CommandContext) {
    if !ctx.RequireAdmin() { return }
    
    entries, _ := h.historyStore.GetAuditLog(limit, filters...)
}
```

#### cmdReloadConfig
```go
func (h *Handler) cmdReloadConfig(ctx *CommandContext) {
    if !ctx.RequireAdmin() { return }
    
    // Trigger config reload - need access to config watcher
    // Could use a channel or direct method on App
    fmt.Fprintln(ctx.Out, "Configuration reloaded")
}
```

### Command Status

| Command | Status | Usage | Notes |
|---------|--------|-------|-------|
| `ls` | ✅ Done | `ls [--format=json]` | List databases |
| `info` | ✅ Done | `info <db>` | Database info |
| `tables` | ✅ Done | `tables <db>` | List tables |
| `schema` | ✅ Done | `schema <db> <table>` | Show schema |
| `whoami` | ✅ Done | `whoami` | Current user |
| `help` | ✅ Done | `help [cmd]` | Show help |
| `version` | ✅ Done | `version` | Show version |
| `query` | ❌ TODO | `query <db> "<sql>"` | Raw SQL |
| `select` | ❌ TODO | `select <db> <table> [--where=...] [--limit=N]` | Browse data |
| `count` | ❌ TODO | `count <db> <table> [--where=...]` | Count rows |
| `insert` | ❌ TODO | `insert <db> <table> --json='{...}'` | Insert row |
| `update` | ❌ TODO | `update <db> <table> --where="..." --set='{...}'` | Update rows |
| `delete` | ❌ TODO | `delete <db> <table> --where="..." --confirm` | Delete rows |
| `export` | ❌ TODO | `export <db> <table> [--format=csv\|json]` | Export to stdout |
| `download` | ❌ TODO | `download <db>` | Stream raw .db file |
| `create-table` | ❌ TODO | `create-table <db> <table> --columns="..."` | Create table |
| `add-column` | ❌ TODO | `add-column <db> <table> <col> <type> [--default=...]` | Add column |
| `drop-table` | ❌ TODO | `drop-table <db> <table> --confirm` | Drop table |
| `sessions` | ❌ TODO | `sessions` | List active sessions |
| `history` | ❌ TODO | `history [--limit=N] [--user=...] [--db=...]` | Query history |
| `audit` | ❌ TODO | `audit [--limit=N] [--action=...]` | Audit log |
| `reload-config` | ❌ TODO | `reload-config` | Reload config |

---

## Phase 3: Build TUI (Bubble Tea)

### 3.1 Dependencies

Add to `go.mod`:
```
github.com/charmbracelet/bubbles v0.20.0
github.com/charmbracelet/lipgloss v1.1.0
```

Key bubbles components:
- `bubbles/list` - for database/table lists
- `bubbles/table` - for data display
- `bubbles/textinput` - for SQL editor
- `bubbles/viewport` - for scrollable content
- `bubbles/key` - for key bindings

### 3.2 Architecture

```
┌─────────────────────────────────────────────────────────────┐
│ sqlite-tui                                    user@host     │
├──────────────┬──────────────┬───────────────────────────────┤
│ Databases    │ Tables       │ Data                          │
│   (list)     │   (list)     │   (table)                     │
│              │              │                               │
│ > mydb.db    │ > users      │ id │ name    │ email          │
│   other.db   │   posts      │ 1  │ Alice   │ alice@...      │
│              │   comments   │ 2  │ Bob     │ bob@...        │
│              │              │ 3  │ Charlie │ charlie@...    │
│              │              │                               │
├──────────────┴──────────────┴───────────────────────────────┤
│ SQL> SELECT * FROM users WHERE active = 1                   │
│   (textinput)                                               │
├─────────────────────────────────────────────────────────────┤
│ mydb.db > users │ 3 rows │ RO │ Tab: switch │ ?: help       │
│   (status bar)                                              │
└─────────────────────────────────────────────────────────────┘
```

### 3.3 File Structure

```
internal/tui/
├── app.go              # Root model, tea.Model implementation
├── handler.go          # SSH bubbletea.Handler factory
├── keymap.go           # Key bindings definition
├── styles.go           # lipgloss styles and theme
├── messages.go         # Custom tea.Msg types
├── views/
│   ├── database_list.go    # Database selection pane
│   ├── table_list.go       # Table selection pane  
│   ├── data_view.go        # Data table with pagination
│   ├── query_editor.go     # SQL input and execution
│   └── schema_view.go      # Table schema display (modal)
└── components/
    ├── table.go            # Reusable data table wrapper
    ├── status_bar.go       # Bottom status bar
    └── help.go             # Help overlay (modal)
```

### 3.4 Root App Model

```go
// internal/tui/app.go

type App struct {
    // Dependencies
    dbManager    *database.Manager
    historyStore *history.Store
    user         *access.UserInfo
    
    // Window
    width, height int
    
    // State
    focus        Focus  // DatabaseList, TableList, DataView, QueryEditor
    selectedDB   *database.DatabaseInfo
    selectedTable string
    
    // Child models
    dbList      views.DatabaseList
    tableList   views.TableList
    dataView    views.DataView
    queryEditor views.QueryEditor
    statusBar   components.StatusBar
    help        components.Help
    
    // Modals
    showHelp    bool
    showSchema  bool
    schemaView  views.SchemaView
}

type Focus int
const (
    FocusDatabases Focus = iota
    FocusTables
    FocusData
    FocusQuery
)
```

### 3.5 Key Bindings

| Key | Context | Action |
|-----|---------|--------|
| `Tab` | Global | Cycle focus: DB → Tables → Data → Query |
| `Shift+Tab` | Global | Cycle focus backwards |
| `h/l` or `←/→` | Global | Move focus left/right |
| `j/k` or `↓/↑` | List/Data | Navigate items |
| `Enter` | DB List | Select database, load tables |
| `Enter` | Table List | Select table, load data |
| `Enter` | Query | Execute query |
| `s` | Table List | Show schema modal |
| `r` | Global | Refresh current view |
| `e` | Data View | Edit selected cell (if write access) |
| `d` | Data View | Delete selected row (with confirm) |
| `n` | Data View | New row (insert modal) |
| `/` | Global | Focus query editor |
| `Esc` | Modal | Close modal |
| `Esc` | Query | Clear or unfocus |
| `?` | Global | Toggle help overlay |
| `q` | Global | Quit |
| `Ctrl+C` | Global | Quit |

### 3.6 Messages

```go
// internal/tui/messages.go

// Data loading
type DatabasesLoadedMsg struct { Databases []*database.DatabaseInfo }
type TablesLoadedMsg struct { Tables []string }
type DataLoadedMsg struct { Result *database.QueryResult }
type SchemaLoadedMsg struct { Info *database.TableInfo }

// Errors
type ErrorMsg struct { Err error }

// Actions
type QueryExecutedMsg struct { Result *database.QueryResult; Duration time.Duration }
type CellUpdatedMsg struct { Row, Col int }
type RowDeletedMsg struct { RowID any }
type RowInsertedMsg struct { ID int64 }

// Navigation
type DatabaseSelectedMsg struct { DB *database.DatabaseInfo }
type TableSelectedMsg struct { Table string }
```

### 3.7 State Machine

```
┌──────────────┐   select db   ┌──────────────┐   select table   ┌──────────────┐
│ Database     │ ────────────> │ Table        │ ───────────────> │ Data         │
│ List         │               │ List         │                  │ View         │
└──────────────┘               └──────────────┘                  └──────────────┘
      │                              │                                  │
      │                              │ 's' key                          │
      │                              v                                  │
      │                        ┌──────────────┐                         │
      │                        │ Schema Modal │                         │
      │                        └──────────────┘                         │
      │                                                                 │
      │  '/' or Tab to query                                            │
      v                                                                 v
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Query Editor                                   │
│  Execute → DataLoadedMsg → Update DataView                                  │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 3.8 Styling (lipgloss)

```go
// internal/tui/styles.go

var (
    // Colors
    primaryColor   = lipgloss.Color("#7C3AED")  // Purple
    secondaryColor = lipgloss.Color("#10B981")  // Green
    mutedColor     = lipgloss.Color("#6B7280")  // Gray
    errorColor     = lipgloss.Color("#EF4444")  // Red
    
    // Pane styles
    paneStyle = lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(mutedColor)
    
    focusedPaneStyle = lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(primaryColor)
    
    // Status bar
    statusBarStyle = lipgloss.NewStyle().
        Background(lipgloss.Color("#1F2937")).
        Foreground(lipgloss.Color("#F3F4F6")).
        Padding(0, 1)
    
    // Access level badges
    readOnlyBadge = lipgloss.NewStyle().
        Background(lipgloss.Color("#FCD34D")).
        Foreground(lipgloss.Color("#000")).
        Padding(0, 1).
        Render("RO")
    
    readWriteBadge = lipgloss.NewStyle().
        Background(secondaryColor).
        Foreground(lipgloss.Color("#FFF")).
        Padding(0, 1).
        Render("RW")
)
```

---

## Phase 4: Connect TUI to SSH Server

### 4.1 Create TUI Handler

```go
// internal/tui/handler.go

func Handler(
    dbManager *database.Manager,
    historyStore *history.Store,
) bubbletea.Handler {
    return func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
        user := server.GetUserFromContext(s.Context())
        pty, _, ok := s.Pty()
        if !ok {
            // Shouldn't happen - routing middleware checks this
            return nil, nil
        }
        
        app := NewApp(dbManager, historyStore, user, pty.Window.Width, pty.Window.Height)
        
        return app, []tea.ProgramOption{
            tea.WithAltScreen(),
            tea.WithMouseCellMotion(),
        }
    }
}
```

### 4.2 Local Mode Entry Point

Local mode takes a path argument (file, directory, or glob) and runs with admin access:

```go
// cmd/sqlite-tui/main.go

func main() {
    // Parse flags
    sshMode := flag.Bool("ssh", false, "run SSH server mode")
    configPath := flag.String("config", "", "path to config file (required for SSH mode)")
    showVersion := flag.Bool("version", false, "show version")
    flag.Parse()
    
    if *showVersion {
        fmt.Printf("sqlite-tui %s\n", version)
        os.Exit(0)
    }
    
    // SSH server mode
    if *sshMode {
        if *configPath == "" {
            log.Fatal("SSH mode requires -config flag")
        }
        runSSHServer(*configPath)
        return
    }
    
    // Local mode - require path argument
    args := flag.Args()
    if len(args) == 0 {
        fmt.Println("Usage: sqlite-tui <path> [command] [args...]")
        fmt.Println("       sqlite-tui -ssh -config config.yaml")
        fmt.Println("")
        fmt.Println("Examples:")
        fmt.Println("  sqlite-tui mydb.db              # TUI mode")
        fmt.Println("  sqlite-tui mydb.db ls           # CLI: list databases")
        fmt.Println("  sqlite-tui mydb.db tables mydb  # CLI: list tables")
        os.Exit(1)
    }
    
    pathArg := args[0]
    cmdArgs := args[1:]  // Remaining args are command + args
    
    if len(cmdArgs) > 0 {
        runLocalCLI(pathArg, cmdArgs)
    } else {
        runLocalTUI(pathArg)
    }
}

func runLocal(pathArg string) (*database.Manager, *access.UserInfo, error) {
    // Create minimal config from path argument
    cfg := config.DefaultConfig()
    cfg.Databases = []config.DatabaseSource{{
        Path: pathArg,
        Description: "Local database",
    }}
    
    // Initialize database manager
    dbManager, err := database.NewManager(cfg)
    if err != nil {
        return nil, nil, err
    }
    dbManager.Start()
    
    // Create local admin user
    user := &access.UserInfo{
        Name:    "local",
        IsAdmin: true,  // Always admin in local mode
    }
    
    return dbManager, user, nil
}

func runLocalCLI(pathArg string, cmdArgs []string) {
    dbManager, user, err := runLocal(pathArg)
    if err != nil {
        log.Fatalf("Failed to initialize: %v", err)
    }
    defer dbManager.Stop()
    
    // Create CLI handler and execute command
    handler := cli.NewHandler(dbManager, nil, version)
    
    // Create a local command context (wraps os.Stdout/Stderr)
    ctx := cli.NewLocalContext(user, cmdArgs, os.Stdout, os.Stderr)
    handler.HandleLocal(ctx)
}

func runLocalTUI(pathArg string) error {
    dbManager, user, err := runLocal(pathArg)
    if err != nil {
        log.Fatalf("Failed to initialize: %v", err)
    }
    defer dbManager.Stop()
    
    // Get terminal size
    width, height := 80, 24
    if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
        width, height = w, h
    }
    
    // Run TUI
    app := tui.NewApp(dbManager, nil, user, width, height)
    p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
    _, err = p.Run()
    return err
}
```

### 4.3 SSH Server Entry Point

```go
func runSSHServer(configPath string) error {
    cfg, err := config.Load(configPath)
    if err != nil {
        log.Fatalf("Failed to load config: %v", err)
    }
    
    historyStore, _ := history.NewStore(cfg.GetDataDir())
    defer historyStore.Close()
    
    dbManager, _ := database.NewManager(cfg)
    dbManager.Start()
    defer dbManager.Stop()
    
    // Create handlers
    cliHandler := cli.NewHandler(dbManager, historyStore, version)
    
    // Create and configure SSH server
    sshServer := server.NewServer(cfg, dbManager, historyStore)
    sshServer.SetCLIHandler(cliHandler.Handle)
    sshServer.SetTUIHandler(tui.Handler(dbManager, historyStore))
    
    log.Printf("Starting SSH server on %s", cfg.Server.SSH.Listen)
    return sshServer.ListenAndServe()
}
```

---

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Entry Points                                   │
├─────────────────────────────────┬───────────────────────────────────────────┤
│         SSH Server              │           Local Mode                      │
│   (wish + middleware)           │     (direct terminal)                     │
└─────────────┬───────────────────┴───────────────────┬───────────────────────┘
              │                                       │
              ▼                                       │
┌─────────────────────────────┐                       │
│       Authenticator         │                       │
│  - Public key auth          │                       │
│  - Keyboard-interactive     │                       │
│  - Anonymous users          │                       │
└─────────────┬───────────────┘                       │
              │                                       │
              ▼                                       │
┌─────────────────────────────────────────────────────┤
│              Middleware Chain                       │
│  LoggingMiddleware                                  │
│    → HistoryMiddleware                              │
│      → DatabaseMiddleware                           │
│        → SessionMiddleware                          │
│          → RoutingMiddleware                        │
└─────────────┬───────────────────────────────────────┘
              │
              ▼
┌─────────────────────────────┐
│     Routing Middleware      │
│  Command args?  → CLI       │
│  PTY present?   → TUI       │
│  Neither?       → Error     │
└─────────┬───────────┬───────┘
          │           │
          ▼           ▼
┌─────────────┐ ┌─────────────┐
│ CLI Handler │ │ TUI Handler │◄──── Local mode joins here
│  (20 cmds)  │ │ (bubbletea) │
└──────┬──────┘ └──────┬──────┘
       │               │
       └───────┬───────┘
               ▼
┌─────────────────────────────┐
│      Database Manager       │
├─────────────────────────────┤
│ • Discovery (glob/fsnotify) │
│ • Connection Pool           │
│ • Lock Manager              │
│ • Access Resolver           │
│ • Query Execution           │
└─────────────┬───────────────┘
              │
              ▼
┌─────────────────────────────┐
│       History Store         │
│ • Session tracking          │
│ • Query history             │
│ • Audit log                 │
└─────────────────────────────┘
```

---

## Testing Strategy

### Unit Tests

| Package | Test Focus |
|---------|------------|
| `access` | Resolver logic, level parsing |
| `config` | YAML parsing, reload, validation |
| `database` | Query building, schema introspection |
| `cli` | Command parsing, output formatting |
| `tui/views` | Model updates, key handling |

### Integration Tests

1. **SSH Authentication**: Test public key + anonymous flows
2. **CLI Commands**: End-to-end command execution over SSH
3. **Access Control**: Verify read-only users can't write
4. **Config Reload**: Hot-reload behavior

### Manual Testing

```bash
# Local TUI mode (interactive)
./sqlite-tui test.db
./sqlite-tui ./databases/

# Local CLI mode (run and exit)
./sqlite-tui test.db ls
./sqlite-tui test.db tables test
./sqlite-tui test.db query test "SELECT * FROM users"
./sqlite-tui test.db insert test users --json='{"name":"Alice"}'

# SSH server mode
./sqlite-tui -ssh -config config.yaml

# SSH CLI commands (same as local)
ssh localhost -p 2222 ls
ssh localhost -p 2222 query mydb "SELECT * FROM users"

# SSH TUI (interactive)
ssh -t localhost -p 2222  # -t forces PTY
```

---

## Implementation Todos

### Phase 1
- [ ] Update `go.mod` module to `github.com/johan-st/sqlite-tui`
- [ ] Update imports in all 13 files
- [ ] Refactor `main.go` for new CLI: `sqlite-tui <path>` (local) vs `sqlite-tui -ssh -config file` (server)
- [ ] Implement `runLocalTUI(path)` - path arg, auto-admin, direct terminal
- [ ] Implement `runSSHServer(config)` - use `server.NewServer` with CLI handler
- [ ] Test local mode with file/directory/glob
- [ ] Test SSH connection and CLI commands

### Phase 2 - CLI Commands
- [ ] Add `LocalContext` and `HandleLocal()` to `internal/cli/cli.go` for local mode
- [ ] Refactor `Handle()` to share routing logic with `HandleLocal()` via `routeCommand()`
- [ ] Create `internal/cli/query.go` with `cmdQuery`, `cmdSelect`, `cmdCount`
- [ ] Create `internal/cli/data.go` with `cmdInsert`, `cmdUpdate`, `cmdDelete`
- [ ] Create `internal/cli/export.go` with `cmdExport`, `cmdDownload`
- [ ] Create `internal/cli/schema_cmd.go` with `cmdCreateTable`, `cmdAddColumn`, `cmdDropTable`
- [ ] Create `internal/cli/admin.go` with `cmdSessions`, `cmdHistory`, `cmdAudit`, `cmdReloadConfig`
- [ ] Add history logging to all data modification commands
- [ ] Add audit logging to all write operations

### Phase 3 - TUI
- [ ] Create `internal/tui/app.go` - root model
- [ ] Create `internal/tui/handler.go` - SSH handler factory
- [ ] Create `internal/tui/keymap.go` - key bindings
- [ ] Create `internal/tui/styles.go` - lipgloss theme
- [ ] Create `internal/tui/messages.go` - custom messages
- [ ] Create `internal/tui/views/database_list.go`
- [ ] Create `internal/tui/views/table_list.go`
- [ ] Create `internal/tui/views/data_view.go`
- [ ] Create `internal/tui/views/query_editor.go`
- [ ] Create `internal/tui/views/schema_view.go`
- [ ] Create `internal/tui/components/table.go`
- [ ] Create `internal/tui/components/status_bar.go`
- [ ] Create `internal/tui/components/help.go`

### Phase 4 - Integration
- [ ] Connect TUI handler to SSH server
- [ ] Implement `RunLocalTUI()` with direct terminal
- [ ] Test SSH TUI mode
- [ ] Test local TUI mode
- [ ] End-to-end testing
