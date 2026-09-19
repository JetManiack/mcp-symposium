package storage_test

import (
	"sync"
	"testing"

	"mcp-symposium/internal/storage"
)

func TestGetOrCreateHumanActor_CreatesOnFirstCall(t *testing.T) {
	db := openTestDB(t)

	actor, err := storage.GetOrCreateHumanActor(db, "stub-user", "Local Tester", "admin")
	if err != nil {
		t.Fatalf("GetOrCreateHumanActor() error = %v", err)
	}
	if actor.Kind != storage.ActorKindHuman {
		t.Errorf("Kind = %q, want %q", actor.Kind, storage.ActorKindHuman)
	}
	if actor.DisplayName != "Local Tester" {
		t.Errorf("DisplayName = %q, want %q", actor.DisplayName, "Local Tester")
	}

	var identity storage.UserIdentity
	if err := db.First(&identity, "actor_id = ?", actor.ID).Error; err != nil {
		t.Fatalf("expected a UserIdentity row: %v", err)
	}
	if identity.KeycloakSubject != "stub-user" || identity.Role != "admin" {
		t.Errorf("identity = %+v, want subject=stub-user role=admin", identity)
	}
}

func TestGetOrCreateHumanActor_ReturnsSameActorOnSecondCall(t *testing.T) {
	db := openTestDB(t)

	first, err := storage.GetOrCreateHumanActor(db, "stub-user", "Local Tester", "admin")
	if err != nil {
		t.Fatalf("GetOrCreateHumanActor(1) error = %v", err)
	}
	second, err := storage.GetOrCreateHumanActor(db, "stub-user", "Local Tester", "admin")
	if err != nil {
		t.Fatalf("GetOrCreateHumanActor(2) error = %v", err)
	}

	if first.ID != second.ID {
		t.Errorf("second call created a new actor: first.ID = %q, second.ID = %q", first.ID, second.ID)
	}

	var count int64
	db.Model(&storage.Actor{}).Where("kind = ?", storage.ActorKindHuman).Count(&count)
	if count != 1 {
		t.Errorf("human actor count = %d, want exactly 1 (no duplicate on second call)", count)
	}
}

func TestGetOrCreateHumanActor_UpdatesRoleIfChanged(t *testing.T) {
	db := openTestDB(t)

	if _, err := storage.GetOrCreateHumanActor(db, "stub-user", "Local Tester", "viewer"); err != nil {
		t.Fatalf("GetOrCreateHumanActor(1) error = %v", err)
	}
	if _, err := storage.GetOrCreateHumanActor(db, "stub-user", "Local Tester", "admin"); err != nil {
		t.Fatalf("GetOrCreateHumanActor(2) error = %v", err)
	}

	var identity storage.UserIdentity
	if err := db.Where("keycloak_subject = ?", "stub-user").First(&identity).Error; err != nil {
		t.Fatalf("First() error = %v", err)
	}
	if identity.Role != "admin" {
		t.Errorf("Role = %q, want %q (should be updated to the latest value)", identity.Role, "admin")
	}
}

func TestGetOrCreateHumanActor_ConcurrentFirstCallsConverge(t *testing.T) {
	db := openTestDB(t)

	const n = 10
	results := make([]*storage.Actor, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = storage.GetOrCreateHumanActor(db, "concurrent-user", "Local Tester", "admin")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d error = %v", i, err)
		}
	}

	firstID := results[0].ID
	for i, actor := range results {
		if actor.ID != firstID {
			t.Errorf("call %d returned actor.ID = %q, want %q (all concurrent first-calls should converge on one actor)", i, actor.ID, firstID)
		}
	}

	var actorCount int64
	db.Model(&storage.Actor{}).Where("kind = ?", storage.ActorKindHuman).Count(&actorCount)
	if actorCount != 1 {
		t.Errorf("human actor count = %d, want exactly 1", actorCount)
	}

	var identityCount int64
	db.Model(&storage.UserIdentity{}).Where("keycloak_subject = ?", "concurrent-user").Count(&identityCount)
	if identityCount != 1 {
		t.Errorf("UserIdentity count = %d, want exactly 1", identityCount)
	}
}
