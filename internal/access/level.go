// Package access provides access level types and resolution for database permissions.
package access

import "strings"

// Level represents the access level a user has to a database.
type Level int

const (
	// None means the user cannot see or access the database.
	None Level = iota
	// ReadOnly allows browsing data, viewing schema, SELECT queries, and exports.
	ReadOnly
	// ReadWrite allows all read operations plus INSERT/UPDATE/DELETE and schema changes.
	ReadWrite
	// Admin allows all operations including DROP and raw file downloads.
	Admin
)

// String returns the string representation of the access level.
func (l Level) String() string {
	switch l {
	case None:
		return "none"
	case ReadOnly:
		return "read-only"
	case ReadWrite:
		return "read-write"
	case Admin:
		return "admin"
	default:
		return "unknown"
	}
}

// ParseLevel parses a string into an access Level.
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "none", "no-access":
		return None
	case "read-only", "readonly", "ro":
		return ReadOnly
	case "read-write", "readwrite", "rw":
		return ReadWrite
	case "admin":
		return Admin
	default:
		return None
	}
}

// CanRead returns true if the level allows read operations.
func (l Level) CanRead() bool {
	return l >= ReadOnly
}

// CanWrite returns true if the level allows write operations.
func (l Level) CanWrite() bool {
	return l >= ReadWrite
}

// CanAdmin returns true if the level allows admin operations.
func (l Level) CanAdmin() bool {
	return l >= Admin
}

// CanDownload returns true if the level allows downloading the raw database file.
// Anyone with read access can download since they can already see all the data.
func (l Level) CanDownload() bool {
	return l >= ReadOnly
}
