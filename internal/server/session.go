package server

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/johan-st/sqlite-tui/internal/access"
	"github.com/johan-st/sqlite-tui/internal/history"
)

// Session represents an active SSH session.
type Session struct {
	ID           string
	User         *access.UserInfo
	RemoteAddr   string
	StartTime    time.Time
	LastActivity time.Time
	mu           sync.RWMutex
}

// NewSession creates a new session.
func NewSession(user *access.UserInfo, remoteAddr string) *Session {
	now := time.Now()
	return &Session{
		ID:           uuid.New().String(),
		User:         user,
		RemoteAddr:   remoteAddr,
		StartTime:    now,
		LastActivity: now,
	}
}

// Touch updates the last activity time.
func (s *Session) Touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastActivity = time.Now()
}

// Duration returns how long the session has been active.
func (s *Session) Duration() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Since(s.StartTime)
}

// IdleTime returns how long since the last activity.
func (s *Session) IdleTime() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Since(s.LastActivity)
}

// ToHistorySession converts to a history.Session for storage.
func (s *Session) ToHistorySession() *history.Session {
	return history.NewSession(s.ID, s.User, s.RemoteAddr)
}

// SessionManager manages active sessions.
type SessionManager struct {
	sessions     map[string]*Session
	historyStore *history.Store
	mu           sync.RWMutex
}

// NewSessionManager creates a new session manager.
func NewSessionManager(historyStore *history.Store) *SessionManager {
	return &SessionManager{
		sessions:     make(map[string]*Session),
		historyStore: historyStore,
	}
}

// CreateSession creates and registers a new session.
func (sm *SessionManager) CreateSession(user *access.UserInfo, remoteAddr string) (*Session, error) {
	session := NewSession(user, remoteAddr)

	sm.mu.Lock()
	sm.sessions[session.ID] = session
	sm.mu.Unlock()

	// Store in history
	if sm.historyStore != nil {
		if err := sm.historyStore.CreateSession(session.ToHistorySession()); err != nil {
			// Log but don't fail - history is not critical
			return session, nil
		}
	}

	return session, nil
}

// GetSession returns a session by ID.
func (sm *SessionManager) GetSession(id string) *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessions[id]
}

// EndSession ends a session.
func (sm *SessionManager) EndSession(id string) {
	sm.mu.Lock()
	delete(sm.sessions, id)
	sm.mu.Unlock()

	if sm.historyStore != nil {
		sm.historyStore.EndSession(id)
	}
}

// ListActiveSessions returns all active sessions.
func (sm *SessionManager) ListActiveSessions() []*Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	sessions := make([]*Session, 0, len(sm.sessions))
	for _, s := range sm.sessions {
		sessions = append(sessions, s)
	}
	return sessions
}

// Count returns the number of active sessions.
func (sm *SessionManager) Count() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.sessions)
}

// UpdateActivity updates the activity time for a session.
func (sm *SessionManager) UpdateActivity(id string) {
	sm.mu.RLock()
	session := sm.sessions[id]
	sm.mu.RUnlock()

	if session != nil {
		session.Touch()
		if sm.historyStore != nil {
			sm.historyStore.UpdateSessionActivity(id)
		}
	}
}
