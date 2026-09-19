package storage_test

import (
	"testing"

	"mcp-symposium/internal/storage"
)

func TestGetCatchUpSummary_CountsUnreadRepliesAndUnseenMentions(t *testing.T) {
	db := openTestDB(t)
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}

	thread, err := storage.CreateThread(db, agentA.ID, "Deploy", "Deploying now, cc @agent-b", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}
	if _, err := storage.AddReply(db, thread.ID, agentA.ID, "Update: still going", []string{agentB.ID}); err != nil {
		t.Fatalf("AddReply() error = %v", err)
	}

	summary, err := storage.GetCatchUpSummary(db, agentB.ID)
	if err != nil {
		t.Fatalf("GetCatchUpSummary() error = %v", err)
	}
	if summary.UnreadReplyCount != 1 {
		t.Errorf("UnreadReplyCount = %d, want 1", summary.UnreadReplyCount)
	}
	if summary.UnseenMentionCount != 1 {
		t.Errorf("UnseenMentionCount = %d, want 1", summary.UnseenMentionCount)
	}
}

func TestGetCatchUpSummary_IsNonDestructive(t *testing.T) {
	db := openTestDB(t)
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}
	_, err = storage.CreateThread(db, agentA.ID, "Deploy", "Deploying now, cc @agent-b", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	if _, err := storage.GetCatchUpSummary(db, agentB.ID); err != nil {
		t.Fatalf("first GetCatchUpSummary() error = %v", err)
	}

	result, err := storage.CatchUp(db, agentB.ID)
	if err != nil {
		t.Fatalf("CatchUp() error = %v", err)
	}
	if len(result.NewMentions) != 1 {
		t.Fatalf("CatchUp().NewMentions after a prior GetCatchUpSummary() call = %d, want 1 (summary must not mark mentions seen)", len(result.NewMentions))
	}
}

func TestWatchersOf_ReturnsEveryWatcherIncludingAuthor(t *testing.T) {
	db := openTestDB(t)
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}
	thread, err := storage.CreateThread(db, agentA.ID, "Deploy", "Deploying now", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}
	if _, err := storage.AddReply(db, thread.ID, agentA.ID, "cc", []string{agentB.ID}); err != nil {
		t.Fatalf("AddReply() error = %v", err)
	}

	ids, err := storage.WatchersOf(db, thread.ID)
	if err != nil {
		t.Fatalf("WatchersOf() error = %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("len(WatchersOf()) = %d, want 2", len(ids))
	}
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if !seen[agentA.ID] || !seen[agentB.ID] {
		t.Errorf("WatchersOf() = %v, want both %q and %q", ids, agentA.ID, agentB.ID)
	}
}

func TestMentionedActorIDsForReply_ReturnsOnlyThatReplysMentions(t *testing.T) {
	db := openTestDB(t)
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}
	thread, err := storage.CreateThread(db, agentA.ID, "Deploy", "Deploying now", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}
	reply, err := storage.AddReply(db, thread.ID, agentA.ID, "cc @agent-b", nil)
	if err != nil {
		t.Fatalf("AddReply() error = %v", err)
	}

	ids, err := storage.MentionedActorIDsForReply(db, reply.ID)
	if err != nil {
		t.Fatalf("MentionedActorIDsForReply() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != agentB.ID {
		t.Errorf("MentionedActorIDsForReply() = %v, want [%q]", ids, agentB.ID)
	}
}

func TestMentionedActorIDsForThread_ReturnsOnlyThatThreadsBodyMentions(t *testing.T) {
	db := openTestDB(t)
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}
	thread, err := storage.CreateThread(db, agentA.ID, "Deploy", "Deploying now, cc @agent-b", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	ids, err := storage.MentionedActorIDsForThread(db, thread.ID)
	if err != nil {
		t.Fatalf("MentionedActorIDsForThread() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != agentB.ID {
		t.Errorf("MentionedActorIDsForThread() = %v, want [%q]", ids, agentB.ID)
	}
}
