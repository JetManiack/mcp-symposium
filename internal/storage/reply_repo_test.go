package storage_test

import (
	"errors"
	"testing"

	"mcp-symposium/internal/storage"
)

func TestAddReply_CreatesMentionAndUpdatesWatch(t *testing.T) {
	db := openTestDB(t)
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}
	thread, err := storage.CreateThread(db, agentA.ID, "Deploy", "Deploying feature X now.", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	reply, err := storage.AddReply(db, thread.ID, agentB.ID, "Hit a bug, cc @agent-a", nil)
	if err != nil {
		t.Fatalf("AddReply() error = %v", err)
	}
	if reply.Body != "Hit a bug, cc @agent-a" {
		t.Errorf("Body = %q, want the original body", reply.Body)
	}

	var mention storage.Mention
	if err := db.First(&mention, "reply_id = ? AND mentioned_actor_id = ?", reply.ID, agentA.ID).Error; err != nil {
		t.Fatalf("expected mention of agent-a: %v", err)
	}

	var watcher storage.Watcher
	if err := db.First(&watcher, "thread_id = ? AND actor_id = ?", thread.ID, agentB.ID).Error; err != nil {
		t.Fatalf("expected replier to become a watcher: %v", err)
	}

	var watch storage.ThreadWatch
	if err := db.First(&watch, "thread_id = ? AND actor_id = ?", thread.ID, agentB.ID).Error; err != nil {
		t.Fatalf("expected a ThreadWatch row for the replier: %v", err)
	}
	if !watch.LastReadAt.Equal(reply.CreatedAt) {
		t.Errorf("LastReadAt = %v, want %v (the replier's own reply time)", watch.LastReadAt, reply.CreatedAt)
	}
}

func TestAddReply_SkipsUnresolvableMention(t *testing.T) {
	db := openTestDB(t)
	agentA, _ := storage.CreateAgent(db, "agent-a")
	thread, _ := storage.CreateThread(db, agentA.ID, "Deploy", "Deploying feature X now.", nil)

	reply, err := storage.AddReply(db, thread.ID, agentA.ID, "cc @nobody-registered", nil)
	if err != nil {
		t.Fatalf("AddReply() error = %v", err)
	}

	var count int64
	db.Model(&storage.Mention{}).Where("reply_id = ?", reply.ID).Count(&count)
	if count != 0 {
		t.Errorf("mention count = %d, want 0 for an unresolvable @name", count)
	}
}

func TestAddReply_RejectsNonexistentThread(t *testing.T) {
	db := openTestDB(t)
	actor, _ := storage.CreateAgent(db, "agent-a")

	_, err := storage.AddReply(db, "nonexistent-id", actor.ID, "body", nil)
	if err == nil {
		t.Fatal("AddReply() error = nil, want a non-nil error for a nonexistent thread_id")
	}
}

func TestAddReply_RejectsEmptyOrWhitespaceOnlyBody(t *testing.T) {
	db := openTestDB(t)
	agentA, _ := storage.CreateAgent(db, "agent-a")
	thread, _ := storage.CreateThread(db, agentA.ID, "Deploy", "Deploying feature X now.", nil)

	for _, body := range []string{"", "   ", "\t\n"} {
		_, err := storage.AddReply(db, thread.ID, agentA.ID, body, nil)
		if !errors.Is(err, storage.ErrEmptyBody) {
			t.Errorf("AddReply(body=%q) error = %v, want %v", body, err, storage.ErrEmptyBody)
		}
	}
}

func TestAddReply_MentionResolvesByNicknameBeforeDisplayName(t *testing.T) {
	db := openTestDB(t)
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}
	if _, err := storage.UpsertActorProfile(db, agentB.ID, "Agent B", "bee", "bio", nil); err != nil {
		t.Fatalf("UpsertActorProfile() error = %v", err)
	}

	thread, err := storage.CreateThread(db, agentA.ID, "Deploy", "Deploying feature X now.", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	reply, err := storage.AddReply(db, thread.ID, agentA.ID, "Hit a bug, cc @bee", nil)
	if err != nil {
		t.Fatalf("AddReply() error = %v", err)
	}

	var mention storage.Mention
	if err := db.First(&mention, "reply_id = ? AND mentioned_actor_id = ?", reply.ID, agentB.ID).Error; err != nil {
		t.Fatalf("expected mention of agent-b via nickname 'bee': %v", err)
	}
}
