package access

import "context"

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

const (
	// userKey is the context key for the authenticated user.
	userKey contextKey = "user"
	// sessionKey is the context key for the session info.
	sessionKey contextKey = "session"
	// levelKey is the context key for the resolved access level.
	levelKey contextKey = "access_level"
)

// UserInfo contains information about an authenticated user.
type UserInfo struct {
	Name          string
	IsAdmin       bool
	PublicKeyFP   string // SSH public key fingerprint
	IsAnonymous   bool
	AnonymousName string // Generated name for anonymous users (e.g., "azure-tiger-42")
	RemoteAddr    string
}

// SessionInfo contains session-specific information.
type SessionInfo struct {
	ID         string
	User       *UserInfo
	RemoteAddr string
}

// WithUser adds user info to the context.
func WithUser(ctx context.Context, user *UserInfo) context.Context {
	return context.WithValue(ctx, userKey, user)
}

// UserFromContext retrieves user info from the context.
func UserFromContext(ctx context.Context) (*UserInfo, bool) {
	user, ok := ctx.Value(userKey).(*UserInfo)
	return user, ok
}

// WithSession adds session info to the context.
func WithSession(ctx context.Context, session *SessionInfo) context.Context {
	return context.WithValue(ctx, sessionKey, session)
}

// SessionFromContext retrieves session info from the context.
func SessionFromContext(ctx context.Context) (*SessionInfo, bool) {
	session, ok := ctx.Value(sessionKey).(*SessionInfo)
	return session, ok
}

// WithLevel adds the access level to the context.
func WithLevel(ctx context.Context, level Level) context.Context {
	return context.WithValue(ctx, levelKey, level)
}

// LevelFromContext retrieves the access level from the context.
func LevelFromContext(ctx context.Context) (Level, bool) {
	level, ok := ctx.Value(levelKey).(Level)
	return level, ok
}

// DisplayName returns the name to display for the user.
func (u *UserInfo) DisplayName() string {
	if u == nil {
		return "unknown"
	}
	if u.IsAnonymous {
		return u.AnonymousName
	}
	return u.Name
}
