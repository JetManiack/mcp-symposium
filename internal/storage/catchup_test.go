package storage_test

import (
	"testing"

	"mcp-symposium/internal/storage"
)

func TestCatchUp_ReturnsUnreadRepliesAndMentionsOnce(t *testing.T) {
	db := openTestDB(t)
	agentA, _ := storage.CreateAgent(db, "agent-a")
	agentB, _ := storage.CreateAgent(db, "agent-b")
	thread, err := storage.CreateThread(db, agentA.ID, "Deploy", "Deploying feature X now.", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	reply, err := storage.AddReply(db, thread.ID, agentB.ID, "Hit a bug, cc @agent-a", nil)
	if err != nil {
		t.Fatalf("AddReply() error = %v", err)
	}

	result, err := storage.CatchUp(db, agentA.ID)
	if err != nil {
		t.Fatalf("CatchUp() error = %v", err)
	}
	if len(result.UnreadReplies) != 1 || result.UnreadReplies[0].ID != reply.ID {
		t.Fatalf("UnreadReplies = %+v, want exactly one reply with ID %q", result.UnreadReplies, reply.ID)
	}
	if len(result.NewMentions) != 1 || result.NewMentions[0].MentionedActorID != agentA.ID {
		t.Fatalf("NewMentions = %+v, want exactly one mention of agent-a", result.NewMentions)
	}

	second, err := storage.CatchUp(db, agentA.ID)
	if err != nil {
		t.Fatalf("second CatchUp() error = %v", err)
	}
	if len(second.UnreadReplies) != 0 {
		t.Errorf("second CatchUp() UnreadReplies = %+v, want empty (already caught up)", second.UnreadReplies)
	}
	if len(second.NewMentions) != 0 {
		t.Errorf("second CatchUp() NewMentions = %+v, want empty (already seen)", second.NewMentions)
	}
}

func TestCatchUp_ActorWithNoActivityGetsEmptyResult(t *testing.T) {
	db := openTestDB(t)
	agent, _ := storage.CreateAgent(db, "agent-solo")

	result, err := storage.CatchUp(db, agent.ID)
	if err != nil {
		t.Fatalf("CatchUp() error = %v", err)
	}
	if len(result.UnreadReplies) != 0 || len(result.NewMentions) != 0 {
		t.Errorf("CatchUp() = %+v, want empty result for an actor with no threads/mentions", result)
	}
}

// TestCatchUp_SeesActivityForExtraWatcherAddedViaReply proves that an actor
// added as an extra watcher via AddReply's extraWatcherIDs param becomes
// visible to catch_up, not just a Watcher row with no corresponding
// ThreadWatch cursor.
func TestCatchUp_SeesActivityForExtraWatcherAddedViaReply(t *testing.T) {
	db := openTestDB(t)
	agentA, _ := storage.CreateAgent(db, "agent-a")
	agentB, _ := storage.CreateAgent(db, "agent-b")
	agentC, _ := storage.CreateAgent(db, "agent-c")

	thread, err := storage.CreateThread(db, agentA.ID, "Deploy", "Deploying feature X now.", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	reply, err := storage.AddReply(db, thread.ID, agentB.ID, "body", []string{agentC.ID})
	if err != nil {
		t.Fatalf("AddReply() error = %v", err)
	}

	result, err := storage.CatchUp(db, agentC.ID)
	if err != nil {
		t.Fatalf("CatchUp() error = %v", err)
	}
	if len(result.UnreadReplies) != 1 || result.UnreadReplies[0].ID != reply.ID {
		t.Fatalf("UnreadReplies = %+v, want exactly the new reply for agent-c (newly added extra watcher)", result.UnreadReplies)
	}
}
