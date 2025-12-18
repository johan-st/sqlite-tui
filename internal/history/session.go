package history

import (
	"time"

	"github.com/johan-st/sqlite-tui/internal/access"
)

// Session represents a user session.
type Session struct {
	ID                   string
	UserName             string // Authenticated username or empty
	PublicKeyFingerprint string // SSH key fingerprint or empty
	AnonymousName        string // Generated name for anonymous users
	RemoteAddr           string
	CreatedAt            time.Time
	LastActiveAt         time.Time
	IsActive             bool
}

// QueryRecord represents a query in the history.
type QueryRecord struct {
	ID              int64
	SessionID       string
	DatabasePath    string
	Query           string
	ExecutionTimeMs int64
	RowsAffected    int64
	Error           string
	CreatedAt       time.Time
}

// AuditRecord represents an audit log entry.
type AuditRecord struct {
	ID           int64
	SessionID    string
	Action       string // query, update, delete, export, download, etc.
	DatabasePath string
	TableName    string
	Details      string // JSON with specifics
	CreatedAt    time.Time
}

// NewSession creates a new session from user info.
func NewSession(id string, user *access.UserInfo, remoteAddr string) *Session {
	s := &Session{
		ID:           id,
		RemoteAddr:   remoteAddr,
		CreatedAt:    time.Now(),
		LastActiveAt: time.Now(),
		IsActive:     true,
	}

	if user != nil {
		if user.IsAnonymous {
			s.AnonymousName = user.AnonymousName
		} else {
			s.UserName = user.Name
			s.PublicKeyFingerprint = user.PublicKeyFP
		}
	}

	return s
}

// DisplayName returns the display name for the session.
func (s *Session) DisplayName() string {
	if s.UserName != "" {
		return s.UserName
	}
	if s.AnonymousName != "" {
		return s.AnonymousName
	}
	return "unknown"
}

// IsAuthenticated returns true if the session has an authenticated user.
func (s *Session) IsAuthenticated() bool {
	return s.UserName != ""
}

// Touch updates the last active time.
func (s *Session) Touch() {
	s.LastActiveAt = time.Now()
}

// AuditAction constants
const (
	ActionQuery       = "query"
	ActionSelect      = "select"
	ActionInsert      = "insert"
	ActionUpdate      = "update"
	ActionDelete      = "delete"
	ActionExport      = "export"
	ActionDownload    = "download"
	ActionCreateTable = "create_table"
	ActionDropTable   = "drop_table"
	ActionAddColumn   = "add_column"
	ActionDropColumn  = "drop_column"
)
