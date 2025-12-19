package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/johan-st/sqlite-tui/internal/access"
	"github.com/johan-st/sqlite-tui/internal/config"
	"github.com/johan-st/sqlite-tui/internal/database"
	"github.com/johan-st/sqlite-tui/internal/testutil"
)

// testEnv sets up a test environment with database manager.
type testEnv struct {
	t        *testing.T
	dbPath   string
	cleanup  func()
	manager  *database.Manager
	handler  *Handler
	adminUser    *access.UserInfo
	readOnlyUser *access.UserInfo
	anonUser     *access.UserInfo
}

func newTestEnv(t *testing.T, fixture string) *testEnv {
	t.Helper()

	dbPath, cleanup := testutil.TestDB(t, fixture)

	// Create config that points to test database
	cfg := &config.Config{
		Databases: []config.DatabaseSource{
			{Path: dbPath, Alias: "test"},
		},
		AnonymousAccess: "none",
		Users: []config.User{
			{Name: "admin", Admin: true},
			{Name: "reader", Access: []config.AccessRule{{Pattern: "*", Level: "read-only"}}},
			{Name: "writer", Access: []config.AccessRule{{Pattern: "*", Level: "read-write"}}},
		},
	}

	manager, err := database.NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Start discovery
	if err := manager.Start(); err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}

	env := &testEnv{
		t:       t,
		dbPath:  dbPath,
		cleanup: cleanup,
		manager: manager,
		handler: NewHandler(manager, nil, "test"),
		adminUser:    &access.UserInfo{Name: "admin", IsAdmin: true},
		readOnlyUser: &access.UserInfo{Name: "reader"},
		anonUser:     &access.UserInfo{Name: "anon", IsAnonymous: true},
	}

	return env
}

func (e *testEnv) Close() {
	e.manager.Stop()
	e.cleanup()
}

func (e *testEnv) run(user *access.UserInfo, args ...string) (stdout, stderr string, exitCode int) {
	var outBuf, errBuf bytes.Buffer

	ctx := &CommandContext{
		User:      user,
		DBManager: e.manager,
		Args:      args,
		Out:       &outBuf,
		Err:       &errBuf,
		exitCode:  0,
	}

	if len(args) > 0 {
		e.handler.routeCommand(args[0], &CommandContext{
			User:      user,
			DBManager: e.manager,
			Args:      args[1:], // args after command
			Out:       &outBuf,
			Err:       &errBuf,
			exitCode:  0,
		})
	}

	return outBuf.String(), errBuf.String(), ctx.exitCode
}

// --- Access Control Tests ---

func TestCLI_ReadOnlyUser_CannotInsert(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	stdout, stderr, _ := env.run(env.readOnlyUser,
		"insert", "test", "users", `--json={"name":"Hacker","email":"hack@evil.com"}`)

	if !strings.Contains(stderr, "access denied") && !strings.Contains(stderr, "no write access") {
		t.Errorf("expected access denied error, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCLI_ReadOnlyUser_CannotUpdate(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	stdout, stderr, _ := env.run(env.readOnlyUser,
		"update", "test", "users", `--where=id=1`, `--set={"name":"Hacked"}`)

	if !strings.Contains(stderr, "access denied") && !strings.Contains(stderr, "no write access") {
		t.Errorf("expected access denied error, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCLI_ReadOnlyUser_CannotDelete(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	stdout, stderr, _ := env.run(env.readOnlyUser,
		"delete", "test", "users", "--where=id=1", "--confirm")

	if !strings.Contains(stderr, "access denied") && !strings.Contains(stderr, "no write access") {
		t.Errorf("expected access denied error, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCLI_ReadOnlyUser_CannotDropTable(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	stdout, stderr, _ := env.run(env.readOnlyUser,
		"drop-table", "test", "users", "--confirm")

	if !strings.Contains(stderr, "access denied") && !strings.Contains(stderr, "no write access") {
		t.Errorf("expected access denied error, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCLI_ReadOnlyUser_CannotExecuteWriteQuery(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	stdout, stderr, _ := env.run(env.readOnlyUser,
		"query", "test", "DROP TABLE users")

	if !strings.Contains(stderr, "access denied") && !strings.Contains(stderr, "write") {
		t.Errorf("expected access denied for write query, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCLI_ReadOnlyUser_CanSelect(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	stdout, stderr, _ := env.run(env.readOnlyUser, "select", "test", "users")

	if stderr != "" {
		t.Errorf("unexpected error: %s", stderr)
	}
	if !strings.Contains(stdout, "Alice") {
		t.Errorf("expected to see 'Alice' in output, got: %s", stdout)
	}
}

// --- Safety Guard Tests ---

func TestCLI_Delete_RequiresConfirm(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	// Missing --confirm
	_, stderr, _ := env.run(env.adminUser,
		"delete", "test", "users", "--where=id=1")

	if !strings.Contains(stderr, "--confirm") {
		t.Errorf("expected error about --confirm flag, got: %s", stderr)
	}
}

func TestCLI_Delete_RequiresWhere(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	// Missing --where
	_, stderr, _ := env.run(env.adminUser,
		"delete", "test", "users", "--confirm")

	if !strings.Contains(stderr, "--where") {
		t.Errorf("expected error about --where flag, got: %s", stderr)
	}
}

func TestCLI_Update_RequiresWhere(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	// Missing --where
	_, stderr, _ := env.run(env.adminUser,
		"update", "test", "users", `--set={"name":"x"}`)

	if !strings.Contains(stderr, "--where") {
		t.Errorf("expected error about --where flag, got: %s", stderr)
	}
}

func TestCLI_DropTable_RequiresConfirm(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	// Missing --confirm
	_, stderr, _ := env.run(env.adminUser,
		"drop-table", "test", "users")

	if !strings.Contains(stderr, "--confirm") {
		t.Errorf("expected error about --confirm flag, got: %s", stderr)
	}
}

// --- Command Output Tests ---

func TestCLI_Tables_ListsTables(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	stdout, stderr, _ := env.run(env.adminUser, "tables", "test")

	if stderr != "" {
		t.Errorf("unexpected error: %s", stderr)
	}
	if !strings.Contains(stdout, "users") {
		t.Errorf("expected 'users' in output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "posts") {
		t.Errorf("expected 'posts' in output, got: %s", stdout)
	}
}

func TestCLI_Schema_ShowsSchema(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	stdout, stderr, _ := env.run(env.adminUser, "schema", "test", "users")

	if stderr != "" {
		t.Errorf("unexpected error: %s", stderr)
	}
	// Should show column info
	if !strings.Contains(stdout, "id") {
		t.Errorf("expected 'id' column in output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "name") {
		t.Errorf("expected 'name' column in output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "email") {
		t.Errorf("expected 'email' column in output, got: %s", stdout)
	}
}

func TestCLI_Count_ReturnsCount(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	stdout, stderr, _ := env.run(env.adminUser, "count", "test", "users")

	if stderr != "" {
		t.Errorf("unexpected error: %s", stderr)
	}
	// Should output "3" for 3 users
	if !strings.Contains(strings.TrimSpace(stdout), "3") {
		t.Errorf("expected count of 3, got: %s", stdout)
	}
}

func TestCLI_Query_SelectReturnsData(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	stdout, stderr, _ := env.run(env.adminUser, "query", "test", "SELECT name FROM users WHERE id = 1")

	if stderr != "" {
		t.Errorf("unexpected error: %s", stderr)
	}
	if !strings.Contains(stdout, "Alice") {
		t.Errorf("expected 'Alice' in output, got: %s", stdout)
	}
}

// --- Anonymous Access Tests ---

func TestCLI_Anonymous_CannotAccessByDefault(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	_, stderr, _ := env.run(env.anonUser, "select", "test", "users")

	// Should be denied since anonymous access is "none"
	if !strings.Contains(stderr, "access denied") && !strings.Contains(stderr, "no read access") {
		t.Errorf("expected access denied for anonymous user, got: %s", stderr)
	}
}

// --- Unknown Command Tests ---

func TestCLI_UnknownCommand(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	_, stderr, _ := env.run(env.adminUser, "nonexistent-command")

	if !strings.Contains(stderr, "Unknown command") {
		t.Errorf("expected 'Unknown command' error, got: %s", stderr)
	}
}

// --- Missing Argument Tests ---

func TestCLI_Insert_MissingJSON(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	_, stderr, _ := env.run(env.adminUser, "insert", "test", "users")

	if !strings.Contains(stderr, "--json") {
		t.Errorf("expected error about --json flag, got: %s", stderr)
	}
}

func TestCLI_Update_MissingSet(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	_, stderr, _ := env.run(env.adminUser, "update", "test", "users", "--where=id=1")

	if !strings.Contains(stderr, "--set") {
		t.Errorf("expected error about --set flag, got: %s", stderr)
	}
}

// --- JSON Output Tests ---

func TestCLI_Select_JSONFormat(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	stdout, stderr, _ := env.run(env.adminUser, "select", "test", "users", "--format=json")

	if stderr != "" {
		t.Errorf("unexpected error: %s", stderr)
	}
	// Should be valid JSON array
	if !strings.HasPrefix(strings.TrimSpace(stdout), "[") {
		t.Errorf("expected JSON array output, got: %s", stdout)
	}
	if !strings.Contains(stdout, `"Alice"`) {
		t.Errorf("expected Alice in JSON output, got: %s", stdout)
	}
}

func TestCLI_Count_JSONFormat(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	stdout, stderr, _ := env.run(env.adminUser, "count", "test", "users", "--format=json")

	if stderr != "" {
		t.Errorf("unexpected error: %s", stderr)
	}
	if !strings.Contains(stdout, `"count"`) {
		t.Errorf("expected JSON with count field, got: %s", stdout)
	}
}

// --- Help and Version Tests ---

func TestCLI_Help(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	stdout, _, _ := env.run(env.adminUser, "help")

	if !strings.Contains(stdout, "ls") || !strings.Contains(stdout, "query") {
		t.Errorf("expected help to list commands, got: %s", stdout)
	}
}

func TestCLI_Version(t *testing.T) {
	env := newTestEnv(t, "users.db")
	defer env.Close()

	stdout, _, _ := env.run(env.adminUser, "version")

	if !strings.Contains(stdout, "test") {
		t.Errorf("expected version string, got: %s", stdout)
	}
}

