package mcpserver_test

import (
	"path/filepath"
	"testing"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
	"mcp-symposium/internal/tools/rendezvous"
)

func TestListThreadsTool(t *testing.T) {
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

	var deployThread rendezvous.CreateThreadOutput
	callTool(t, session, "create_thread", map[string]any{
		"title": "Deploy thread",
		"body":  "body",
		"tags":  []string{"deploy"},
	}, &deployThread)

	var otherThread rendezvous.CreateThreadOutput
	callTool(t, session, "create_thread", map[string]any{
		"title": "Other thread",
		"body":  "body",
	}, &otherThread)

	var got rendezvous.ListThreadsOutput
	callTool(t, session, "list_threads", map[string]any{
		"tags": []string{"deploy"},
	}, &got)

	if len(got.Threads) != 1 || got.Threads[0].ID != deployThread.ThreadID {
		t.Fatalf("Threads = %+v, want exactly the one deploy-tagged thread", got.Threads)
	}
}
