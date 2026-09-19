package mcpserver_test

import (
	"path/filepath"
	"testing"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
	"mcp-symposium/internal/tools/rendezvous"
)

func TestListProfilesTool_IncludesOnboardedAndBareActors(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	onboarded, err := storage.CreateAgent(db, "agent-onboarded")
	if err != nil {
		t.Fatalf("CreateAgent(agent-onboarded) error = %v", err)
	}
	bare, err := storage.CreateAgent(db, "agent-bare")
	if err != nil {
		t.Fatalf("CreateAgent(agent-bare) error = %v", err)
	}
	if _, err := storage.UpsertActorProfile(db, onboarded.ID, "Onboarded", "onboarded-nick", "bio", []string{"ops"}); err != nil {
		t.Fatalf("UpsertActorProfile() error = %v", err)
	}
	token, err := auth.Issue(db, bare.ID)
	if err != nil {
		t.Fatalf("auth.Issue(agent-bare) error = %v", err)
	}

	session, cleanup := newTestSession(t, db, token)
	defer cleanup()

	var out rendezvous.ListProfilesOutput
	callTool(t, session, "list_profiles", map[string]any{}, &out)

	var sawOnboarded, sawBare bool
	for _, p := range out.Profiles {
		if p.ActorID == onboarded.ID {
			sawOnboarded = true
			if p.Nickname != "onboarded-nick" || len(p.Tags) != 1 || p.Tags[0] != "ops" {
				t.Errorf("onboarded entry = %+v, unexpected fields", p)
			}
		}
		if p.ActorID == bare.ID {
			sawBare = true
			if p.Nickname != "" {
				t.Errorf("bare entry = %+v, want empty Nickname", p)
			}
			if p.DisplayName != "agent-bare" {
				t.Errorf("bare entry DisplayName = %q, want %q", p.DisplayName, "agent-bare")
			}
		}
	}
	if !sawOnboarded || !sawBare {
		t.Fatalf("sawOnboarded=%v sawBare=%v, want both true", sawOnboarded, sawBare)
	}
}
