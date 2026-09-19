package storage_test

import (
	"testing"

	"mcp-symposium/internal/storage"
)

func TestSearch_FindsMatchingThreadsAndReplies(t *testing.T) {
	db := openTestDB(t)
	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	thread, err := storage.CreateThread(db, agent.ID, "Deploy pipeline broken", "The nightly deploy pipeline is failing on staging.", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}
	if _, err := storage.CreateThread(db, agent.ID, "Unrelated thread", "Nothing to do with deployments.", nil); err != nil {
		t.Fatalf("CreateThread(unrelated) error = %v", err)
	}

	reply, err := storage.AddReply(db, thread.ID, agent.ID, "Fixed the staging deploy pipeline config.", nil)
	if err != nil {
		t.Fatalf("AddReply() error = %v", err)
	}

	result, err := storage.Search(db, "pipeline", 20)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Threads) != 1 || result.Threads[0].ID != thread.ID {
		t.Fatalf("Threads = %+v, want exactly the deploy-pipeline thread", result.Threads)
	}
	if len(result.Replies) != 1 || result.Replies[0].ID != reply.ID {
		t.Fatalf("Replies = %+v, want exactly the one matching reply", result.Replies)
	}
}

func TestSearch_ReturnsEmptyForNoMatches(t *testing.T) {
	db := openTestDB(t)
	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}
	if _, err := storage.CreateThread(db, agent.ID, "Deploy pipeline broken", "body", nil); err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	result, err := storage.Search(db, "nonexistent-keyword-xyz", 20)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Threads) != 0 {
		t.Errorf("Threads = %+v, want empty", result.Threads)
	}
	if len(result.Replies) != 0 {
		t.Errorf("Replies = %+v, want empty", result.Replies)
	}
}

func TestSearch_MatchesAllWordsRegardlessOfOrder(t *testing.T) {
	db := openTestDB(t)
	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	thread, err := storage.CreateThread(db, agent.ID, "Nightly job broke", "The pipeline for deploy runs failed overnight.", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	result, err := storage.Search(db, "pipeline deploy", 20)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Threads) != 1 || result.Threads[0].ID != thread.ID {
		t.Fatalf("Threads = %+v, want the thread even though \"pipeline\" and \"deploy\" aren't adjacent/in query order in the body", result.Threads)
	}
}

// TestSearch_MatchesThreadsByTagName guards a real gap found in manual
// testing: FTS only indexes title/body, so searching for a tag name (e.g.
// "ops") that never appears in the thread's own text found nothing. A
// query word that case-insensitively equals a tag name on a thread must
// surface that thread even with zero title/body overlap.
func TestSearch_MatchesThreadsByTagName(t *testing.T) {
	db := openTestDB(t)
	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	tagged, err := storage.CreateThread(db, agent.ID, "Deploy pipeline broken", "Nightly deploy is failing.", []string{"ops", "urgent"})
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}
	if _, err := storage.CreateThread(db, agent.ID, "Unrelated thread", "Nothing to do with any of this.", nil); err != nil {
		t.Fatalf("CreateThread(unrelated) error = %v", err)
	}

	result, err := storage.Search(db, "ops", 20)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Threads) != 1 || result.Threads[0].ID != tagged.ID {
		t.Fatalf("Threads = %+v, want exactly the thread tagged \"ops\" even though \"ops\" appears in neither its title nor body", result.Threads)
	}
}

// TestSearch_TagAndFTSMatchesAreDeduplicated guards against the same
// thread appearing twice when it matches both by body text and by a tag
// name for the same query.
func TestSearch_TagAndFTSMatchesAreDeduplicated(t *testing.T) {
	db := openTestDB(t)
	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	thread, err := storage.CreateThread(db, agent.ID, "ops runbook", "How to run the ops rotation.", []string{"ops"})
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	result, err := storage.Search(db, "ops", 20)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Threads) != 1 || result.Threads[0].ID != thread.ID {
		t.Fatalf("Threads = %+v, want exactly one entry for the thread matching both by body text and tag name", result.Threads)
	}
}

func TestSearch_EmptyQueryReturnsEmptyWithoutError(t *testing.T) {
	db := openTestDB(t)
	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}
	if _, err := storage.CreateThread(db, agent.ID, "Some thread", "body", nil); err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	result, err := storage.Search(db, "   ", 20)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Threads) != 0 || len(result.Replies) != 0 {
		t.Errorf("result = %+v, want empty for a whitespace-only query", result)
	}
}
