package mcpserver_test

import (
	"path/filepath"
	"testing"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
	"mcp-symposium/internal/tools/rendezvous"
)

func TestReplyTool(t *testing.T) {
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

	if replied.ReplyID == "" {
		t.Fatal("ReplyOutput.ReplyID is empty")
	}

	var mention storage.Mention
	if err := db.First(&mention, "reply_id = ? AND mentioned_actor_id = ?", replied.ReplyID, agentA.ID).Error; err != nil {
		t.Fatalf("expected mention of agent-a: %v", err)
	}

	if len(replied.Mentions.Resolved) != 1 || replied.Mentions.Resolved[0] != "agent-a" {
		t.Errorf("ReplyOutput.Mentions.Resolved = %v, want [agent-a]", replied.Mentions.Resolved)
	}
	if len(replied.Mentions.Unresolved) != 0 {
		t.Errorf("ReplyOutput.Mentions.Unresolved = %v, want empty", replied.Mentions.Unresolved)
	}
}

func TestReplyTool_ReportsUnresolvedMentions(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	tokenA, err := auth.Issue(db, agentA.ID)
	if err != nil {
		t.Fatalf("auth.Issue(agent-a) error = %v", err)
	}

	sessionA, cleanupA := newTestSession(t, db, tokenA)
	defer cleanupA()

	var created rendezvous.CreateThreadOutput
	callTool(t, sessionA, "create_thread", map[string]any{
		"title": "Deploy",
		"body":  "Deploying feature X now.",
	}, &created)

	var replied rendezvous.ReplyOutput
	callTool(t, sessionA, "reply", map[string]any{
		"thread_id": created.ThreadID,
		"body":      "cc @typo-name",
	}, &replied)

	if len(replied.Mentions.Unresolved) != 1 || replied.Mentions.Unresolved[0] != "typo-name" {
		t.Errorf("ReplyOutput.Mentions.Unresolved = %v, want [typo-name]", replied.Mentions.Unresolved)
	}
}
