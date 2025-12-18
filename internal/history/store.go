package history

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store manages the history database.
type Store struct {
	db            *sql.DB
	nameGenerator *NameGenerator
}

// NewStore creates a new history store.
func NewStore(dataDir string) (*Store, error) {
	// Ensure data directory exists
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "history.db")
	dsn := fmt.Sprintf("file:%s?_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=ON", dbPath)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open history database: %w", err)
	}

	store := &Store{
		db:            db,
		nameGenerator: NewNameGenerator(),
	}

	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate history database: %w", err)
	}

	return store, nil
}

// migrate creates the database schema.
func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_name TEXT,
		public_key_fingerprint TEXT,
		anonymous_name TEXT,
		remote_addr TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_active_at DATETIME,
		is_active INTEGER DEFAULT 1
	);

	CREATE INDEX IF NOT EXISTS idx_sessions_user_name ON sessions(user_name);
	CREATE INDEX IF NOT EXISTS idx_sessions_created_at ON sessions(created_at);
	CREATE INDEX IF NOT EXISTS idx_sessions_is_active ON sessions(is_active);

	CREATE TABLE IF NOT EXISTS query_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT REFERENCES sessions(id),
		database_path TEXT,
		query TEXT,
		execution_time_ms INTEGER,
		rows_affected INTEGER,
		error TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_query_history_session_id ON query_history(session_id);
	CREATE INDEX IF NOT EXISTS idx_query_history_database_path ON query_history(database_path);
	CREATE INDEX IF NOT EXISTS idx_query_history_created_at ON query_history(created_at);

	CREATE TABLE IF NOT EXISTS audit_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT REFERENCES sessions(id),
		action TEXT,
		database_path TEXT,
		table_name TEXT,
		details TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_audit_log_session_id ON audit_log(session_id);
	CREATE INDEX IF NOT EXISTS idx_audit_log_action ON audit_log(action);
	CREATE INDEX IF NOT EXISTS idx_audit_log_database_path ON audit_log(database_path);
	CREATE INDEX IF NOT EXISTS idx_audit_log_created_at ON audit_log(created_at);
	`

	_, err := s.db.Exec(schema)
	return err
}

// Close closes the store.
func (s *Store) Close() error {
	return s.db.Close()
}

// GenerateAnonymousName generates a new anonymous name.
func (s *Store) GenerateAnonymousName() string {
	return s.nameGenerator.Generate()
}

// CreateSession creates a new session record.
func (s *Store) CreateSession(session *Session) error {
	_, err := s.db.Exec(`
		INSERT INTO sessions (id, user_name, public_key_fingerprint, anonymous_name, remote_addr, created_at, last_active_at, is_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, session.ID, nullString(session.UserName), nullString(session.PublicKeyFingerprint),
		nullString(session.AnonymousName), session.RemoteAddr, session.CreatedAt, session.LastActiveAt, session.IsActive)

	return err
}

// UpdateSessionActivity updates the last active time for a session.
func (s *Store) UpdateSessionActivity(sessionID string) error {
	_, err := s.db.Exec(`
		UPDATE sessions SET last_active_at = ? WHERE id = ?
	`, time.Now(), sessionID)
	return err
}

// EndSession marks a session as inactive.
func (s *Store) EndSession(sessionID string) error {
	_, err := s.db.Exec(`
		UPDATE sessions SET is_active = 0, last_active_at = ? WHERE id = ?
	`, time.Now(), sessionID)
	return err
}

// GetSession retrieves a session by ID.
func (s *Store) GetSession(sessionID string) (*Session, error) {
	row := s.db.QueryRow(`
		SELECT id, user_name, public_key_fingerprint, anonymous_name, remote_addr, created_at, last_active_at, is_active
		FROM sessions WHERE id = ?
	`, sessionID)

	var session Session
	var userName, pkFP, anonName sql.NullString
	var isActive int

	err := row.Scan(&session.ID, &userName, &pkFP, &anonName, &session.RemoteAddr,
		&session.CreatedAt, &session.LastActiveAt, &isActive)
	if err != nil {
		return nil, err
	}

	session.UserName = userName.String
	session.PublicKeyFingerprint = pkFP.String
	session.AnonymousName = anonName.String
	session.IsActive = isActive == 1

	return &session, nil
}

// ListSessions lists sessions with optional filters.
func (s *Store) ListSessions(activeOnly bool, limit int) ([]*Session, error) {
	query := `
		SELECT id, user_name, public_key_fingerprint, anonymous_name, remote_addr, created_at, last_active_at, is_active
		FROM sessions
	`
	args := make([]any, 0)

	if activeOnly {
		query += " WHERE is_active = 1"
	}

	query += " ORDER BY last_active_at DESC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var session Session
		var userName, pkFP, anonName sql.NullString
		var isActive int

		err := rows.Scan(&session.ID, &userName, &pkFP, &anonName, &session.RemoteAddr,
			&session.CreatedAt, &session.LastActiveAt, &isActive)
		if err != nil {
			return nil, err
		}

		session.UserName = userName.String
		session.PublicKeyFingerprint = pkFP.String
		session.AnonymousName = anonName.String
		session.IsActive = isActive == 1

		sessions = append(sessions, &session)
	}

	return sessions, rows.Err()
}

// RecordQuery records a query execution.
func (s *Store) RecordQuery(record *QueryRecord) error {
	_, err := s.db.Exec(`
		INSERT INTO query_history (session_id, database_path, query, execution_time_ms, rows_affected, error, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, record.SessionID, record.DatabasePath, record.Query, record.ExecutionTimeMs,
		record.RowsAffected, nullString(record.Error), record.CreatedAt)

	return err
}

// ListQueryHistory lists query history with optional filters.
func (s *Store) ListQueryHistory(sessionID, databasePath string, since time.Time, limit int) ([]*QueryRecord, error) {
	query := "SELECT id, session_id, database_path, query, execution_time_ms, rows_affected, error, created_at FROM query_history WHERE 1=1"
	args := make([]any, 0)

	if sessionID != "" {
		query += " AND session_id = ?"
		args = append(args, sessionID)
	}

	if databasePath != "" {
		query += " AND database_path = ?"
		args = append(args, databasePath)
	}

	if !since.IsZero() {
		query += " AND created_at >= ?"
		args = append(args, since)
	}

	query += " ORDER BY created_at DESC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*QueryRecord
	for rows.Next() {
		var record QueryRecord
		var errStr sql.NullString

		err := rows.Scan(&record.ID, &record.SessionID, &record.DatabasePath, &record.Query,
			&record.ExecutionTimeMs, &record.RowsAffected, &errStr, &record.CreatedAt)
		if err != nil {
			return nil, err
		}

		record.Error = errStr.String
		records = append(records, &record)
	}

	return records, rows.Err()
}

// GetQueryHistoryForUser lists query history for a specific user.
func (s *Store) GetQueryHistoryForUser(userName string, limit int) ([]*QueryRecord, error) {
	query := `
		SELECT qh.id, qh.session_id, qh.database_path, qh.query, qh.execution_time_ms, qh.rows_affected, qh.error, qh.created_at
		FROM query_history qh
		JOIN sessions s ON qh.session_id = s.id
		WHERE s.user_name = ?
		ORDER BY qh.created_at DESC
	`
	args := []any{userName}

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*QueryRecord
	for rows.Next() {
		var record QueryRecord
		var errStr sql.NullString

		err := rows.Scan(&record.ID, &record.SessionID, &record.DatabasePath, &record.Query,
			&record.ExecutionTimeMs, &record.RowsAffected, &errStr, &record.CreatedAt)
		if err != nil {
			return nil, err
		}

		record.Error = errStr.String
		records = append(records, &record)
	}

	return records, rows.Err()
}

// RecordAudit records an audit log entry.
func (s *Store) RecordAudit(record *AuditRecord) error {
	_, err := s.db.Exec(`
		INSERT INTO audit_log (session_id, action, database_path, table_name, details, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, record.SessionID, record.Action, record.DatabasePath, nullString(record.TableName),
		nullString(record.Details), record.CreatedAt)

	return err
}

// RecordAuditSimple is a convenience method for recording audit entries.
func (s *Store) RecordAuditSimple(sessionID, action, dbPath, tableName string, details map[string]any) error {
	var detailsJSON string
	if details != nil {
		data, err := json.Marshal(details)
		if err == nil {
			detailsJSON = string(data)
		}
	}

	return s.RecordAudit(&AuditRecord{
		SessionID:    sessionID,
		Action:       action,
		DatabasePath: dbPath,
		TableName:    tableName,
		Details:      detailsJSON,
		CreatedAt:    time.Now(),
	})
}

// ListAuditLog lists audit log entries with optional filters.
func (s *Store) ListAuditLog(sessionID, action, databasePath string, since time.Time, limit int) ([]*AuditRecord, error) {
	query := "SELECT id, session_id, action, database_path, table_name, details, created_at FROM audit_log WHERE 1=1"
	args := make([]any, 0)

	if sessionID != "" {
		query += " AND session_id = ?"
		args = append(args, sessionID)
	}

	if action != "" {
		query += " AND action = ?"
		args = append(args, action)
	}

	if databasePath != "" {
		query += " AND database_path = ?"
		args = append(args, databasePath)
	}

	if !since.IsZero() {
		query += " AND created_at >= ?"
		args = append(args, since)
	}

	query += " ORDER BY created_at DESC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*AuditRecord
	for rows.Next() {
		var record AuditRecord
		var tableName, details sql.NullString

		err := rows.Scan(&record.ID, &record.SessionID, &record.Action, &record.DatabasePath,
			&tableName, &details, &record.CreatedAt)
		if err != nil {
			return nil, err
		}

		record.TableName = tableName.String
		record.Details = details.String
		records = append(records, &record)
	}

	return records, rows.Err()
}

// nullString converts an empty string to sql.NullString.
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
