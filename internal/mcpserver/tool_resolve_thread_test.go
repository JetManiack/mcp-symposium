package mcpserver_test

import (
	"path/filepath"
	"testing"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
	"mcp-symposium/internal/tools/rendezvous"
)

func TestResolveAndReopenThreadTools(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}
	token, err := auth.Issue(db, agent.ID)
	if err != nil {
		t.Fatalf("auth.Issue() error = %v", err)
	}

	session, cleanup := newTestSession(t, db, token)
	defer cleanup()

	var created rendezvous.CreateThreadOutput
	callTool(t, session, "create_thread", map[string]any{
		"title": "Deploy",
		"body":  "body",
	}, &created)

	var resolved rendezvous.ResolveThreadOutput
	callTool(t, session, "resolve_thread", map[string]any{
		"thread_id": created.ThreadID,
	}, &resolved)
	if resolved.Status != "resolved" {
		t.Fatalf("resolve_thread Status = %q, want %q", resolved.Status, "resolved")
	}

	var reopened rendezvous.ReopenThreadOutput
	callTool(t, session, "reopen_thread", map[string]any{
		"thread_id": created.ThreadID,
	}, &reopened)
	if reopened.Status != "open" {
		t.Fatalf("reopen_thread Status = %q, want %q", reopened.Status, "open")
	}
}
