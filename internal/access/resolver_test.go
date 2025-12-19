package access

import (
	"testing"
)

func TestResolver_AdminAccess(t *testing.T) {
	r := NewResolver()
	r.AddAdmin("admin_user")

	tests := []struct {
		name      string
		user      *UserInfo
		dbPath    string
		dbAlias   string
		wantLevel Level
	}{
		{
			name:      "admin via IsAdmin flag",
			user:      &UserInfo{Name: "some_user", IsAdmin: true},
			dbPath:    "/any/path.db",
			dbAlias:   "any",
			wantLevel: Admin,
		},
		{
			name:      "admin via admin list",
			user:      &UserInfo{Name: "admin_user"},
			dbPath:    "/any/path.db",
			dbAlias:   "any",
			wantLevel: Admin,
		},
		{
			name:      "non-admin user",
			user:      &UserInfo{Name: "regular_user"},
			dbPath:    "/any/path.db",
			dbAlias:   "any",
			wantLevel: None,
		},
		{
			name:      "anonymous user gets anonymous level",
			user:      &UserInfo{Name: "anon", IsAnonymous: true},
			dbPath:    "/any/path.db",
			dbAlias:   "any",
			wantLevel: None,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Resolve(tt.user, tt.dbPath, tt.dbAlias)
			if got != tt.wantLevel {
				t.Errorf("Resolve() = %v, want %v", got, tt.wantLevel)
			}
		})
	}
}

func TestResolver_ReadOnlyUserCannotWrite(t *testing.T) {
	r := NewResolver()
	r.AddUserRule("reader", "*", ReadOnly)
	r.AddUserRule("writer", "*", ReadWrite)

	tests := []struct {
		name     string
		user     *UserInfo
		canRead  bool
		canWrite bool
	}{
		{
			name:     "read-only user",
			user:     &UserInfo{Name: "reader"},
			canRead:  true,
			canWrite: false, // CRITICAL: Must not be able to write!
		},
		{
			name:     "read-write user",
			user:     &UserInfo{Name: "writer"},
			canRead:  true,
			canWrite: true,
		},
		{
			name:     "unknown user has no access",
			user:     &UserInfo{Name: "unknown"},
			canRead:  false,
			canWrite: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level := r.Resolve(tt.user, "/data/test.db", "test")

			if level.CanRead() != tt.canRead {
				t.Errorf("CanRead() = %v, want %v", level.CanRead(), tt.canRead)
			}
			if level.CanWrite() != tt.canWrite {
				t.Errorf("CanWrite() = %v, want %v", level.CanWrite(), tt.canWrite)
			}
		})
	}
}

func TestResolver_PatternMatching(t *testing.T) {
	r := NewResolver()
	r.AddPublicRule("public_*", ReadOnly)
	r.AddPublicRule("/data/shared/*.db", ReadOnly)
	r.AddUserRule("dev", "/dev/**", ReadWrite)

	tests := []struct {
		name      string
		user      *UserInfo
		dbPath    string
		dbAlias   string
		wantLevel Level
	}{
		{
			name:      "exact alias match",
			user:      nil,
			dbPath:    "/data/public.db",
			dbAlias:   "public_data",
			wantLevel: ReadOnly,
		},
		{
			name:      "glob alias match",
			user:      nil,
			dbPath:    "/data/public.db",
			dbAlias:   "public_logs",
			wantLevel: ReadOnly,
		},
		{
			name:      "path glob match",
			user:      nil,
			dbPath:    "/data/shared/users.db",
			dbAlias:   "users",
			wantLevel: ReadOnly,
		},
		{
			name:      "no match returns None",
			user:      nil,
			dbPath:    "/private/secret.db",
			dbAlias:   "secret",
			wantLevel: None,
		},
		{
			name:      "user-specific rule with glob",
			user:      &UserInfo{Name: "dev"},
			dbPath:    "/dev/test/mydb.db",
			dbAlias:   "mydb",
			wantLevel: ReadWrite,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Resolve(tt.user, tt.dbPath, tt.dbAlias)
			if got != tt.wantLevel {
				t.Errorf("Resolve() = %v, want %v", got, tt.wantLevel)
			}
		})
	}
}

func TestResolver_RulePrecedence(t *testing.T) {
	// User-specific rules should override public rules
	r := NewResolver()
	r.AddPublicRule("shared", ReadOnly)
	r.AddUserRule("privileged", "shared", ReadWrite)

	// Public user gets ReadOnly
	publicLevel := r.Resolve(nil, "/data/shared.db", "shared")
	if publicLevel != ReadOnly {
		t.Errorf("public access = %v, want ReadOnly", publicLevel)
	}

	// Privileged user gets ReadWrite (user rules checked first)
	privilegedLevel := r.Resolve(&UserInfo{Name: "privileged"}, "/data/shared.db", "shared")
	if privilegedLevel != ReadWrite {
		t.Errorf("privileged access = %v, want ReadWrite", privilegedLevel)
	}
}

func TestLevel_AccessMethods(t *testing.T) {
	tests := []struct {
		level       Level
		canRead     bool
		canWrite    bool
		canDownload bool
		canAdmin    bool
	}{
		{None, false, false, false, false},
		{ReadOnly, true, false, true, false}, // ReadOnly can download
		{ReadWrite, true, true, true, false},
		{Admin, true, true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.level.String(), func(t *testing.T) {
			if tt.level.CanRead() != tt.canRead {
				t.Errorf("CanRead() = %v, want %v", tt.level.CanRead(), tt.canRead)
			}
			if tt.level.CanWrite() != tt.canWrite {
				t.Errorf("CanWrite() = %v, want %v", tt.level.CanWrite(), tt.canWrite)
			}
			if tt.level.CanDownload() != tt.canDownload {
				t.Errorf("CanDownload() = %v, want %v", tt.level.CanDownload(), tt.canDownload)
			}
			if tt.level.CanAdmin() != tt.canAdmin {
				t.Errorf("CanAdmin() = %v, want %v", tt.level.CanAdmin(), tt.canAdmin)
			}
		})
	}
}

func TestResolver_AnonymousAccess(t *testing.T) {
	r := NewResolver()
	r.SetAnonymousAccess(ReadOnly)
	r.AddPublicRule("protected", None) // Explicitly deny

	// Anonymous user with default access
	anonUser := &UserInfo{Name: "anon", IsAnonymous: true}

	// Should get anonymous level for unmatched databases
	level := r.Resolve(anonUser, "/data/random.db", "random")
	if level != ReadOnly {
		t.Errorf("anonymous access = %v, want ReadOnly", level)
	}

	// Should be denied for protected database
	level = r.Resolve(anonUser, "/data/protected.db", "protected")
	if level != None {
		t.Errorf("protected access = %v, want None", level)
	}
}

func TestResolver_NilUser(t *testing.T) {
	r := NewResolver()
	r.SetAnonymousAccess(ReadOnly)

	// nil user should be treated as anonymous
	level := r.Resolve(nil, "/data/test.db", "test")
	if level != ReadOnly {
		t.Errorf("nil user access = %v, want ReadOnly", level)
	}
}
