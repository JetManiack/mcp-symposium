package rendezvous

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/activity"
	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
)

const (
	// WaitMaxTimeout caps one wait_for_activity call. Long enough that a
	// client polls the server a handful of times per hour, short enough
	// that proxies and LBs in front of the server don't kill the request
	// first (Cloudflare's default read timeout is 100 s — stay under it).
	WaitMaxTimeout = 90 * time.Second

	defaultWaitTimeout = WaitMaxTimeout

	// waitPollInterval is the safety-net re-check of the database while
	// blocked. The hub delivers same-replica writes instantly; this bounds
	// the delay for writes on another replica or signals lost to a restart.
	waitPollInterval = 2 * time.Second
)

type WaitForActivityInput struct {
	TimeoutSeconds int `json:"timeout_s,omitempty" jsonschema:"how long to block waiting for news, in seconds (default and max 90)"`
}

type WaitForActivityOutput struct {
	ActorID string `json:"actor_id"`
	// Activity is true when something new is waiting; call catch_up to
	// collect it. False means the timeout expired quietly.
	Activity           bool       `json:"activity"`
	UnreadReplyCount   int        `json:"unread_reply_count"`
	UnseenMentionCount int        `json:"unseen_mention_count"`
	MaxCreatedAt       *time.Time `json:"max_created_at,omitempty"`
}

// waitForActivityHandler blocks until the calling actor has a new unread
// reply or unseen mention, or the timeout expires. It never marks anything
// read — pair it with catch_up, which stays the single consuming read.
func waitForActivityHandler(db *gorm.DB, hub *activity.Hub) mcp.ToolHandlerFor[WaitForActivityInput, WaitForActivityOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in WaitForActivityInput) (*mcp.CallToolResult, WaitForActivityOutput, error) {
		actor, ok := auth.ActorFromContext(ctx)
		if !ok {
			return nil, WaitForActivityOutput{}, fmt.Errorf("no authenticated actor in context")
		}

		timeout := defaultWaitTimeout
		if in.TimeoutSeconds > 0 {
			timeout = time.Duration(in.TimeoutSeconds) * time.Second
			if timeout > WaitMaxTimeout {
				timeout = WaitMaxTimeout
			}
		}

		signal, cancel := hub.Register(actor.ID)
		defer cancel()

		deadline := time.NewTimer(timeout)
		defer deadline.Stop()
		poll := time.NewTicker(waitPollInterval)
		defer poll.Stop()

		check := func() (*WaitForActivityOutput, error) {
			summary, err := storage.GetCatchUpSummary(db, actor.ID)
			if err != nil {
				return nil, err
			}
			if summary.UnreadReplyCount == 0 && summary.UnseenMentionCount == 0 && summary.UnseenHandoffAlertCount == 0 {
				return nil, nil
			}
			maxAt, err := storage.LatestActivityAt(db, actor.ID)
			if err != nil {
				return nil, err
			}
			return &WaitForActivityOutput{
				ActorID:            actor.ID,
				Activity:           true,
				UnreadReplyCount:   summary.UnreadReplyCount,
				UnseenMentionCount: summary.UnseenMentionCount,
				MaxCreatedAt:       maxAt,
			}, nil
		}

		// Anything that arrived before this call blocked must be reported
		// immediately — the waiter would otherwise sleep on news it already
		// has.
		if out, err := check(); err != nil || out != nil {
			if err != nil {
				return nil, WaitForActivityOutput{}, err
			}
			return nil, *out, nil
		}

		for {
			signalled := false
			select {
			case <-ctx.Done():
				return nil, WaitForActivityOutput{ActorID: actor.ID}, ctx.Err()
			case <-deadline.C:
				return nil, WaitForActivityOutput{ActorID: actor.ID, Activity: false}, nil
			case <-signal:
				signalled = true
			case <-poll.C:
			}
			out, err := check()
			if err != nil {
				return nil, WaitForActivityOutput{}, err
			}
			if out != nil {
				return nil, *out, nil
			}
			// A hub registration is one-shot (consumed by Notify). If it
			// fired but check() saw nothing — the signal raced the writing
			// transaction's commit, or referred to activity catch_up already
			// consumed — re-arm it and keep waiting on the poll meanwhile.
			if signalled {
				cancel()
				signal, cancel = hub.Register(actor.ID)
				defer cancel()
			}
		}
	}
}
