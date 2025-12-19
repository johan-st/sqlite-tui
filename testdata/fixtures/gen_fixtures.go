//go:build ignore

// gen_fixtures generates test fixture databases.
// Run with: go run gen_fixtures.go
package main

import (
	"database/sql"
	"log"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	if err := generateUsers(); err != nil {
		log.Fatalf("Failed to generate users.db: %v", err)
	}
	if err := generateEmpty(); err != nil {
		log.Fatalf("Failed to generate empty.db: %v", err)
	}
	if err := generateLarge(); err != nil {
		log.Fatalf("Failed to generate large.db: %v", err)
	}
	log.Println("All fixtures generated successfully")
}

// users.db - Standard test database with users and posts tables
func generateUsers() error {
	os.Remove("users.db")
	db, err := sql.Open("sqlite", "users.db")
	if err != nil {
		return err
	}
	defer db.Close()

	// Create tables
	_, err = db.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			email TEXT UNIQUE NOT NULL,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		);
		
		CREATE TABLE posts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			content TEXT,
			published INTEGER DEFAULT 0,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);
		
		CREATE TABLE sensitive_data (
			id INTEGER PRIMARY KEY,
			secret TEXT NOT NULL
		);
		
		-- Insert test data
		INSERT INTO users (name, email) VALUES 
			('Alice', 'alice@example.com'),
			('Bob', 'bob@example.com'),
			('Charlie', 'charlie@example.com');
		
		INSERT INTO posts (user_id, title, content, published) VALUES
			(1, 'Hello World', 'First post content', 1),
			(1, 'Draft Post', 'Work in progress', 0),
			(2, 'Bob''s Post', 'Hello from Bob', 1);
		
		INSERT INTO sensitive_data (id, secret) VALUES
			(1, 'TOP_SECRET_VALUE_123'),
			(2, 'CONFIDENTIAL_DATA_456');
	`)
	if err != nil {
		return err
	}

	log.Println("Generated users.db")
	return nil
}

// empty.db - Database with tables but no data
func generateEmpty() error {
	os.Remove("empty.db")
	db, err := sql.Open("sqlite", "empty.db")
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			value REAL
		);
		
		CREATE TABLE logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message TEXT,
			level TEXT DEFAULT 'INFO',
			timestamp TEXT DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return err
	}

	log.Println("Generated empty.db")
	return nil
}

// large.db - Database with many rows for pagination tests
func generateLarge() error {
	os.Remove("large.db")
	db, err := sql.Open("sqlite", "large.db")
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			value INTEGER,
			category TEXT
		);
	`)
	if err != nil {
		return err
	}

	// Insert many rows
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare("INSERT INTO records (name, value, category) VALUES (?, ?, ?)")
	if err != nil {
		tx.Rollback()
		return err
	}

	categories := []string{"A", "B", "C", "D"}
	for i := 1; i <= 1000; i++ {
		name := "Record " + itoa(i)
		value := i * 10
		category := categories[i%4]
		if _, err := stmt.Exec(name, value, category); err != nil {
			tx.Rollback()
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	log.Println("Generated large.db with 1000 rows")
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

