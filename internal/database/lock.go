package database

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// LockError represents a database locking error.
type LockError struct {
	Database string
	HeldBy   string
	Since    time.Time
}

func (e *LockError) Error() string {
	return fmt.Sprintf("database %q is locked by %s (since %s)",
		e.Database, e.HeldBy, e.Since.Format(time.Kitchen))
}

// LockInfo contains information about who holds a lock.
type LockInfo struct {
	HeldBy    string
	SessionID string
	Since     time.Time
}

// LockManager manages application-level database locks.
// This provides clearer error messages than SQLite's built-in locking.
type LockManager struct {
	locks map[string]*LockInfo
	mu    sync.RWMutex
}

// NewLockManager creates a new lock manager.
func NewLockManager() *LockManager {
	return &LockManager{
		locks: make(map[string]*LockInfo),
	}
}

// TryLock attempts to acquire a write lock on a database.
// Returns nil if successful, or a LockError if already locked.
func (lm *LockManager) TryLock(dbPath, holder, sessionID string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if info, exists := lm.locks[dbPath]; exists {
		// If same session holds the lock, allow re-entry
		if info.SessionID == sessionID {
			return nil
		}
		return &LockError{
			Database: dbPath,
			HeldBy:   info.HeldBy,
			Since:    info.Since,
		}
	}

	lm.locks[dbPath] = &LockInfo{
		HeldBy:    holder,
		SessionID: sessionID,
		Since:     time.Now(),
	}
	return nil
}

// Unlock releases a lock on a database.
func (lm *LockManager) Unlock(dbPath, sessionID string) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if info, exists := lm.locks[dbPath]; exists {
		if info.SessionID == sessionID {
			delete(lm.locks, dbPath)
		}
	}
}

// IsLocked checks if a database is locked.
func (lm *LockManager) IsLocked(dbPath string) bool {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	_, exists := lm.locks[dbPath]
	return exists
}

// GetLockInfo returns information about who holds a lock.
func (lm *LockManager) GetLockInfo(dbPath string) *LockInfo {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	if info, exists := lm.locks[dbPath]; exists {
		return &LockInfo{
			HeldBy:    info.HeldBy,
			SessionID: info.SessionID,
			Since:     info.Since,
		}
	}
	return nil
}

// ReleaseAllForSession releases all locks held by a session.
func (lm *LockManager) ReleaseAllForSession(sessionID string) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	for dbPath, info := range lm.locks {
		if info.SessionID == sessionID {
			delete(lm.locks, dbPath)
		}
	}
}

// ListLocks returns all current locks.
func (lm *LockManager) ListLocks() map[string]*LockInfo {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	result := make(map[string]*LockInfo, len(lm.locks))
	for k, v := range lm.locks {
		result[k] = &LockInfo{
			HeldBy:    v.HeldBy,
			SessionID: v.SessionID,
			Since:     v.Since,
		}
	}
	return result
}

// WithWriteLock executes a function while holding the write lock.
func (lm *LockManager) WithWriteLock(dbPath, holder, sessionID string, fn func() error) error {
	if err := lm.TryLock(dbPath, holder, sessionID); err != nil {
		return err
	}
	defer lm.Unlock(dbPath, sessionID)

	return fn()
}

// LogWALError logs when we hit the WAL lock (SQLite's internal locking).
// This should be rare if application-level locking is working correctly.
func LogWALError(dbPath string, err error) {
	log.Printf("ERROR: WAL lock encountered on %s: %v (this indicates application lock failure)", dbPath, err)
}

// IsWALLockError checks if an error is a SQLite WAL lock error.
func IsWALLockError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return contains(errStr, "database is locked") ||
		contains(errStr, "SQLITE_BUSY") ||
		contains(errStr, "SQLITE_LOCKED")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
