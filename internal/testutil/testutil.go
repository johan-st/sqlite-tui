// Package testutil provides test utilities for sqlite-tui tests.
package testutil

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// TestDB creates a temporary copy of a fixture database for testing.
// Returns the path to the copy and a cleanup function.
func TestDB(t *testing.T, fixtureName string) (string, func()) {
	t.Helper()

	fixtureDir := FindFixturesDir(t)
	srcPath := filepath.Join(fixtureDir, fixtureName)

	// Create temp file
	tmpDir := t.TempDir()
	dstPath := filepath.Join(tmpDir, fixtureName)

	// Copy fixture
	if err := copyFile(srcPath, dstPath); err != nil {
		t.Fatalf("failed to copy fixture %s: %v", fixtureName, err)
	}

	cleanup := func() {
		os.Remove(dstPath)
		os.Remove(dstPath + "-shm")
		os.Remove(dstPath + "-wal")
	}

	return dstPath, cleanup
}

// EmptyDB creates a new empty database for testing.
func EmptyDB(t *testing.T) (string, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "empty.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to create empty db: %v", err)
	}
	db.Close()

	cleanup := func() {
		os.Remove(dbPath)
		os.Remove(dbPath + "-shm")
		os.Remove(dbPath + "-wal")
	}

	return dbPath, cleanup
}

// FindFixturesDir locates the testdata/fixtures directory.
func FindFixturesDir(t *testing.T) string {
	t.Helper()

	// Walk up from current directory looking for testdata
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	for i := 0; i < 10; i++ {
		fixturesDir := filepath.Join(dir, "testdata", "fixtures")
		if info, err := os.Stat(fixturesDir); err == nil && info.IsDir() {
			return fixturesDir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	t.Fatalf("could not find testdata/fixtures directory")
	return ""
}

// FindGoldenDir locates the testdata/golden directory.
func FindGoldenDir(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	for i := 0; i < 10; i++ {
		goldenDir := filepath.Join(dir, "testdata", "golden")
		if info, err := os.Stat(goldenDir); err == nil && info.IsDir() {
			return goldenDir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	t.Fatalf("could not find testdata/golden directory")
	return ""
}

// Golden compares output against a golden file.
// If GOLDEN_UPDATE=1 is set, updates the golden file instead.
func Golden(t *testing.T, name string, got []byte) {
	t.Helper()

	goldenDir := FindGoldenDir(t)
	goldenPath := filepath.Join(goldenDir, name+".golden")

	if os.Getenv("GOLDEN_UPDATE") == "1" {
		if err := os.WriteFile(goldenPath, got, 0644); err != nil {
			t.Fatalf("failed to update golden file: %v", err)
		}
		t.Logf("Updated golden file: %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("golden file not found: %s\nGot:\n%s\n\nRun with GOLDEN_UPDATE=1 to create", goldenPath, got)
		}
		t.Fatalf("failed to read golden file: %v", err)
	}

	if !bytes.Equal(normalizeNewlines(got), normalizeNewlines(want)) {
		t.Errorf("output mismatch for %s\nGot:\n%s\nWant:\n%s", name, got, want)
	}
}

// GoldenJSON compares JSON output against a golden file (normalized).
func GoldenJSON(t *testing.T, name string, got []byte) {
	t.Helper()

	var gotObj any
	if err := json.Unmarshal(got, &gotObj); err != nil {
		t.Fatalf("failed to parse output as JSON: %v\nGot: %s", err, got)
	}

	normalized, err := json.MarshalIndent(gotObj, "", "  ")
	if err != nil {
		t.Fatalf("failed to normalize JSON: %v", err)
	}

	Golden(t, name, normalized)
}

// CaptureOutput captures stdout and stderr from a function.
func CaptureOutput(fn func(out, errOut io.Writer)) (stdout, stderr string) {
	var outBuf, errBuf bytes.Buffer
	fn(&outBuf, &errBuf)
	return outBuf.String(), errBuf.String()
}

// OutputCapture is a helper for capturing CLI output.
type OutputCapture struct {
	Out bytes.Buffer
	Err bytes.Buffer
}

// Stdout returns captured stdout as string.
func (c *OutputCapture) Stdout() string {
	return c.Out.String()
}

// Stderr returns captured stderr as string.
func (c *OutputCapture) Stderr() string {
	return c.Err.String()
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

func normalizeNewlines(b []byte) []byte {
	return []byte(strings.ReplaceAll(string(b), "\r\n", "\n"))
}

// MustExec executes SQL or fails the test.
func MustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("MustExec failed: %v\nQuery: %s", err, query)
	}
}

// MustQuery executes a query and scans the first row into dest.
func MustQueryRow(t *testing.T, db *sql.DB, query string, dest ...any) {
	t.Helper()
	if err := db.QueryRow(query).Scan(dest...); err != nil {
		t.Fatalf("MustQueryRow failed: %v\nQuery: %s", err, query)
	}
}
