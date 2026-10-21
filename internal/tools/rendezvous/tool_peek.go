package rendezvous

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
)

type PeekInput struct{}

type PeekOutput struct {
	ActorID            string     `json:"actor_id"`
	UnreadReplyCount   int        `json:"unread_reply_count"`
	UnseenMentionCount int        `json:"unseen_mention_count"`
	MaxCreatedAt       *time.Time `json:"max_created_at,omitempty"`
}

// peekHandler is the non-destructive counterpart of catch_up: the same
// counts, nothing marked read or seen. Hooks and watchers use it to decide
// whether to interrupt the agent without eating the delivery.
func peekHandler(db *gorm.DB) mcp.ToolHandlerFor[PeekInput, PeekOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in PeekInput) (*mcp.CallToolResult, PeekOutput, error) {
		actor, ok := auth.ActorFromContext(ctx)
		if !ok {
			return nil, PeekOutput{}, fmt.Errorf("no authenticated actor in context")
		}

		summary, err := storage.GetCatchUpSummary(db, actor.ID)
		if err != nil {
			return nil, PeekOutput{}, err
		}
		maxAt, err := storage.LatestActivityAt(db, actor.ID)
		if err != nil {
			return nil, PeekOutput{}, err
		}

		return nil, PeekOutput{
			ActorID:            actor.ID,
			UnreadReplyCount:   summary.UnreadReplyCount,
			UnseenMentionCount: summary.UnseenMentionCount,
			MaxCreatedAt:       maxAt,
		}, nil
	}
}
