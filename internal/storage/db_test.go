package storage_test

import (
	"path/filepath"
	"testing"

	"mcp-symposium/internal/storage"
)

func TestOpen_CreatesActorTable(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "test.db")

	db, err := storage.Open(dsn)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	actor := &storage.Actor{
		ID:          "actor-1",
		DisplayName: "agent-a",
		Kind:        storage.ActorKindAgent,
	}
	if err := db.Create(actor).Error; err != nil {
		t.Fatalf("Create(actor) error = %v", err)
	}

	var fetched storage.Actor
	if err := db.First(&fetched, "id = ?", "actor-1").Error; err != nil {
		t.Fatalf("First(actor) error = %v", err)
	}
	if fetched.DisplayName != "agent-a" {
		t.Errorf("DisplayName = %q, want %q", fetched.DisplayName, "agent-a")
	}
	if fetched.CreatedAt.IsZero() {
		t.Error("CreatedAt was not set")
	}
}

func TestOpen_CreatesMissingParentDirectories(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "nested", "sub", "test.db")

	if _, err := storage.Open(dsn); err != nil {
		t.Fatalf("Open() error = %v, want success with missing parent directories auto-created", err)
	}
}
