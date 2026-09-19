package storage_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcp-symposium/internal/storage"
)

func TestOpen_CreatesSessionTable(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	session := &storage.Session{
		ID:           "session-1",
		Subject:      "keycloak-subject-1",
		DisplayName:  "Jane Admin",
		Role:         "admin",
		RefreshToken: "refresh-token-value",
		ExpiresAt:    time.Now().Add(15 * time.Minute),
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("Create(session) error = %v", err)
	}

	var fetched storage.Session
	if err := db.First(&fetched, "id = ?", "session-1").Error; err != nil {
		t.Fatalf("First(session) error = %v", err)
	}
	if fetched.Subject != "keycloak-subject-1" {
		t.Errorf("Subject = %q, want %q", fetched.Subject, "keycloak-subject-1")
	}
	if fetched.Role != "admin" {
		t.Errorf("Role = %q, want %q", fetched.Role, "admin")
	}
	if fetched.RefreshToken != "refresh-token-value" {
		t.Errorf("RefreshToken = %q, want %q", fetched.RefreshToken, "refresh-token-value")
	}
	if fetched.CreatedAt.IsZero() {
		t.Error("CreatedAt was not set")
	}
}

// TestOpen_SessionIDColumnFitsA64CharHash guards a real bug: Session.ID
// stores a SHA-256 hex digest (humanauth.hashSessionID) of the session
// cookie value, not a UUID — 64 characters, not 36. SQLite's "char(36)"
// type hint is unenforced (any length fits), so this bug was invisible
// on SQLite; Postgres enforces CHARACTER(n) column widths and rejects
// longer values outright. This test is gated on TEST_POSTGRES_DSN since
// that's the only backend that actually enforces the column width — see
// docker-compose.yml for how to run it locally.
func TestOpen_SessionIDColumnFitsA64CharHash(t *testing.T) {
	pgDSN := os.Getenv("TEST_POSTGRES_DSN")
	if pgDSN == "" {
		t.Skip("TEST_POSTGRES_DSN not set; skipping Postgres-backed test — see docker-compose.yml")
	}
	db := openTestPostgresDB(t, pgDSN)

	sixtyFourChars := strings.Repeat("a", 64)
	session := &storage.Session{
		ID:           sixtyFourChars,
		Subject:      "keycloak-subject-1",
		DisplayName:  "Jane Admin",
		Role:         "admin",
		RefreshToken: "refresh-token-value",
		ExpiresAt:    time.Now().Add(15 * time.Minute),
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("Create(session) with a 64-char ID error = %v (Session.ID's column is too narrow for a SHA-256 hex digest)", err)
	}
}
