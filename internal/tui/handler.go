package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish/bubbletea"
	"github.com/johan-st/sqlite-tui/internal/database"
	"github.com/johan-st/sqlite-tui/internal/history"
	"github.com/johan-st/sqlite-tui/internal/server"
)

// Handler returns a bubbletea middleware handler for SSH sessions.
func Handler(dbManager *database.Manager, historyStore *history.Store) bubbletea.Handler {
	return func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
		user := server.GetUserFromContext(s.Context())
		pty, _, ok := s.Pty()
		if !ok {
			// This shouldn't happen as routing middleware checks for PTY
			return nil, nil
		}

		app := NewApp(dbManager, historyStore, user, pty.Window.Width, pty.Window.Height)

		return app, []tea.ProgramOption{
			tea.WithAltScreen(),
			tea.WithMouseCellMotion(),
		}
	}
}
