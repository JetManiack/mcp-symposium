package storage

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// KeyBundleRecord stores an agent's operator-signed key bundle verbatim.
// The server never verifies or uses the keys — clients verify the operator
// signature themselves (the trust root lives on the clients, board thread
// a6dc0141), the server is only the distribution point.
type KeyBundleRecord struct {
	ActorID    string    `gorm:"type:char(36);primaryKey" json:"actor_id"`
	BundleJSON string    `gorm:"type:text;not null;default:''" json:"bundle_json"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// SecretHandoff is one encrypted credential in transit. The blob lives in
// its own table, never in threads/replies/FTS/catch_up bodies. EnvelopeJSON
// is cleared on first fetch or at TTL expiry; the metadata row survives as
// the operator-visible audit trail.
type SecretHandoff struct {
	ID          string `gorm:"type:char(36);primaryKey" json:"id"`
	FromActorID string `gorm:"type:char(36);not null;index" json:"from_actor_id"`
	ToActorID   string `gorm:"type:char(36);not null;index" json:"to_actor_id"`
	Label       string `gorm:"type:varchar(255);not null;default:''" json:"label"`
	Class       string `gorm:"type:varchar(16);not null;default:'routine'" json:"class"`

	// EnvelopeJSON is the opaque sealed envelope. Emptied on fetch/expiry.
	EnvelopeJSON string `gorm:"type:text" json:"-"`

	CreatedAt time.Time `gorm:"index" json:"created_at"`
	ExpiresAt time.Time `gorm:"index" json:"expires_at"`

	// Audit: who fetched (or tried), when.
	FetchedAt      *time.Time `json:"fetched_at,omitempty"`
	FetchedByActor string     `gorm:"type:char(36)" json:"fetched_by_actor,omitempty"`
	// ForeignAttempts counts fetch attempts by anyone other than the
	// recipient — a direct marker of a stolen token.
	ForeignAttempts int `gorm:"not null;default:0" json:"foreign_attempts"`

	// Admin-class releases need this set by the operator first.
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
}

// Named errors so the tool layer can map each refusal to a distinct,
// accurate message (and alarm where the spec demands one).
var (
	ErrSecretNotFound       = errors.New("secret not found or expired")
	ErrSecretAlreadyFetched = errors.New("secret was already fetched")
	ErrSecretForeignFetch   = errors.New("secret is addressed to a different actor")
	ErrSecretNeedsApproval  = errors.New("admin-class secret needs operator approval before release")
)

// CreateSecretHandoff stores a new envelope.
func CreateSecretHandoff(db *gorm.DB, h *SecretHandoff) error {
	return db.Create(h).Error
}

// FetchSecretHandoff releases the envelope to actorID exactly once, inside
// one transaction. Any refusal leaves an audit trace:
//   - fetch by anyone but the recipient increments ForeignAttempts and
//     returns ErrSecretForeignFetch (the caller alarms sender + operator);
//   - a second fetch by the recipient returns ErrSecretAlreadyFetched with
//     the row, so the caller can say who got it and when;
//   - an unapproved admin-class secret returns ErrSecretNeedsApproval.
func FetchSecretHandoff(db *gorm.DB, id, actorID string, now time.Time) (*SecretHandoff, string, error) {
	var envelope string
	var out SecretHandoff
	err := db.Transaction(func(tx *gorm.DB) error {
		var h SecretHandoff
		if err := tx.First(&h, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSecretNotFound
			}
			return err
		}
		out = h

		if h.ToActorID != actorID {
			return ErrSecretForeignFetch
		}
		// "Already fetched" outranks "expired": it's the theft alarm the
		// spec wants the legitimate recipient to see, even late.
		if h.FetchedAt != nil {
			return ErrSecretAlreadyFetched
		}
		if now.After(h.ExpiresAt) || h.EnvelopeJSON == "" {
			return ErrSecretNotFound
		}
		if h.Class == "admin" && h.ApprovedAt == nil {
			return ErrSecretNeedsApproval
		}

		envelope = h.EnvelopeJSON
		if err := tx.Model(&SecretHandoff{}).Where("id = ?", id).Updates(map[string]interface{}{
			"envelope_json":    "",
			"fetched_at":       now,
			"fetched_by_actor": actorID,
		}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		// The foreign-attempt counter must survive the refusal, so it is
		// written outside the (rolled-back) transaction.
		if errors.Is(err, ErrSecretForeignFetch) {
			db.Model(&SecretHandoff{}).Where("id = ?", id).
				UpdateColumn("foreign_attempts", gorm.Expr("foreign_attempts + 1"))
		}
		return &out, "", err
	}
	out.FetchedAt = &now
	out.FetchedByActor = actorID
	return &out, envelope, nil
}

// SweepExpiredSecrets blanks the blobs of expired, unfetched handoffs.
// Metadata rows stay for the audit feed. Idempotent; call it from the
// write paths or a ticker.
func SweepExpiredSecrets(db *gorm.DB, now time.Time) error {
	return db.Model(&SecretHandoff{}).
		Where("expires_at < ? AND fetched_at IS NULL AND envelope_json <> ''", now).
		UpdateColumn("envelope_json", "").Error
}

// ApproveSecretHandoff marks an admin-class handoff releasable. Human/REST
// side only — there is deliberately no MCP tool for this.
func ApproveSecretHandoff(db *gorm.DB, id string, now time.Time) error {
	res := db.Model(&SecretHandoff{}).Where("id = ? AND approved_at IS NULL", id).
		Update("approved_at", now)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("handoff %q not found or already approved", id)
	}
	return nil
}

// FindActorByName resolves a nickname or display name to an Actor with the
// same precedence rules as @mentions (exact nickname > exact display name >
// case-folded, ambiguity is an error).
func FindActorByName(db *gorm.DB, name string) (*Actor, error) {
	actor, ok, err := resolveMentionTarget(db, name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("no actor named %q (or the name is ambiguous)", name)
	}
	return &actor, nil
}

// UpsertKeyBundle stores actorID's bundle verbatim.
func UpsertKeyBundle(db *gorm.DB, actorID, bundleJSON string) error {
	rec := KeyBundleRecord{ActorID: actorID, BundleJSON: bundleJSON, UpdatedAt: time.Now()}
	return db.Save(&rec).Error
}

// GetKeyBundle returns actorID's stored bundle, or ErrSecretNotFound.
func GetKeyBundle(db *gorm.DB, actorID string) (*KeyBundleRecord, error) {
	var rec KeyBundleRecord
	if err := db.First(&rec, "actor_id = ?", actorID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSecretNotFound
		}
		return nil, err
	}
	return &rec, nil
}

// ListSecretHandoffs returns newest-first metadata (never blobs) for the
// operator feed.
func ListSecretHandoffs(db *gorm.DB, limit int) ([]SecretHandoff, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []SecretHandoff
	if err := db.Select("id", "from_actor_id", "to_actor_id", "label", "class",
		"created_at", "expires_at", "fetched_at", "fetched_by_actor", "foreign_attempts", "approved_at").
		Order("created_at desc").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
