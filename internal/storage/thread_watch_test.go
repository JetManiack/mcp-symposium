package storage_test

import (
	"testing"

	"mcp-symposium/internal/storage"
)

func TestWatchThread_CreatesWatcherAndThreadWatchRows(t *testing.T) {
	db := openTestDB(t)
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}
	thread, err := storage.CreateThread(db, agentA.ID, "Deploy", "body", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	if err := storage.WatchThread(db, agentB.ID, thread.ID); err != nil {
		t.Fatalf("WatchThread() error = %v", err)
	}

	var watcher storage.Watcher
	if err := db.First(&watcher, "thread_id = ? AND actor_id = ?", thread.ID, agentB.ID).Error; err != nil {
		t.Fatalf("expected a Watcher row: %v", err)
	}
	var watch storage.ThreadWatch
	if err := db.First(&watch, "thread_id = ? AND actor_id = ?", thread.ID, agentB.ID).Error; err != nil {
		t.Fatalf("expected a ThreadWatch row: %v", err)
	}
}

func TestWatchThread_MakesFutureRepliesVisibleToCatchUp(t *testing.T) {
	db := openTestDB(t)
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}
	agentC, err := storage.CreateAgent(db, "agent-c")
	if err != nil {
		t.Fatalf("CreateAgent(agent-c) error = %v", err)
	}
	thread, err := storage.CreateThread(db, agentA.ID, "Deploy", "body", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	if err := storage.WatchThread(db, agentC.ID, thread.ID); err != nil {
		t.Fatalf("WatchThread() error = %v", err)
	}
	reply, err := storage.AddReply(db, thread.ID, agentB.ID, "update", nil)
	if err != nil {
		t.Fatalf("AddReply() error = %v", err)
	}

	result, err := storage.CatchUp(db, agentC.ID)
	if err != nil {
		t.Fatalf("CatchUp() error = %v", err)
	}
	if len(result.UnreadReplies) != 1 || result.UnreadReplies[0].ID != reply.ID {
		t.Fatalf("UnreadReplies = %+v, want exactly the new reply", result.UnreadReplies)
	}
}

func TestWatchThread_ReturnsErrorForNonexistentThread(t *testing.T) {
	db := openTestDB(t)
	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	if err := storage.WatchThread(db, agent.ID, "nonexistent-id"); err == nil {
		t.Fatal("WatchThread() error = nil, want an error for a nonexistent thread")
	}
}

func TestUnwatchThread_RemovesWatcherAndThreadWatchRows(t *testing.T) {
	db := openTestDB(t)
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}
	thread, err := storage.CreateThread(db, agentA.ID, "Deploy", "body", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}
	if err := storage.WatchThread(db, agentB.ID, thread.ID); err != nil {
		t.Fatalf("WatchThread() error = %v", err)
	}

	if err := storage.UnwatchThread(db, agentB.ID, thread.ID); err != nil {
		t.Fatalf("UnwatchThread() error = %v", err)
	}

	var watcherCount int64
	db.Model(&storage.Watcher{}).Where("thread_id = ? AND actor_id = ?", thread.ID, agentB.ID).Count(&watcherCount)
	if watcherCount != 0 {
		t.Errorf("Watcher rows = %d, want 0", watcherCount)
	}
	var watchCount int64
	db.Model(&storage.ThreadWatch{}).Where("thread_id = ? AND actor_id = ?", thread.ID, agentB.ID).Count(&watchCount)
	if watchCount != 0 {
		t.Errorf("ThreadWatch rows = %d, want 0", watchCount)
	}
}

func TestUnwatchThread_StopsFutureRepliesFromAppearingInCatchUp(t *testing.T) {
	db := openTestDB(t)
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}
	agentC, err := storage.CreateAgent(db, "agent-c")
	if err != nil {
		t.Fatalf("CreateAgent(agent-c) error = %v", err)
	}
	thread, err := storage.CreateThread(db, agentA.ID, "Deploy", "body", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}
	if err := storage.WatchThread(db, agentC.ID, thread.ID); err != nil {
		t.Fatalf("WatchThread() error = %v", err)
	}

	if err := storage.UnwatchThread(db, agentC.ID, thread.ID); err != nil {
		t.Fatalf("UnwatchThread() error = %v", err)
	}
	if _, err := storage.AddReply(db, thread.ID, agentB.ID, "update", nil); err != nil {
		t.Fatalf("AddReply() error = %v", err)
	}

	result, err := storage.CatchUp(db, agentC.ID)
	if err != nil {
		t.Fatalf("CatchUp() error = %v", err)
	}
	if len(result.UnreadReplies) != 0 {
		t.Errorf("UnreadReplies = %+v, want empty after unwatching", result.UnreadReplies)
	}
}
