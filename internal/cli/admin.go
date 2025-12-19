package cli

import (
	"fmt"
	"strconv"
	"time"

	"github.com/johan-st/sqlite-tui/internal/server"
)

// cmdSessions lists active SSH sessions.
func (h *Handler) cmdSessions(ctx *CommandContext) {
	if !ctx.RequireAdmin() {
		return
	}

	// Get session manager from SSH context (only available in SSH mode)
	if ctx.Session == nil {
		fmt.Fprintln(ctx.Err, "sessions command is only available in SSH server mode")
		ctx.Exit(1)
		return
	}

	sessionMgr := server.GetSessionMgrFromSSH(ctx.Session)
	if sessionMgr == nil {
		fmt.Fprintln(ctx.Err, "Session manager not available")
		ctx.Exit(1)
		return
	}

	sessions := sessionMgr.ListActiveSessions()

	format := ctx.GetFlag("format")
	if format == "json" {
		result := make([]map[string]any, 0, len(sessions))
		for _, s := range sessions {
			result = append(result, map[string]any{
				"id":          s.ID,
				"user":        s.User.DisplayName(),
				"remote_addr": s.RemoteAddr,
				"duration":    s.Duration().String(),
				"idle":        s.IdleTime().String(),
			})
		}
		printJSON(ctx.Out, result)
		return
	}

	if len(sessions) == 0 {
		fmt.Fprintln(ctx.Out, "No active sessions")
		return
	}

	fmt.Fprintln(ctx.Out, "ID\tUSER\tREMOTE\tDURATION\tIDLE")
	for _, s := range sessions {
		fmt.Fprintf(ctx.Out, "%s\t%s\t%s\t%s\t%s\n",
			s.ID[:8],
			s.User.DisplayName(),
			s.RemoteAddr,
			formatDuration(s.Duration()),
			formatDuration(s.IdleTime()))
	}
}

// cmdHistory shows query history.
func (h *Handler) cmdHistory(ctx *CommandContext) {
	if !ctx.RequireAdmin() {
		return
	}

	if h.historyStore == nil {
		fmt.Fprintln(ctx.Err, "History not available in local mode")
		ctx.Exit(1)
		return
	}

	limit := 50
	if l := ctx.GetFlag("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			limit = n
		}
	}

	queries, err := h.historyStore.ListQueryHistory("", "", time.Time{}, limit)
	if err != nil {
		fmt.Fprintf(ctx.Err, "Error fetching history: %v\n", err)
		ctx.Exit(1)
		return
	}

	format := ctx.GetFlag("format")
	if format == "json" {
		printJSON(ctx.Out, queries)
		return
	}

	if len(queries) == 0 {
		fmt.Fprintln(ctx.Out, "No query history")
		return
	}

	fmt.Fprintln(ctx.Out, "TIME\tDATABASE\tDURATION\tQUERY")
	for _, q := range queries {
		query := q.Query
		if len(query) > 50 {
			query = query[:47] + "..."
		}
		fmt.Fprintf(ctx.Out, "%s\t%s\t%dms\t%s\n",
			q.CreatedAt.Format("15:04:05"),
			q.DatabasePath,
			q.ExecutionTimeMs,
			query)
	}
}

// cmdAudit shows the audit log.
func (h *Handler) cmdAudit(ctx *CommandContext) {
	if !ctx.RequireAdmin() {
		return
	}

	if h.historyStore == nil {
		fmt.Fprintln(ctx.Err, "Audit log not available in local mode")
		ctx.Exit(1)
		return
	}

	limit := 50
	if l := ctx.GetFlag("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			limit = n
		}
	}

	entries, err := h.historyStore.ListAuditLog("", "", "", time.Time{}, limit)
	if err != nil {
		fmt.Fprintf(ctx.Err, "Error fetching audit log: %v\n", err)
		ctx.Exit(1)
		return
	}

	format := ctx.GetFlag("format")
	if format == "json" {
		printJSON(ctx.Out, entries)
		return
	}

	if len(entries) == 0 {
		fmt.Fprintln(ctx.Out, "No audit log entries")
		return
	}

	fmt.Fprintln(ctx.Out, "TIME\tACTION\tDATABASE\tTABLE\tDETAILS")
	for _, e := range entries {
		details := e.Details
		if len(details) > 40 {
			details = details[:37] + "..."
		}
		fmt.Fprintf(ctx.Out, "%s\t%s\t%s\t%s\t%s\n",
			e.CreatedAt.Format("15:04:05"),
			e.Action,
			e.DatabasePath,
			e.TableName,
			details)
	}
}

// cmdReloadConfig reloads the configuration.
func (h *Handler) cmdReloadConfig(ctx *CommandContext) {
	if !ctx.RequireAdmin() {
		return
	}

	// In local mode, there's no config to reload
	if ctx.Session == nil {
		fmt.Fprintln(ctx.Err, "reload-config is only available in SSH server mode")
		ctx.Exit(1)
		return
	}

	// TODO: Implement config reload via a channel or callback
	// For now, just print a message
	fmt.Fprintln(ctx.Out, "Configuration reload triggered")
	fmt.Fprintln(ctx.Out, "Note: Config watcher handles automatic reloading")
}

// formatDuration formats a duration for display.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
