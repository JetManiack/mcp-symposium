package mcpserver_test

import (
	"path/filepath"
	"testing"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
	"mcp-symposium/internal/tools/rendezvous"
)

func TestCatchUpTool(t *testing.T) {
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
	}, &created)

	sessionB, cleanupB := newTestSession(t, db, tokenB)
	defer cleanupB()

	var replied rendezvous.ReplyOutput
	callTool(t, sessionB, "reply", map[string]any{
		"thread_id": created.ThreadID,
		"body":      "Hit a bug, cc @agent-a",
	}, &replied)

	var caughtUp rendezvous.CatchUpOutput
	callTool(t, sessionA, "catch_up", map[string]any{}, &caughtUp)

	if len(caughtUp.UnreadReplies) != 1 || caughtUp.UnreadReplies[0].ID != replied.ReplyID {
		t.Fatalf("UnreadReplies = %+v, want exactly the new reply", caughtUp.UnreadReplies)
	}
	if len(caughtUp.NewMentions) != 1 {
		t.Fatalf("NewMentions = %+v, want exactly one mention", caughtUp.NewMentions)
	}

	var second rendezvous.CatchUpOutput
	callTool(t, sessionA, "catch_up", map[string]any{}, &second)
	if len(second.UnreadReplies) != 0 || len(second.NewMentions) != 0 {
		t.Errorf("second catch_up = %+v, want empty (already caught up)", second)
	}
}
