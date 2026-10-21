package storage_test

import (
	"testing"

	"mcp-symposium/internal/storage"
)

// TestReplyMentionCarriesThreadID pins the fix for the field bug where a
// reply mention carried only reply_id: the board-wait watcher (and any
// client resolving a mention) needs thread_id without a search.
func TestReplyMentionCarriesThreadID(t *testing.T) {
	db := openTestDB(t)
	agentA, _ := storage.CreateAgent(db, "agent-a")
	agentB, _ := storage.CreateAgent(db, "agent-b")
	thread, err := storage.CreateThread(db, agentA.ID, "T", "opening post", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}
	reply, err := storage.AddReply(db, thread.ID, agentB.ID, "ping @agent-a", nil)
	if err != nil {
		t.Fatalf("AddReply() error = %v", err)
	}

	var mentions []storage.Mention
	if err := db.Where("mentioned_actor_id = ?", agentA.ID).Find(&mentions).Error; err != nil {
		t.Fatalf("load mentions: %v", err)
	}
	if len(mentions) != 1 {
		t.Fatalf("got %d mentions, want 1", len(mentions))
	}
	m := mentions[0]
	if m.ReplyID == nil || *m.ReplyID != reply.ID {
		t.Errorf("ReplyID = %v, want %q", m.ReplyID, reply.ID)
	}
	if m.ThreadID == nil || *m.ThreadID != thread.ID {
		t.Errorf("ThreadID = %v, want %q — reply mentions must carry the thread id too", m.ThreadID, thread.ID)
	}
}

// TestMentionedActorIDsForThread_ExcludesReplyMentions proves the opening
// post helper doesn't pick up reply mentions now that those carry thread_id.
func TestMentionedActorIDsForThread_ExcludesReplyMentions(t *testing.T) {
	db := openTestDB(t)
	agentA, _ := storage.CreateAgent(db, "agent-a")
	agentB, _ := storage.CreateAgent(db, "agent-b")
	if _, err := storage.CreateAgent(db, "agent-c"); err != nil {
		t.Fatalf("CreateAgent(agent-c) error = %v", err)
	}
	thread, err := storage.CreateThread(db, agentA.ID, "T", "hello @agent-b", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}
	if _, err := storage.AddReply(db, thread.ID, agentB.ID, "cc @agent-c", nil); err != nil {
		t.Fatalf("AddReply() error = %v", err)
	}

	ids, err := storage.MentionedActorIDsForThread(db, thread.ID)
	if err != nil {
		t.Fatalf("MentionedActorIDsForThread() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != agentB.ID {
		t.Errorf("ids = %v, want only the opening post's mention of agent-b (%s)", ids, agentB.ID)
	}
}

// TestLatestActivityAt reports the newest unread item and nil when caught up.
func TestLatestActivityAt(t *testing.T) {
	db := openTestDB(t)
	agentA, _ := storage.CreateAgent(db, "agent-a")
	agentB, _ := storage.CreateAgent(db, "agent-b")

	at, err := storage.LatestActivityAt(db, agentA.ID)
	if err != nil {
		t.Fatalf("LatestActivityAt() error = %v", err)
	}
	if at != nil {
		t.Errorf("LatestActivityAt() = %v, want nil before any activity", at)
	}

	thread, _ := storage.CreateThread(db, agentA.ID, "T", "post", nil)
	reply, err := storage.AddReply(db, thread.ID, agentB.ID, "news", nil)
	if err != nil {
		t.Fatalf("AddReply() error = %v", err)
	}

	at, err = storage.LatestActivityAt(db, agentA.ID)
	if err != nil {
		t.Fatalf("LatestActivityAt() error = %v", err)
	}
	if at == nil || !at.Equal(reply.CreatedAt) {
		t.Errorf("LatestActivityAt() = %v, want the reply's CreatedAt %v", at, reply.CreatedAt)
	}

	if _, err := storage.CatchUp(db, agentA.ID); err != nil {
		t.Fatalf("CatchUp() error = %v", err)
	}
	at, err = storage.LatestActivityAt(db, agentA.ID)
	if err != nil {
		t.Fatalf("LatestActivityAt() error = %v", err)
	}
	if at != nil {
		t.Errorf("LatestActivityAt() = %v, want nil after catch_up consumed everything", at)
	}
}
