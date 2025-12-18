package server

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/bubbletea"
	"github.com/johan-st/sqlite-tui/internal/config"
	"github.com/johan-st/sqlite-tui/internal/database"
	"github.com/johan-st/sqlite-tui/internal/history"
)

// Server is the SSH server for sqlite-tui.
type Server struct {
	config        *config.Config
	dbManager     *database.Manager
	historyStore  *history.Store
	sessionMgr    *SessionManager
	authenticator *Authenticator
	sshServer     *ssh.Server
	tuiHandler    bubbletea.Handler
	cliHandler    func(ssh.Session)
}

// NewServer creates a new SSH server.
func NewServer(cfg *config.Config, dbManager *database.Manager, historyStore *history.Store) *Server {
	sessionMgr := NewSessionManager(historyStore)
	authenticator := NewAuthenticator(cfg, historyStore)

	return &Server{
		config:        cfg,
		dbManager:     dbManager,
		historyStore:  historyStore,
		sessionMgr:    sessionMgr,
		authenticator: authenticator,
	}
}

// SetTUIHandler sets the Bubble Tea handler for interactive sessions.
func (s *Server) SetTUIHandler(handler bubbletea.Handler) {
	s.tuiHandler = handler
}

// SetCLIHandler sets the handler for CLI commands.
func (s *Server) SetCLIHandler(handler func(ssh.Session)) {
	s.cliHandler = handler
}

// Start starts the SSH server.
func (s *Server) Start() error {
	// Ensure host key directory exists
	keyDir := filepath.Dir(s.config.Server.SSH.HostKeyPath)
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		return fmt.Errorf("failed to create host key directory: %w", err)
	}

	// Build middleware chain
	middleware := []wish.Middleware{
		// Order matters: last middleware wraps first
		s.routingMiddleware(),             // Route to TUI or CLI
		SessionMiddleware(s.sessionMgr),   // Create session
		DatabaseMiddleware(s.dbManager),   // Inject DB manager
		HistoryMiddleware(s.historyStore), // Inject history store
		LoggingMiddleware(),               // Log connections
	}

	// Create SSH server
	opts := []ssh.Option{
		wish.WithAddress(s.config.Server.SSH.Listen),
		wish.WithHostKeyPath(s.config.Server.SSH.HostKeyPath),
		wish.WithPublicKeyAuth(s.authenticator.PublicKeyHandler()),
		wish.WithMiddleware(middleware...),
	}

	// Add keyboard-interactive auth if keyless is allowed
	if s.config.AllowKeyless {
		opts = append(opts, wish.WithKeyboardInteractiveAuth(s.authenticator.KeyboardInteractiveHandler()))
	}

	// Add timeouts
	if s.config.GetIdleTimeout() > 0 {
		opts = append(opts, wish.WithIdleTimeout(s.config.GetIdleTimeout()))
	}
	if s.config.GetMaxTimeout() > 0 {
		opts = append(opts, wish.WithMaxTimeout(s.config.GetMaxTimeout()))
	}

	server, err := wish.NewServer(opts...)
	if err != nil {
		return fmt.Errorf("failed to create SSH server: %w", err)
	}
	s.sshServer = server

	// Start server
	log.Printf("Starting SSH server on %s", s.config.Server.SSH.Listen)

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != ssh.ErrServerClosed {
			log.Printf("SSH server error: %v", err)
		}
	}()

	<-done
	log.Println("Shutting down SSH server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return server.Shutdown(ctx)
}

// ListenAndServe starts the server without signal handling (for embedding).
func (s *Server) ListenAndServe() error {
	// Ensure host key directory exists
	keyDir := filepath.Dir(s.config.Server.SSH.HostKeyPath)
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		return fmt.Errorf("failed to create host key directory: %w", err)
	}

	// Build middleware chain
	middleware := []wish.Middleware{
		s.routingMiddleware(),
		SessionMiddleware(s.sessionMgr),
		DatabaseMiddleware(s.dbManager),
		HistoryMiddleware(s.historyStore),
		LoggingMiddleware(),
	}

	// Create SSH server
	opts := []ssh.Option{
		wish.WithAddress(s.config.Server.SSH.Listen),
		wish.WithHostKeyPath(s.config.Server.SSH.HostKeyPath),
		wish.WithPublicKeyAuth(s.authenticator.PublicKeyHandler()),
		wish.WithMiddleware(middleware...),
	}

	if s.config.AllowKeyless {
		opts = append(opts, wish.WithKeyboardInteractiveAuth(s.authenticator.KeyboardInteractiveHandler()))
	}

	if s.config.GetIdleTimeout() > 0 {
		opts = append(opts, wish.WithIdleTimeout(s.config.GetIdleTimeout()))
	}
	if s.config.GetMaxTimeout() > 0 {
		opts = append(opts, wish.WithMaxTimeout(s.config.GetMaxTimeout()))
	}

	server, err := wish.NewServer(opts...)
	if err != nil {
		return fmt.Errorf("failed to create SSH server: %w", err)
	}
	s.sshServer = server

	return server.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.sshServer != nil {
		return s.sshServer.Shutdown(ctx)
	}
	return nil
}

// GetAddr returns the server's listen address string.
func (s *Server) GetAddr() string {
	if s.sshServer != nil {
		return s.sshServer.Addr
	}
	return ""
}

// routingMiddleware routes requests to either TUI or CLI handler.
func (s *Server) routingMiddleware() wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(sess ssh.Session) {
			cmd := sess.Command()

			// If command is provided, use CLI handler
			if len(cmd) > 0 {
				if s.cliHandler != nil {
					s.cliHandler(sess)
				} else {
					wish.Fatalln(sess, "CLI commands not yet implemented")
				}
				return
			}

			// No command, use TUI handler
			_, _, hasPty := sess.Pty()
			if !hasPty {
				wish.Fatalln(sess, "PTY required for interactive mode. Use -t flag or provide a command.")
				return
			}

			if s.tuiHandler != nil {
				// Use bubbletea middleware
				btMiddleware := bubbletea.Middleware(s.tuiHandler)
				btMiddleware(next)(sess)
			} else {
				wish.Fatalln(sess, "TUI not yet implemented")
			}
		}
	}
}

// GetSessionManager returns the session manager.
func (s *Server) GetSessionManager() *SessionManager {
	return s.sessionMgr
}
