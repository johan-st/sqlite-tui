package access

import (
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Rule represents an access rule for a database pattern.
type Rule struct {
	Pattern string
	Level   Level
}

// Resolver resolves access levels for users and databases.
type Resolver struct {
	// Default access level for anonymous users
	AnonymousAccess Level

	// Public database rules (accessible without auth)
	PublicRules []Rule

	// User-specific rules (keyed by username)
	UserRules map[string][]Rule

	// Admin usernames (have full access to everything)
	Admins map[string]bool
}

// NewResolver creates a new access resolver.
func NewResolver() *Resolver {
	return &Resolver{
		AnonymousAccess: None,
		PublicRules:     make([]Rule, 0),
		UserRules:       make(map[string][]Rule),
		Admins:          make(map[string]bool),
	}
}

// SetAnonymousAccess sets the default access level for anonymous users.
func (r *Resolver) SetAnonymousAccess(level Level) {
	r.AnonymousAccess = level
}

// AddAdmin marks a user as admin.
func (r *Resolver) AddAdmin(username string) {
	r.Admins[username] = true
}

// AddPublicRule adds a public database rule.
func (r *Resolver) AddPublicRule(pattern string, level Level) {
	r.PublicRules = append(r.PublicRules, Rule{Pattern: pattern, Level: level})
}

// AddUserRule adds an access rule for a specific user.
func (r *Resolver) AddUserRule(username, pattern string, level Level) {
	r.UserRules[username] = append(r.UserRules[username], Rule{Pattern: pattern, Level: level})
}

// Resolve determines the access level for a user to a specific database.
// The database can be identified by path or alias.
func (r *Resolver) Resolve(user *UserInfo, dbPath, dbAlias string) Level {
	// 1. If user is admin (either via flag or in admin list), they have full access
	if user != nil && user.IsAdmin {
		return Admin
	}
	if user != nil && !user.IsAnonymous && r.Admins[user.Name] {
		return Admin
	}

	// 2. Check user-specific rules
	if user != nil && !user.IsAnonymous {
		if rules, ok := r.UserRules[user.Name]; ok {
			if level := matchRules(rules, dbPath, dbAlias); level != None {
				return level
			}
		}
	}

	// 3. Check public rules
	if level := matchRules(r.PublicRules, dbPath, dbAlias); level != None {
		return level
	}

	// 4. Fall back to anonymous access level
	return r.AnonymousAccess
}

// matchRules finds the first matching rule and returns its level.
// Returns None if no rule matches.
func matchRules(rules []Rule, dbPath, dbAlias string) Level {
	for _, rule := range rules {
		if matchPattern(rule.Pattern, dbPath, dbAlias) {
			return rule.Level
		}
	}
	return None
}

// matchPattern checks if a pattern matches a database path or alias.
func matchPattern(pattern, dbPath, dbAlias string) bool {
	// Normalize paths for comparison
	pattern = strings.TrimSpace(pattern)
	dbPath = strings.TrimSpace(dbPath)
	dbAlias = strings.TrimSpace(dbAlias)

	// Try exact alias match first
	if dbAlias != "" && pattern == dbAlias {
		return true
	}

	// Try wildcard match against alias
	if dbAlias != "" {
		if matched, _ := doublestar.Match(pattern, dbAlias); matched {
			return true
		}
	}

	// Try exact path match
	if dbPath != "" && pattern == dbPath {
		return true
	}

	// Try glob match against path
	if dbPath != "" {
		// Handle both relative and absolute patterns
		if matched, _ := doublestar.Match(pattern, dbPath); matched {
			return true
		}

		// Also try matching just the filename
		filename := filepath.Base(dbPath)
		if matched, _ := doublestar.Match(pattern, filename); matched {
			return true
		}
	}

	// Try matching against path with wildcard pattern
	if strings.Contains(pattern, "*") {
		if matched, _ := doublestar.PathMatch(pattern, dbPath); matched {
			return true
		}
	}

	return false
}

// CanAccess returns true if the user has at least read access to the database.
func (r *Resolver) CanAccess(user *UserInfo, dbPath, dbAlias string) bool {
	return r.Resolve(user, dbPath, dbAlias).CanRead()
}

// ListAccessibleDatabases filters a list of databases to those the user can access.
func (r *Resolver) ListAccessibleDatabases(user *UserInfo, databases []DatabaseInfo) []DatabaseInfo {
	result := make([]DatabaseInfo, 0, len(databases))
	for _, db := range databases {
		level := r.Resolve(user, db.Path, db.Alias)
		if level.CanRead() {
			db.AccessLevel = level
			result = append(result, db)
		}
	}
	return result
}

// DatabaseInfo represents basic database information for access resolution.
type DatabaseInfo struct {
	Path        string
	Alias       string
	AccessLevel Level
}
