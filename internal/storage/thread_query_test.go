package storage_test

import (
	"testing"

	"mcp-symposium/internal/storage"
)

func TestGetThread_ReturnsRepliesAndTags(t *testing.T) {
	db := openTestDB(t)
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}

	thread, err := storage.CreateThread(db, agentA.ID, "Deploy", "Deploying feature X now.", []string{"deploy"})
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}
	reply, err := storage.AddReply(db, thread.ID, agentB.ID, "Hit a bug", nil)
	if err != nil {
		t.Fatalf("AddReply() error = %v", err)
	}

	gotThread, replies, tags, err := storage.GetThread(db, thread.ID)
	if err != nil {
		t.Fatalf("GetThread() error = %v", err)
	}
	if gotThread.ID != thread.ID {
		t.Errorf("thread.ID = %q, want %q", gotThread.ID, thread.ID)
	}
	if len(replies) != 1 || replies[0].ID != reply.ID {
		t.Fatalf("replies = %+v, want exactly the one reply", replies)
	}
	if len(tags) != 1 || tags[0].Name != "deploy" {
		t.Fatalf("tags = %+v, want exactly [deploy]", tags)
	}
}

func TestGetThread_ReturnsErrorForNonexistentThread(t *testing.T) {
	db := openTestDB(t)

	_, _, _, err := storage.GetThread(db, "nonexistent-id")
	if err == nil {
		t.Fatal("GetThread() error = nil, want an error for a nonexistent thread")
	}
}

func TestListThreads_FiltersByStatusAndTags(t *testing.T) {
	db := openTestDB(t)
	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	deployOpen, err := storage.CreateThread(db, agent.ID, "Open deploy", "body", []string{"deploy"})
	if err != nil {
		t.Fatalf("CreateThread(1) error = %v", err)
	}
	if _, err := storage.CreateThread(db, agent.ID, "Open no tag", "body", nil); err != nil {
		t.Fatalf("CreateThread(2) error = %v", err)
	}
	deployResolved, err := storage.CreateThread(db, agent.ID, "Resolved deploy", "body", []string{"deploy"})
	if err != nil {
		t.Fatalf("CreateThread(3) error = %v", err)
	}
	if err := db.Model(&storage.Thread{}).Where("id = ?", deployResolved.ID).Update("status", "resolved").Error; err != nil {
		t.Fatalf("mark resolved: %v", err)
	}

	result, err := storage.ListThreads(db, storage.ListThreadsFilter{Status: "open", Tags: []string{"deploy"}})
	if err != nil {
		t.Fatalf("ListThreads() error = %v", err)
	}
	if len(result.Threads) != 1 || result.Threads[0].ID != deployOpen.ID {
		t.Fatalf("Threads = %+v, want exactly the one open+deploy thread", result.Threads)
	}
}

func TestListThreads_PaginatesWithoutSkippingOrRepeating(t *testing.T) {
	db := openTestDB(t)
	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	created := make([]*storage.Thread, 3)
	for i := 0; i < 3; i++ {
		thread, err := storage.CreateThread(db, agent.ID, "Thread", "body", nil)
		if err != nil {
			t.Fatalf("CreateThread(%d) error = %v", i, err)
		}
		created[i] = thread
	}

	firstPage, err := storage.ListThreads(db, storage.ListThreadsFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListThreads(page 1) error = %v", err)
	}
	if len(firstPage.Threads) != 2 {
		t.Fatalf("page 1 threads = %d, want 2", len(firstPage.Threads))
	}
	if firstPage.NextCursor == "" {
		t.Fatal("page 1 NextCursor is empty, want a cursor since a 3rd thread exists")
	}

	secondPage, err := storage.ListThreads(db, storage.ListThreadsFilter{Limit: 2, Cursor: firstPage.NextCursor})
	if err != nil {
		t.Fatalf("ListThreads(page 2) error = %v", err)
	}
	if len(secondPage.Threads) != 1 {
		t.Fatalf("page 2 threads = %d, want 1", len(secondPage.Threads))
	}
	if secondPage.NextCursor != "" {
		t.Fatalf("page 2 NextCursor = %q, want empty (no more pages)", secondPage.NextCursor)
	}

	seen := map[string]bool{}
	for _, thread := range append(firstPage.Threads, secondPage.Threads...) {
		if seen[thread.ID] {
			t.Fatalf("thread %q appeared on more than one page", thread.ID)
		}
		seen[thread.ID] = true
	}
	for _, thread := range created {
		if !seen[thread.ID] {
			t.Fatalf("thread %q (created at %v) never appeared on any page", thread.ID, thread.CreatedAt)
		}
	}
}

func TestListThreads_HandlesIdenticalTimestampsWithoutSkippingOrRepeating(t *testing.T) {
	db := openTestDB(t)
	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	threadA, err := storage.CreateThread(db, agent.ID, "Thread A", "body", nil)
	if err != nil {
		t.Fatalf("CreateThread(A) error = %v", err)
	}
	threadB, err := storage.CreateThread(db, agent.ID, "Thread B", "body", nil)
	if err != nil {
		t.Fatalf("CreateThread(B) error = %v", err)
	}

	// Force a real timestamp collision: give B the exact same CreatedAt as
	// A, bypassing normal timestamp assignment, so pagination must rely on
	// the id tiebreaker rather than timing luck to avoid skipping/repeating
	// either thread.
	if err := db.Model(&storage.Thread{}).Where("id = ?", threadB.ID).
		Update("created_at", threadA.CreatedAt).Error; err != nil {
		t.Fatalf("force timestamp collision: %v", err)
	}

	firstPage, err := storage.ListThreads(db, storage.ListThreadsFilter{Limit: 1})
	if err != nil {
		t.Fatalf("ListThreads(page 1) error = %v", err)
	}
	if len(firstPage.Threads) != 1 {
		t.Fatalf("page 1 threads = %d, want 1", len(firstPage.Threads))
	}
	if firstPage.NextCursor == "" {
		t.Fatal("page 1 NextCursor is empty, want a cursor since a tied second thread exists")
	}

	secondPage, err := storage.ListThreads(db, storage.ListThreadsFilter{Limit: 1, Cursor: firstPage.NextCursor})
	if err != nil {
		t.Fatalf("ListThreads(page 2) error = %v", err)
	}
	if len(secondPage.Threads) != 1 {
		t.Fatalf("page 2 threads = %d, want 1", len(secondPage.Threads))
	}

	if firstPage.Threads[0].ID == secondPage.Threads[0].ID {
		t.Fatalf("same thread %q appeared on both pages despite the tied timestamp", firstPage.Threads[0].ID)
	}
	seen := map[string]bool{firstPage.Threads[0].ID: true, secondPage.Threads[0].ID: true}
	if !seen[threadA.ID] || !seen[threadB.ID] {
		t.Fatalf("threads seen = %v, want both %q and %q", seen, threadA.ID, threadB.ID)
	}
}
