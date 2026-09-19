package storage_test

import (
	"errors"
	"testing"

	"mcp-symposium/internal/storage"
)

func TestUpsertActorProfile_CreatesProfileWithTags(t *testing.T) {
	db := openTestDB(t)
	agent, err := storage.CreateAgent(db, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	profile, err := storage.UpsertActorProfile(db, agent.ID, "Agent A", "agent-a-nick", "I handle deploys.", []string{"deploys", "k8s"})
	if err != nil {
		t.Fatalf("UpsertActorProfile() error = %v", err)
	}
	if profile.Name != "Agent A" || profile.Nickname != "agent-a-nick" || profile.Bio != "I handle deploys." {
		t.Errorf("profile = %+v, unexpected fields", profile)
	}

	view, err := storage.GetProfileView(db, agent.ID)
	if err != nil {
		t.Fatalf("GetProfileView() error = %v", err)
	}
	if view.Profile == nil || view.Profile.Nickname != "agent-a-nick" {
		t.Fatalf("view.Profile = %+v, want nickname agent-a-nick", view.Profile)
	}
	if len(view.Tags) != 2 {
		t.Fatalf("len(view.Tags) = %d, want 2", len(view.Tags))
	}
}

func TestUpsertActorProfile_RejectsEmptyName(t *testing.T) {
	db := openTestDB(t)
	agent, _ := storage.CreateAgent(db, "agent-a")

	_, err := storage.UpsertActorProfile(db, agent.ID, "", "nick", "bio", nil)
	if !errors.Is(err, storage.ErrEmptyName) {
		t.Errorf("err = %v, want ErrEmptyName", err)
	}
}

func TestUpsertActorProfile_RejectsEmptyNickname(t *testing.T) {
	db := openTestDB(t)
	agent, _ := storage.CreateAgent(db, "agent-a")

	_, err := storage.UpsertActorProfile(db, agent.ID, "Agent A", "", "bio", nil)
	if !errors.Is(err, storage.ErrEmptyNickname) {
		t.Errorf("err = %v, want ErrEmptyNickname", err)
	}
}

func TestUpsertActorProfile_RejectsInvalidNicknameCharacters(t *testing.T) {
	db := openTestDB(t)
	agent, _ := storage.CreateAgent(db, "agent-a")

	_, err := storage.UpsertActorProfile(db, agent.ID, "Agent A", "not a nick!", "bio", nil)
	if !errors.Is(err, storage.ErrInvalidNickname) {
		t.Errorf("err = %v, want ErrInvalidNickname", err)
	}
}

func TestUpsertActorProfile_RejectsNicknameTakenByAnotherActor(t *testing.T) {
	db := openTestDB(t)
	agentA, _ := storage.CreateAgent(db, "agent-a")
	agentB, _ := storage.CreateAgent(db, "agent-b")

	if _, err := storage.UpsertActorProfile(db, agentA.ID, "Agent A", "shared-nick", "bio", nil); err != nil {
		t.Fatalf("first UpsertActorProfile() error = %v", err)
	}

	_, err := storage.UpsertActorProfile(db, agentB.ID, "Agent B", "shared-nick", "bio", nil)
	if !errors.Is(err, storage.ErrNicknameTaken) {
		t.Errorf("err = %v, want ErrNicknameTaken", err)
	}
}

func TestUpsertActorProfile_AllowsReUpsertingOwnNickname(t *testing.T) {
	db := openTestDB(t)
	agent, _ := storage.CreateAgent(db, "agent-a")

	if _, err := storage.UpsertActorProfile(db, agent.ID, "Agent A", "same-nick", "v1", nil); err != nil {
		t.Fatalf("first UpsertActorProfile() error = %v", err)
	}
	profile, err := storage.UpsertActorProfile(db, agent.ID, "Agent A", "same-nick", "v2", nil)
	if err != nil {
		t.Fatalf("second UpsertActorProfile() error = %v", err)
	}
	if profile.Bio != "v2" {
		t.Errorf("Bio = %q, want %q (re-upsert should update, not conflict with self)", profile.Bio, "v2")
	}
}

func TestGetProfileView_ActorWithoutProfile_ReturnsNilProfile(t *testing.T) {
	db := openTestDB(t)
	agent, _ := storage.CreateAgent(db, "agent-a")

	view, err := storage.GetProfileView(db, agent.ID)
	if err != nil {
		t.Fatalf("GetProfileView() error = %v", err)
	}
	if view.Profile != nil {
		t.Errorf("view.Profile = %+v, want nil for actor with no profile", view.Profile)
	}
	if view.Actor.ID != agent.ID {
		t.Errorf("view.Actor.ID = %q, want %q", view.Actor.ID, agent.ID)
	}
}

func TestListProfileViews_IncludesActorsWithAndWithoutProfiles(t *testing.T) {
	db := openTestDB(t)
	onboarded, _ := storage.CreateAgent(db, "agent-onboarded")
	bare, _ := storage.CreateAgent(db, "agent-bare")
	if _, err := storage.UpsertActorProfile(db, onboarded.ID, "Onboarded", "onboarded-nick", "bio", []string{"ops"}); err != nil {
		t.Fatalf("UpsertActorProfile() error = %v", err)
	}

	views, err := storage.ListProfileViews(db)
	if err != nil {
		t.Fatalf("ListProfileViews() error = %v", err)
	}

	var sawOnboarded, sawBare bool
	for _, v := range views {
		if v.Actor.ID == onboarded.ID {
			sawOnboarded = true
			if v.Profile == nil || v.Profile.Nickname != "onboarded-nick" {
				t.Errorf("onboarded view.Profile = %+v, want nickname onboarded-nick", v.Profile)
			}
		}
		if v.Actor.ID == bare.ID {
			sawBare = true
			if v.Profile != nil {
				t.Errorf("bare view.Profile = %+v, want nil", v.Profile)
			}
		}
	}
	if !sawOnboarded || !sawBare {
		t.Fatalf("sawOnboarded=%v sawBare=%v, want both true", sawOnboarded, sawBare)
	}
}
