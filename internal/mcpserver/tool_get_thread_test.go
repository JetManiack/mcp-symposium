package mcpserver_test

import (
	"path/filepath"
	"testing"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
	"mcp-symposium/internal/tools/rendezvous"
)

func TestGetThreadTool(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}
	tokenA, err := auth.Issue(db, agentA.ID)
	if err != nil {
		t.Fatalf("auth.Issue(agent-a) error = %v", err)
	}
	tokenB, err := auth.Issue(db, agentB.ID)
	if err != nil {
		t.Fatalf("auth.Issue(agent-b) error = %v", err)
	}

	sessionA, cleanupA := newTestSession(t, db, tokenA)
	defer cleanupA()

	var created rendezvous.CreateThreadOutput
	callTool(t, sessionA, "create_thread", map[string]any{
		"title": "Deploy",
		"body":  "Deploying feature X now.",
		"tags":  []string{"deploy"},
	}, &created)

	sessionB, cleanupB := newTestSession(t, db, tokenB)
	defer cleanupB()

	var replied rendezvous.ReplyOutput
	callTool(t, sessionB, "reply", map[string]any{
		"thread_id": created.ThreadID,
		"body":      "Hit a bug",
	}, &replied)

	var got rendezvous.GetThreadOutput
	callTool(t, sessionA, "get_thread", map[string]any{
		"thread_id": created.ThreadID,
	}, &got)

	if got.Thread.ID != created.ThreadID {
		t.Errorf("Thread.ID = %q, want %q", got.Thread.ID, created.ThreadID)
	}
	if len(got.Replies) != 1 || got.Replies[0].ID != replied.ReplyID {
		t.Fatalf("Replies = %+v, want exactly the one reply", got.Replies)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "deploy" {
		t.Fatalf("Tags = %+v, want exactly [deploy]", got.Tags)
	}
}
