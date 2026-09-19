package storage_test

import (
	"errors"
	"testing"

	"mcp-symposium/internal/storage"
)

func TestCreateThread_AddsAuthorAsWatcher(t *testing.T) {
	db := openTestDB(t)
	author, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	thread, err := storage.CreateThread(db, author.ID, "New feature X", "Shipped feature X, see docs.", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}
	if thread.Status != "open" {
		t.Errorf("Status = %q, want %q", thread.Status, "open")
	}
	if thread.Title != "New feature X" {
		t.Errorf("Title = %q, want %q", thread.Title, "New feature X")
	}

	var watcher storage.Watcher
	if err := db.First(&watcher, "thread_id = ? AND actor_id = ?", thread.ID, author.ID).Error; err != nil {
		t.Fatalf("expected author to be a watcher: %v", err)
	}

	var watch storage.ThreadWatch
	if err := db.First(&watch, "thread_id = ? AND actor_id = ?", thread.ID, author.ID).Error; err != nil {
		t.Fatalf("expected a ThreadWatch row for the author: %v", err)
	}
	if watch.LastReadAt.IsZero() {
		t.Error("LastReadAt was not set")
	}
}

func TestCreateThread_CreatesMentionFromBody(t *testing.T) {
	db := openTestDB(t)
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}

	thread, err := storage.CreateThread(db, agentA.ID, "Deploy request", "Please review, cc @agent-b", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	var mention storage.Mention
	if err := db.First(&mention, "thread_id = ? AND mentioned_actor_id = ?", thread.ID, agentB.ID).Error; err != nil {
		t.Fatalf("expected mention of agent-b: %v", err)
	}
}

func TestCreateThread_CreatesMentionFromBody_CaseInsensitiveDisplayName(t *testing.T) {
	db := openTestDB(t)
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "Agent-B")
	if err != nil {
		t.Fatalf("CreateAgent(Agent-B) error = %v", err)
	}

	thread, err := storage.CreateThread(db, agentA.ID, "Deploy request", "Please review, cc @agent-b", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	var mention storage.Mention
	if err := db.First(&mention, "thread_id = ? AND mentioned_actor_id = ?", thread.ID, agentB.ID).Error; err != nil {
		t.Fatalf("expected case-insensitive display_name match for @agent-b against display_name %q: %v", agentB.DisplayName, err)
	}
}

func TestCreateThread_CreatesMentionFromBody_CaseInsensitiveNickname(t *testing.T) {
	db := openTestDB(t)
	agentA, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) error = %v", err)
	}
	agentB, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}
	if _, err := storage.UpsertActorProfile(db, agentB.ID, "Agent B", "Agent-B-Nick", "", nil); err != nil {
		t.Fatalf("UpsertActorProfile() error = %v", err)
	}

	thread, err := storage.CreateThread(db, agentA.ID, "Deploy request", "Please review, cc @agent-b-nick", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	var mention storage.Mention
	if err := db.First(&mention, "thread_id = ? AND mentioned_actor_id = ?", thread.ID, agentB.ID).Error; err != nil {
		t.Fatalf("expected case-insensitive nickname match for @agent-b-nick against nickname Agent-B-Nick: %v", err)
	}
}

func TestCreateThread_MentionAmbiguousAfterCaseFold_ResolvesToNobody(t *testing.T) {
	db := openTestDB(t)
	author, err := storage.CreateAgent(db, "author")
	if err != nil {
		t.Fatalf("CreateAgent(author) error = %v", err)
	}
	agentX, err := storage.CreateAgent(db, "placeholder-x")
	if err != nil {
		t.Fatalf("CreateAgent(placeholder-x) error = %v", err)
	}
	agentY, err := storage.CreateAgent(db, "placeholder-y")
	if err != nil {
		t.Fatalf("CreateAgent(placeholder-y) error = %v", err)
	}
	if _, err := storage.UpsertActorProfile(db, agentX.ID, "X", "alpha", "", nil); err != nil {
		t.Fatalf("UpsertActorProfile(x) error = %v", err)
	}
	if _, err := storage.UpsertActorProfile(db, agentY.ID, "Y", "Alpha", "", nil); err != nil {
		t.Fatalf("UpsertActorProfile(y) error = %v", err)
	}

	// Neither "alpha" nor "Alpha" exactly, so this only matches via the
	// case-folded lookup — where it matches BOTH agentX's and agentY's
	// nickname. Ambiguous: must resolve to nobody, not pick one arbitrarily.
	thread, err := storage.CreateThread(db, author.ID, "Ambiguous mention", "cc @AlPha", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	var count int64
	if err := db.Model(&storage.Mention{}).Where("thread_id = ?", thread.ID).Count(&count).Error; err != nil {
		t.Fatalf("count mentions: %v", err)
	}
	if count != 0 {
		t.Errorf("mention count = %d, want 0 (ambiguous @AlPha must resolve to nobody, not an arbitrary actor)", count)
	}
}

func TestCreateThread_SameActorMentionedByTwoSpellingsInOnePost_CreatesOneMentionNotTwo(t *testing.T) {
	db := openTestDB(t)
	author, err := storage.CreateAgent(db, "author")
	if err != nil {
		t.Fatalf("CreateAgent(author) error = %v", err)
	}
	target, err := storage.CreateAgent(db, "Claude-on-Mac")
	if err != nil {
		t.Fatalf("CreateAgent(Claude-on-Mac) error = %v", err)
	}
	if _, err := storage.UpsertActorProfile(db, target.ID, "Claude on Mac", "claude-on-mac", "", nil); err != nil {
		t.Fatalf("UpsertActorProfile() error = %v", err)
	}

	// "@Claude-on-Mac" matches display_name exactly; "@claude-on-mac" matches
	// the nickname exactly. Same actor, two distinct spellings, one post.
	thread, err := storage.CreateThread(db, author.ID, "Two spellings", "@Claude-on-Mac see also @claude-on-mac", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	var count int64
	if err := db.Model(&storage.Mention{}).Where("thread_id = ? AND mentioned_actor_id = ?", thread.ID, target.ID).Count(&count).Error; err != nil {
		t.Fatalf("count mentions: %v", err)
	}
	if count != 1 {
		t.Errorf("mention count for target = %d, want 1 (two spellings of the same actor in one post must dedupe by actor, not by literal string)", count)
	}
}

func TestCreateThread_MentionOnlyInsideFencedCodeBlock_DoesNotCreateMention(t *testing.T) {
	db := openTestDB(t)
	author, err := storage.CreateAgent(db, "author")
	if err != nil {
		t.Fatalf("CreateAgent(author) error = %v", err)
	}
	target, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}

	body := "Some log output:\n```\n@agent-b   (example, not a real ping)\n```\n"
	thread, err := storage.CreateThread(db, author.ID, "Pasted log", body, nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	var count int64
	if err := db.Model(&storage.Mention{}).Where("thread_id = ? AND mentioned_actor_id = ?", thread.ID, target.ID).Count(&count).Error; err != nil {
		t.Fatalf("count mentions: %v", err)
	}
	if count != 0 {
		t.Errorf("mention count = %d, want 0 (a handle appearing only inside a fenced code block must not notify)", count)
	}
}

func TestCreateThread_MentionInsideInlineCodeSpan_DoesNotCreateMention(t *testing.T) {
	db := openTestDB(t)
	author, err := storage.CreateAgent(db, "author")
	if err != nil {
		t.Fatalf("CreateAgent(author) error = %v", err)
	}
	target, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}

	thread, err := storage.CreateThread(db, author.ID, "Inline span", "everyone types `@agent-b` out of habit", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	var count int64
	if err := db.Model(&storage.Mention{}).Where("thread_id = ? AND mentioned_actor_id = ?", thread.ID, target.ID).Count(&count).Error; err != nil {
		t.Fatalf("count mentions: %v", err)
	}
	if count != 0 {
		t.Errorf("mention count = %d, want 0 (a handle appearing only inside an inline code span must not notify)", count)
	}
}

func TestCreateThread_MentionInBothProseAndCodeBlock_CreatesExactlyOneMention(t *testing.T) {
	db := openTestDB(t)
	author, err := storage.CreateAgent(db, "author")
	if err != nil {
		t.Fatalf("CreateAgent(author) error = %v", err)
	}
	target, err := storage.CreateAgent(db, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}

	body := "cc @agent-b, and for reference here's the example format:\n```\n@agent-b   (reply, exact case)   -> row created\n```\n"
	thread, err := storage.CreateThread(db, author.ID, "Prose and code", body, nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	var count int64
	if err := db.Model(&storage.Mention{}).Where("thread_id = ? AND mentioned_actor_id = ?", thread.ID, target.ID).Count(&count).Error; err != nil {
		t.Fatalf("count mentions: %v", err)
	}
	if count != 1 {
		t.Errorf("mention count = %d, want 1 (excluding the code occurrence must not drop the legitimate prose ping)", count)
	}
}

func TestCreateThread_MentionReport_ListsResolvedAndUnresolvedHandles(t *testing.T) {
	db := openTestDB(t)
	author, err := storage.CreateAgent(db, "author")
	if err != nil {
		t.Fatalf("CreateAgent(author) error = %v", err)
	}
	if _, err := storage.CreateAgent(db, "agent-b"); err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}

	thread, err := storage.CreateThread(db, author.ID, "Deploy request", "cc @agent-b and @nonexistent-actor", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	if got, want := thread.MentionReport.Resolved, []string{"agent-b"}; !equalStrings(got, want) {
		t.Errorf("MentionReport.Resolved = %v, want %v", got, want)
	}
	if got, want := thread.MentionReport.Unresolved, []string{"nonexistent-actor"}; !equalStrings(got, want) {
		t.Errorf("MentionReport.Unresolved = %v, want %v", got, want)
	}
}

func TestAddReply_MentionReport_ListsResolvedAndUnresolvedHandles(t *testing.T) {
	db := openTestDB(t)
	author, err := storage.CreateAgent(db, "author")
	if err != nil {
		t.Fatalf("CreateAgent(author) error = %v", err)
	}
	if _, err := storage.CreateAgent(db, "agent-b"); err != nil {
		t.Fatalf("CreateAgent(agent-b) error = %v", err)
	}
	thread, err := storage.CreateThread(db, author.ID, "Deploy request", "no mentions here", nil)
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	reply, err := storage.AddReply(db, thread.ID, author.ID, "cc @agent-b and @nonexistent-actor", nil)
	if err != nil {
		t.Fatalf("AddReply() error = %v", err)
	}

	if got, want := reply.MentionReport.Resolved, []string{"agent-b"}; !equalStrings(got, want) {
		t.Errorf("MentionReport.Resolved = %v, want %v", got, want)
	}
	if got, want := reply.MentionReport.Unresolved, []string{"nonexistent-actor"}; !equalStrings(got, want) {
		t.Errorf("MentionReport.Unresolved = %v, want %v", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCreateThread_RejectsEmptyOrWhitespaceOnlyTitleAndBody(t *testing.T) {
	db := openTestDB(t)
	author, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	cases := []struct {
		name    string
		title   string
		body    string
		wantErr error
	}{
		{"empty title", "", "a body", storage.ErrEmptyTitle},
		{"whitespace-only title", "   ", "a body", storage.ErrEmptyTitle},
		{"empty body", "a title", "", storage.ErrEmptyBody},
		{"whitespace-only body", "a title", "  \t\n", storage.ErrEmptyBody},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := storage.CreateThread(db, author.ID, tc.title, tc.body, nil)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("CreateThread() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestCreateThread_AttachesTags(t *testing.T) {
	db := openTestDB(t)
	author, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	thread, err := storage.CreateThread(db, author.ID, "New feature X", "Shipped feature X.", []string{"feature", "deploy"})
	if err != nil {
		t.Fatalf("CreateThread() error = %v", err)
	}

	var tagCount int64
	db.Model(&storage.Tag{}).Where("name IN ?", []string{"feature", "deploy"}).Count(&tagCount)
	if tagCount != 2 {
		t.Fatalf("tag count = %d, want 2", tagCount)
	}

	var joinCount int64
	db.Model(&storage.ThreadTag{}).Where("thread_id = ?", thread.ID).Count(&joinCount)
	if joinCount != 2 {
		t.Fatalf("thread_tag join count = %d, want 2", joinCount)
	}
}

func TestCreateThread_ReusesExistingTagAcrossThreads(t *testing.T) {
	db := openTestDB(t)
	author, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	thread1, err := storage.CreateThread(db, author.ID, "Thread 1", "Body 1", []string{"shared-tag"})
	if err != nil {
		t.Fatalf("CreateThread(1) error = %v", err)
	}
	thread2, err := storage.CreateThread(db, author.ID, "Thread 2", "Body 2", []string{"shared-tag"})
	if err != nil {
		t.Fatalf("CreateThread(2) error = %v", err)
	}

	var tagCount int64
	db.Model(&storage.Tag{}).Where("name = ?", "shared-tag").Count(&tagCount)
	if tagCount != 1 {
		t.Fatalf("tag count = %d, want exactly 1 shared tag row (not duplicated)", tagCount)
	}

	var joinCount int64
	db.Model(&storage.ThreadTag{}).Where("thread_id IN ?", []string{thread1.ID, thread2.ID}).Count(&joinCount)
	if joinCount != 2 {
		t.Fatalf("thread_tag join count = %d, want 2 (one per thread)", joinCount)
	}
}
