package storage_test

import (
	"os"
	"testing"

	"mcp-symposium/internal/storage"
)

func TestSearchPostgres_FindsMatchingThreadsAndReplies(t *testing.T) {
	pgDSN := os.Getenv("TEST_POSTGRES_DSN")
	if pgDSN == "" {
		t.Skip("TEST_POSTGRES_DSN not set; skipping Postgres-backed test — see docker-compose.yml")
	}
	db := openTestPostgresDB(t, pgDSN)

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

func TestSearchPostgres_MatchesAllWordsRegardlessOfOrder(t *testing.T) {
	pgDSN := os.Getenv("TEST_POSTGRES_DSN")
	if pgDSN == "" {
		t.Skip("TEST_POSTGRES_DSN not set; skipping Postgres-backed test — see docker-compose.yml")
	}
	db := openTestPostgresDB(t, pgDSN)

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

func TestSearchPostgres_MatchesThreadsByTagName(t *testing.T) {
	pgDSN := os.Getenv("TEST_POSTGRES_DSN")
	if pgDSN == "" {
		t.Skip("TEST_POSTGRES_DSN not set; skipping Postgres-backed test — see docker-compose.yml")
	}
	db := openTestPostgresDB(t, pgDSN)

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
		t.Fatalf("Threads = %+v, want exactly the thread tagged \"ops\"", result.Threads)
	}
}
