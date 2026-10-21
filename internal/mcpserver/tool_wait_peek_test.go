package mcpserver_test

import (
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
	"mcp-symposium/internal/tools/rendezvous"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return db
}

func newAgentWithToken(t *testing.T, db *gorm.DB, name string) (*storage.Actor, string) {
	t.Helper()
	actor, err := storage.CreateAgent(db, name)
	if err != nil {
		t.Fatalf("CreateAgent(%s) error = %v", name, err)
	}
	token, err := auth.Issue(db, actor.ID)
	if err != nil {
		t.Fatalf("auth.Issue() error = %v", err)
	}
	return actor, token
}

// TestPeekIsNonDestructive proves peek reports news without consuming it:
// the whole point of having it next to catch_up.
func TestPeekIsNonDestructive(t *testing.T) {
	db := openTestDB(t)
	agentA, tokenA := newAgentWithToken(t, db, "peek-a")
	agentB, _ := newAgentWithToken(t, db, "peek-b")

	session, cleanup := newTestSession(t, db, tokenA)
	defer cleanup()

	thread, err := storage.CreateThread(db, agentA.ID, "T", "post", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}
	if _, err := storage.AddReply(db, thread.ID, agentB.ID, "news for @peek-a", nil); err != nil {
		t.Fatalf("AddReply() error = %v", err)
	}

	var peek rendezvous.PeekOutput
	callTool(t, session, "peek", map[string]any{}, &peek)
	if peek.UnreadReplyCount != 1 || peek.UnseenMentionCount != 1 {
		t.Fatalf("peek = %+v, want 1 unread reply and 1 unseen mention", peek)
	}
	if peek.MaxCreatedAt == nil {
		t.Fatal("peek.MaxCreatedAt = nil, want the reply's timestamp")
	}

	// A second peek sees the same thing: nothing was consumed.
	var again rendezvous.PeekOutput
	callTool(t, session, "peek", map[string]any{}, &again)
	if again.UnreadReplyCount != 1 || again.UnseenMentionCount != 1 {
		t.Fatalf("second peek = %+v, want identical counts (peek must not consume)", again)
	}

	// catch_up still delivers everything.
	var caught rendezvous.CatchUpOutput
	callTool(t, session, "catch_up", map[string]any{}, &caught)
	if len(caught.UnreadReplies) != 1 || len(caught.NewMentions) != 1 {
		t.Fatalf("catch_up after peek = %+v, want full delivery", caught)
	}
}

// TestWaitForActivityReturnsImmediatelyWhenNewsWaiting covers the "don't
// sleep on news you already have" path.
func TestWaitForActivityReturnsImmediatelyWhenNewsWaiting(t *testing.T) {
	db := openTestDB(t)
	agentA, tokenA := newAgentWithToken(t, db, "wait-a")
	agentB, _ := newAgentWithToken(t, db, "wait-b")

	session, cleanup := newTestSession(t, db, tokenA)
	defer cleanup()

	thread, _ := storage.CreateThread(db, agentA.ID, "T", "post", nil)
	if _, err := storage.AddReply(db, thread.ID, agentB.ID, "already here", nil); err != nil {
		t.Fatalf("AddReply() error = %v", err)
	}

	start := time.Now()
	var out rendezvous.WaitForActivityOutput
	callTool(t, session, "wait_for_activity", map[string]any{"timeout_s": 30}, &out)
	if !out.Activity {
		t.Fatalf("wait_for_activity = %+v, want Activity=true", out)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("wait_for_activity blocked %v on pre-existing news, want immediate return", elapsed)
	}
}

// TestWaitForActivityWakesOnReply covers the long-poll path: a waiter with
// no news blocks, a reply through the MCP tool wakes it well before its
// timeout.
func TestWaitForActivityWakesOnReply(t *testing.T) {
	db := openTestDB(t)
	agentA, tokenA := newAgentWithToken(t, db, "wake-a")
	_, tokenB := newAgentWithToken(t, db, "wake-b")

	sessionA, cleanupA := newTestSession(t, db, tokenA)
	defer cleanupA()
	sessionB, cleanupB := newTestSession(t, db, tokenB)
	defer cleanupB()

	thread, err := storage.CreateThread(db, agentA.ID, "T", "post", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	type waitResult struct {
		out rendezvous.WaitForActivityOutput
		at  time.Time
	}
	done := make(chan waitResult, 1)
	go func() {
		var out rendezvous.WaitForActivityOutput
		callTool(t, sessionA, "wait_for_activity", map[string]any{"timeout_s": 60}, &out)
		done <- waitResult{out, time.Now()}
	}()

	// Give the waiter a moment to block before producing the activity.
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	var reply rendezvous.ReplyOutput
	callTool(t, sessionB, "reply", map[string]any{
		"thread_id": thread.ID,
		"body":      "wake up",
	}, &reply)

	select {
	case res := <-done:
		if !res.out.Activity {
			t.Fatalf("wait_for_activity = %+v, want Activity=true", res.out)
		}
		if woke := res.at.Sub(start); woke > 10*time.Second {
			t.Fatalf("waiter woke after %v, want well under the 60s timeout", woke)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("wait_for_activity never woke on the reply")
	}
}
