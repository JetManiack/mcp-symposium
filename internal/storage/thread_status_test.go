package storage_test

import (
	"testing"

	"mcp-symposium/internal/storage"
)

func TestResolveThread_SetsStatusToResolved(t *testing.T) {
	db := openTestDB(t)
	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}
	thread, err := storage.CreateThread(db, agent.ID, "Deploy", "body", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	resolved, err := storage.ResolveThread(db, thread.ID)
	if err != nil {
		t.Fatalf("ResolveThread() error = %v", err)
	}
	if resolved.Status != "resolved" {
		t.Errorf("returned Status = %q, want %q", resolved.Status, "resolved")
	}

	var persisted storage.Thread
	if err := db.First(&persisted, "id = ?", thread.ID).Error; err != nil {
		t.Fatalf("First() error = %v", err)
	}
	if persisted.Status != "resolved" {
		t.Errorf("persisted Status = %q, want %q", persisted.Status, "resolved")
	}
}

func TestReopenThread_SetsStatusBackToOpen(t *testing.T) {
	db := openTestDB(t)
	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}
	thread, err := storage.CreateThread(db, agent.ID, "Deploy", "body", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}
	if _, err := storage.ResolveThread(db, thread.ID); err != nil {
		t.Fatalf("ResolveThread() error = %v", err)
	}

	reopened, err := storage.ReopenThread(db, thread.ID)
	if err != nil {
		t.Fatalf("ReopenThread() error = %v", err)
	}
	if reopened.Status != "open" {
		t.Errorf("Status = %q, want %q", reopened.Status, "open")
	}

	var persisted storage.Thread
	if err := db.First(&persisted, "id = ?", thread.ID).Error; err != nil {
		t.Fatalf("First() error = %v", err)
	}
	if persisted.Status != "open" {
		t.Errorf("persisted Status = %q, want %q", persisted.Status, "open")
	}
}

func TestResolveThread_ReturnsErrorForNonexistentThread(t *testing.T) {
	db := openTestDB(t)

	if _, err := storage.ResolveThread(db, "nonexistent-id"); err == nil {
		t.Fatal("ResolveThread() error = nil, want an error for a nonexistent thread")
	}
}

func TestReopenThread_ReturnsErrorForNonexistentThread(t *testing.T) {
	db := openTestDB(t)

	if _, err := storage.ReopenThread(db, "nonexistent-id"); err == nil {
		t.Fatal("ReopenThread() error = nil, want an error for a nonexistent thread")
	}
}
