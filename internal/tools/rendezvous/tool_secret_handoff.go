package rendezvous

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/activity"
	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/secrethandoff"
	"mcp-symposium/internal/storage"
)

// maxEnvelopeBytes bounds what the server will store per handoff. Secrets
// are tokens and small config files, not archives.
const maxEnvelopeBytes = 64 * 1024

// --- set_key_bundle / get_key_bundle -----------------------------------

type SetKeyBundleInput struct {
	// BundleJSON is the operator-signed bundle, verbatim. The server stores
	// it opaquely; clients verify the operator signature themselves.
	BundleJSON string `json:"bundle_json" jsonschema:"the operator-signed key bundle JSON for the calling actor"`
}

type SetKeyBundleOutput struct {
	ActorID     string `json:"actor_id"`
	Fingerprint string `json:"fingerprint"`
}

func setKeyBundleHandler(db *gorm.DB) mcp.ToolHandlerFor[SetKeyBundleInput, SetKeyBundleOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SetKeyBundleInput) (*mcp.CallToolResult, SetKeyBundleOutput, error) {
		actor, ok := auth.ActorFromContext(ctx)
		if !ok {
			return nil, SetKeyBundleOutput{}, fmt.Errorf("no authenticated actor in context")
		}
		if len(in.BundleJSON) > 16*1024 {
			return nil, SetKeyBundleOutput{}, fmt.Errorf("bundle too large")
		}
		// Shape check only — the server can't verify the operator signature
		// (it doesn't hold the operator key, by design) but it can refuse
		// obvious garbage and bundles claiming someone else's name.
		var b secrethandoff.KeyBundle
		if err := json.Unmarshal([]byte(in.BundleJSON), &b); err != nil {
			return nil, SetKeyBundleOutput{}, fmt.Errorf("bundle is not valid JSON: %w", err)
		}
		if len(b.EncPubKey) != 32 || len(b.SignPubKey) != 32 || len(b.OperatorSig) == 0 {
			return nil, SetKeyBundleOutput{}, fmt.Errorf("bundle is missing keys or the operator signature")
		}
		// The bundle's actor must be the caller: otherwise a stolen token can
		// publish someone else's (or an old rotated) signed bundle under its
		// own slot and confuse senders.
		if named, err := storage.FindActorByName(db, b.Actor); err != nil || named.ID != actor.ID {
			return nil, SetKeyBundleOutput{}, fmt.Errorf("bundle is for actor %q, which is not the calling actor", b.Actor)
		}
		if err := storage.UpsertKeyBundle(db, actor.ID, in.BundleJSON); err != nil {
			return nil, SetKeyBundleOutput{}, err
		}
		return nil, SetKeyBundleOutput{ActorID: actor.ID, Fingerprint: secrethandoff.Fingerprint(&b)}, nil
	}
}

type GetKeyBundleInput struct {
	Actor string `json:"actor" jsonschema:"nickname or display name of the actor whose bundle to fetch"`
}

type GetKeyBundleOutput struct {
	ActorID    string `json:"actor_id"`
	BundleJSON string `json:"bundle_json"`
}

func getKeyBundleHandler(db *gorm.DB) mcp.ToolHandlerFor[GetKeyBundleInput, GetKeyBundleOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in GetKeyBundleInput) (*mcp.CallToolResult, GetKeyBundleOutput, error) {
		target, err := storage.FindActorByName(db, in.Actor)
		if err != nil {
			return nil, GetKeyBundleOutput{}, err
		}
		rec, err := storage.GetKeyBundle(db, target.ID)
		if err != nil {
			return nil, GetKeyBundleOutput{}, fmt.Errorf("no key bundle published for %q", in.Actor)
		}
		return nil, GetKeyBundleOutput{ActorID: target.ID, BundleJSON: rec.BundleJSON}, nil
	}
}

// --- send_secret / fetch_secret ----------------------------------------

type SendSecretInput struct {
	To           string `json:"to" jsonschema:"recipient actor nickname or display name"`
	EnvelopeJSON string `json:"envelope_json" jsonschema:"the sealed+signed envelope produced by the board-secret CLI"`
}

type SendSecretOutput struct {
	SecretID  string    `json:"secret_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

func sendSecretHandler(db *gorm.DB, hub *activity.Hub) mcp.ToolHandlerFor[SendSecretInput, SendSecretOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SendSecretInput) (*mcp.CallToolResult, SendSecretOutput, error) {
		actor, ok := auth.ActorFromContext(ctx)
		if !ok {
			return nil, SendSecretOutput{}, fmt.Errorf("no authenticated actor in context")
		}
		if len(in.EnvelopeJSON) > maxEnvelopeBytes {
			return nil, SendSecretOutput{}, fmt.Errorf("envelope exceeds %d bytes", maxEnvelopeBytes)
		}
		env, err := secrethandoff.UnmarshalEnvelope([]byte(in.EnvelopeJSON))
		if err != nil {
			return nil, SendSecretOutput{}, fmt.Errorf("envelope is not valid JSON: %w", err)
		}
		h := env.Header
		if h.V != 1 {
			return nil, SendSecretOutput{}, fmt.Errorf("unsupported envelope version %d", h.V)
		}
		ttl := time.Duration(h.TTLSeconds) * time.Second
		if ttl <= 0 || ttl > secrethandoff.MaxTTL {
			return nil, SendSecretOutput{}, fmt.Errorf("ttl_s must be in (0, %d]", int(secrethandoff.MaxTTL.Seconds()))
		}
		if h.Class != secrethandoff.ClassRoutine && h.Class != secrethandoff.ClassAdmin {
			return nil, SendSecretOutput{}, fmt.Errorf("class must be routine or admin")
		}

		// The envelope's From must be the calling actor: the metadata row,
		// operator feed and foreign-fetch alarms all trust it, so a caller
		// must not be able to send as someone else.
		if named, err := storage.FindActorByName(db, h.From); err != nil || named.ID != actor.ID {
			return nil, SendSecretOutput{}, fmt.Errorf("envelope From=%q is not the calling actor", h.From)
		}
		// Likewise To must match the addressed recipient.
		if h.To != in.To {
			return nil, SendSecretOutput{}, fmt.Errorf("envelope To=%q does not match the to argument %q", h.To, in.To)
		}

		recipient, err := storage.FindActorByName(db, in.To)
		if err != nil {
			return nil, SendSecretOutput{}, err
		}
		if recipient.ID == actor.ID {
			return nil, SendSecretOutput{}, fmt.Errorf("cannot send a secret to yourself")
		}

		// Housekeeping on the write path: expired blobs don't outlive the
		// next send.
		_ = storage.SweepExpiredSecrets(db, time.Now())

		id := h.SecretID
		if id == "" {
			id = uuid.NewString()
		}
		now := time.Now()
		rec := &storage.SecretHandoff{
			ID:           id,
			FromActorID:  actor.ID,
			ToActorID:    recipient.ID,
			Label:        h.Label,
			Class:        h.Class,
			EnvelopeJSON: in.EnvelopeJSON,
			CreatedAt:    now,
			ExpiresAt:    now.Add(ttl),
		}
		if err := storage.CreateSecretHandoff(db, rec); err != nil {
			return nil, SendSecretOutput{}, err
		}

		// Wake the recipient's wait_for_activity the same way a reply would.
		// The envelope itself never rides a notification — only the nudge.
		hub.Notify(recipient.ID)

		return nil, SendSecretOutput{SecretID: rec.ID, ExpiresAt: rec.ExpiresAt}, nil
	}
}

type FetchSecretInput struct {
	SecretID string `json:"secret_id" jsonschema:"id returned by send_secret (relayed to the recipient out of band or via a thread)"`
}

type FetchSecretOutput struct {
	SecretID     string `json:"secret_id"`
	EnvelopeJSON string `json:"envelope_json"`
}

func fetchSecretHandler(db *gorm.DB, hub *activity.Hub) mcp.ToolHandlerFor[FetchSecretInput, FetchSecretOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in FetchSecretInput) (*mcp.CallToolResult, FetchSecretOutput, error) {
		actor, ok := auth.ActorFromContext(ctx)
		if !ok {
			return nil, FetchSecretOutput{}, fmt.Errorf("no authenticated actor in context")
		}

		rec, envelope, err := storage.FetchSecretHandoff(db, in.SecretID, actor.ID, time.Now())
		switch {
		case err == nil:
			return nil, FetchSecretOutput{SecretID: in.SecretID, EnvelopeJSON: envelope}, nil
		case errors.Is(err, storage.ErrSecretForeignFetch):
			// Theft marker: someone who isn't the recipient asked for the
			// blob. Wake the sender so their session sees it; the metadata
			// row (foreign_attempts) is the operator's audit trail.
			if rec != nil {
				hub.Notify(rec.FromActorID)
			}
			return nil, FetchSecretOutput{}, fmt.Errorf("secret %q is not addressed to you; the sender and operator have been notified", in.SecretID)
		case errors.Is(err, storage.ErrSecretAlreadyFetched):
			when, who := "", ""
			if rec != nil && rec.FetchedAt != nil {
				when, who = rec.FetchedAt.UTC().Format(time.RFC3339), rec.FetchedByActor
			}
			if rec != nil {
				hub.Notify(rec.FromActorID)
			}
			return nil, FetchSecretOutput{}, fmt.Errorf(
				"secret %q was already fetched at %s by actor %s — if that wasn't you, treat the credential as burned and tell the operator",
				in.SecretID, when, who)
		case errors.Is(err, storage.ErrSecretNeedsApproval):
			return nil, FetchSecretOutput{}, fmt.Errorf("secret %q is admin-class and needs operator approval before release", in.SecretID)
		default:
			return nil, FetchSecretOutput{}, err
		}
	}
}
