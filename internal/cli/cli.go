// Package cli implements the command-line interface for both SSH and local modes.
package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/ssh"
	"github.com/johan-st/sqlite-tui/internal/access"
	"github.com/johan-st/sqlite-tui/internal/database"
	"github.com/johan-st/sqlite-tui/internal/history"
	"github.com/johan-st/sqlite-tui/internal/server"
)

// Handler handles CLI commands over SSH or locally.
type Handler struct {
	dbManager    *database.Manager
	historyStore *history.Store
	version      string
}

// NewHandler creates a new CLI handler.
func NewHandler(dbManager *database.Manager, historyStore *history.Store, version string) *Handler {
	return &Handler{
		dbManager:    dbManager,
		historyStore: historyStore,
		version:      version,
	}
}

// LocalContext wraps command execution for local (non-SSH) mode.
type LocalContext struct {
	User *access.UserInfo
	Args []string
	Out  io.Writer
	Err  io.Writer
}

// NewLocalContext creates a context for local CLI execution.
func NewLocalContext(user *access.UserInfo, args []string, out, errOut io.Writer) *LocalContext {
	return &LocalContext{
		User: user,
		Args: args,
		Out:  out,
		Err:  errOut,
	}
}

// HandleLocal processes a CLI command in local mode (no SSH session).
func (h *Handler) HandleLocal(lctx *LocalContext) error {
	if len(lctx.Args) == 0 {
		fmt.Fprintln(lctx.Out, "No command specified. Run 'help' for usage.")
		return nil
	}

	// Create CommandContext compatible with existing handlers
	ctx := &CommandContext{
		Session:      nil, // No SSH session in local mode
		User:         lctx.User,
		SessionInfo:  nil,
		DBManager:    h.dbManager,
		HistoryStore: h.historyStore,
		Args:         lctx.Args[1:],
		Out:          lctx.Out,
		Err:          lctx.Err,
		exitCode:     0,
	}

	h.routeCommand(lctx.Args[0], ctx)

	if ctx.exitCode != 0 {
		return fmt.Errorf("command failed with exit code %d", ctx.exitCode)
	}
	return nil
}

// Handle processes an SSH session with a CLI command.
func (h *Handler) Handle(s ssh.Session) {
	cmd := s.Command()
	if len(cmd) == 0 {
		fmt.Fprintln(s, "No command specified. Run 'help' for usage.")
		return
	}

	// Get user and session info
	user := server.GetUserFromContext(s.Context())
	session := server.GetSessionFromSSH(s)

	ctx := &CommandContext{
		Session:      s,
		User:         user,
		SessionInfo:  session,
		DBManager:    h.dbManager,
		HistoryStore: h.historyStore,
		Args:         cmd[1:],
		Out:          s,
		Err:          s.Stderr(),
		exitCode:     0,
	}

	h.routeCommand(cmd[0], ctx)

	if ctx.exitCode != 0 {
		s.Exit(ctx.exitCode)
	}
}

// routeCommand routes a command to its handler.
func (h *Handler) routeCommand(cmd string, ctx *CommandContext) {
	switch cmd {
	// Database commands
	case "ls", "list":
		h.cmdList(ctx)
	case "info":
		h.cmdInfo(ctx)
	case "tables":
		h.cmdTables(ctx)
	case "schema":
		h.cmdSchema(ctx)

	// Query commands
	case "query":
		h.cmdQuery(ctx)
	case "select":
		h.cmdSelect(ctx)
	case "count":
		h.cmdCount(ctx)

	// Data commands
	case "insert":
		h.cmdInsert(ctx)
	case "update":
		h.cmdUpdate(ctx)
	case "delete":
		h.cmdDelete(ctx)

	// Export commands
	case "export":
		h.cmdExport(ctx)
	case "download":
		h.cmdDownload(ctx)

	// Schema commands
	case "create-table":
		h.cmdCreateTable(ctx)
	case "add-column":
		h.cmdAddColumn(ctx)
	case "drop-table":
		h.cmdDropTable(ctx)

	// Admin commands
	case "sessions":
		h.cmdSessions(ctx)
	case "history":
		h.cmdHistory(ctx)
	case "audit":
		h.cmdAudit(ctx)
	case "reload-config":
		h.cmdReloadConfig(ctx)

	// Utility commands
	case "whoami":
		h.cmdWhoami(ctx)
	case "help":
		h.cmdHelp(ctx)
	case "version":
		h.cmdVersion(ctx)

	default:
		fmt.Fprintf(ctx.Err, "Unknown command: %s\n", cmd)
		fmt.Fprintln(ctx.Err, "Run 'help' for usage.")
		ctx.Exit(1)
	}
}

// CommandContext provides context for command execution.
type CommandContext struct {
	Session      ssh.Session // nil in local mode
	User         *access.UserInfo
	SessionInfo  *server.Session
	DBManager    *database.Manager
	HistoryStore *history.Store
	Args         []string
	Out          io.Writer
	Err          io.Writer
	exitCode     int
}

// Exit sets the exit code (used instead of calling Session.Exit directly).
func (c *CommandContext) Exit(code int) {
	c.exitCode = code
}

// GetSessionID returns the session ID or empty string.
func (c *CommandContext) GetSessionID() string {
	if c.SessionInfo != nil {
		return c.SessionInfo.ID
	}
	return ""
}

// RequireArg ensures an argument is provided.
func (c *CommandContext) RequireArg(index int, name string) (string, bool) {
	if index >= len(c.Args) {
		fmt.Fprintf(c.Err, "Missing required argument: %s\n", name)
		c.Exit(1)
		return "", false
	}
	return c.Args[index], true
}

// GetFlag returns a flag value from args (e.g., --format=json).
func (c *CommandContext) GetFlag(name string) string {
	prefix := "--" + name + "="
	shortPrefix := "-" + name + "="
	for _, arg := range c.Args {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
		if strings.HasPrefix(arg, shortPrefix) {
			return strings.TrimPrefix(arg, shortPrefix)
		}
	}
	return ""
}

// HasFlag checks if a boolean flag is present.
func (c *CommandContext) HasFlag(name string) bool {
	flag := "--" + name
	shortFlag := "-" + name
	for _, arg := range c.Args {
		if arg == flag || arg == shortFlag {
			return true
		}
	}
	return false
}

// GetPositionalArgs returns args that are not flags.
func (c *CommandContext) GetPositionalArgs() []string {
	var result []string
	for _, arg := range c.Args {
		if !strings.HasPrefix(arg, "-") {
			result = append(result, arg)
		}
	}
	return result
}

// RequireRead checks if user has read access to a database.
func (c *CommandContext) RequireRead(dbPath string) bool {
	level := c.DBManager.GetAccessLevel(c.User, dbPath)
	if !level.CanRead() {
		fmt.Fprintf(c.Err, "Access denied: no read access to %s\n", dbPath)
		c.Exit(1)
		return false
	}
	return true
}

// RequireWrite checks if user has write access to a database.
func (c *CommandContext) RequireWrite(dbPath string) bool {
	level := c.DBManager.GetAccessLevel(c.User, dbPath)
	if !level.CanWrite() {
		fmt.Fprintf(c.Err, "Access denied: no write access to %s\n", dbPath)
		c.Exit(1)
		return false
	}
	return true
}

// RequireAdmin checks if user has admin access.
func (c *CommandContext) RequireAdmin() bool {
	if c.User == nil || !c.User.IsAdmin {
		fmt.Fprintln(c.Err, "Access denied: admin access required")
		c.Exit(1)
		return false
	}
	return true
}
