package server

import (
	"log"

	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/johan-st/sqlite-tui/internal/access"
	"github.com/johan-st/sqlite-tui/internal/database"
	"github.com/johan-st/sqlite-tui/internal/history"
)

// Context keys for middleware values
type ctxKey string

const (
	ctxKeySession    ctxKey = "session"
	ctxKeyUser       ctxKey = "user"
	ctxKeyDBManager  ctxKey = "db_manager"
	ctxKeyHistory    ctxKey = "history"
	ctxKeySessionMgr ctxKey = "session_mgr"
)

// SessionMiddleware creates sessions for each connection.
func SessionMiddleware(sessionMgr *SessionManager) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			user := GetUserFromContext(s.Context())
			if user == nil {
				// Create anonymous user if not authenticated
				user = &access.UserInfo{
					IsAnonymous:   true,
					AnonymousName: "unknown",
					RemoteAddr:    s.RemoteAddr().String(),
				}
			}

			session, err := sessionMgr.CreateSession(user, s.RemoteAddr().String())
			if err != nil {
				log.Printf("Failed to create session: %v", err)
			}

			// Store session in context
			s.Context().SetValue(ctxKeySession, session)
			s.Context().SetValue(ctxKeySessionMgr, sessionMgr)

			// Ensure session is cleaned up
			defer func() {
				if session != nil {
					sessionMgr.EndSession(session.ID)
				}
			}()

			next(s)
		}
	}
}

// DatabaseMiddleware injects the database manager into the context.
func DatabaseMiddleware(dbManager *database.Manager) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			s.Context().SetValue(ctxKeyDBManager, dbManager)
			next(s)
		}
	}
}

// HistoryMiddleware injects the history store into the context.
func HistoryMiddleware(historyStore *history.Store) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			s.Context().SetValue(ctxKeyHistory, historyStore)
			next(s)
		}
	}
}

// LoggingMiddleware logs connections.
func LoggingMiddleware() wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			user := GetUserFromContext(s.Context())
			userName := "anonymous"
			if user != nil {
				userName = user.DisplayName()
			}

			log.Printf("Connection from %s as %s (command: %v)",
				s.RemoteAddr(), userName, s.Command())

			next(s)

			log.Printf("Disconnected: %s", s.RemoteAddr())
		}
	}
}

// ActiveTermMiddleware ensures the client has a PTY for TUI mode.
func ActiveTermMiddleware() wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			// Allow non-PTY connections for CLI commands
			if len(s.Command()) > 0 {
				next(s)
				return
			}

			// For TUI mode, require PTY
			_, _, hasPty := s.Pty()
			if !hasPty {
				wish.Fatalln(s, "PTY required for interactive mode. Use -t flag or run a command directly.")
				return
			}

			next(s)
		}
	}
}

// GetSessionFromSSH retrieves the session from the SSH session context.
func GetSessionFromSSH(s ssh.Session) *Session {
	if session, ok := s.Context().Value(ctxKeySession).(*Session); ok {
		return session
	}
	return nil
}

// GetDBManagerFromSSH retrieves the database manager from the SSH session context.
func GetDBManagerFromSSH(s ssh.Session) *database.Manager {
	if mgr, ok := s.Context().Value(ctxKeyDBManager).(*database.Manager); ok {
		return mgr
	}
	return nil
}

// GetHistoryFromSSH retrieves the history store from the SSH session context.
func GetHistoryFromSSH(s ssh.Session) *history.Store {
	if store, ok := s.Context().Value(ctxKeyHistory).(*history.Store); ok {
		return store
	}
	return nil
}

// GetSessionMgrFromSSH retrieves the session manager from the SSH session context.
func GetSessionMgrFromSSH(s ssh.Session) *SessionManager {
	if mgr, ok := s.Context().Value(ctxKeySessionMgr).(*SessionManager); ok {
		return mgr
	}
	return nil
}
