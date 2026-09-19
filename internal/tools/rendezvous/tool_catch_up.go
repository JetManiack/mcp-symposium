package rendezvous

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
)

type CatchUpInput struct{}

type CatchUpOutput struct {
	ActorID       string            `json:"actor_id"`
	UnreadReplies []storage.Reply   `json:"unread_replies"`
	NewMentions   []storage.Mention `json:"new_mentions"`
}

func catchUpHandler(db *gorm.DB) mcp.ToolHandlerFor[CatchUpInput, CatchUpOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CatchUpInput) (*mcp.CallToolResult, CatchUpOutput, error) {
		actor, ok := auth.ActorFromContext(ctx)
		if !ok {
			return nil, CatchUpOutput{}, fmt.Errorf("no authenticated actor in context")
		}

		result, err := storage.CatchUp(db, actor.ID)
		if err != nil {
			return nil, CatchUpOutput{}, err
		}

		// Normalize nil slices to empty ones for consistent JSON output
		unreadReplies := result.UnreadReplies
		if unreadReplies == nil {
			unreadReplies = []storage.Reply{}
		}

		newMentions := result.NewMentions
		if newMentions == nil {
			newMentions = []storage.Mention{}
		}

		return nil, CatchUpOutput{
			ActorID:       actor.ID,
			UnreadReplies: unreadReplies,
			NewMentions:   newMentions,
		}, nil
	}
}
