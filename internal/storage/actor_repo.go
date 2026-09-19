package storage

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrInvalidToken     = errors.New("invalid or revoked token")
	ErrEmptyDisplayName = errors.New("display name must not be empty")
)

func CreateAgent(db *gorm.DB, displayName string) (*Actor, error) {
	if strings.TrimSpace(displayName) == "" {
		return nil, ErrEmptyDisplayName
	}

	actor := &Actor{
		ID:          uuid.NewString(),
		DisplayName: displayName,
		Kind:        ActorKindAgent,
	}
	if err := db.Create(actor).Error; err != nil {
		return nil, err
	}
	return actor, nil
}

// RevokeAgentToken revokes a single credential by its ID. Revoking a
// credential that's already revoked, or doesn't exist, is a no-op, not
// an error.
func RevokeAgentToken(db *gorm.DB, credentialID string) error {
	return db.Model(&AgentCredential{}).
		Where("id = ? AND revoked_at IS NULL", credentialID).
		Update("revoked_at", time.Now()).Error
}

// RevokeAllAgentCredentials revokes every non-revoked credential belonging
// to actorID — the bulk form of RevokeAgentToken, used when an agent is
// "deleted" via the REST API (which removes its ability to authenticate
// without deleting its Actor row or its thread/reply history).
func RevokeAllAgentCredentials(db *gorm.DB, actorID string) error {
	return db.Model(&AgentCredential{}).
		Where("actor_id = ? AND revoked_at IS NULL", actorID).
		Update("revoked_at", time.Now()).Error
}
