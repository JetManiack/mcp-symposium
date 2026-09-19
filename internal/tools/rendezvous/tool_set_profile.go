package rendezvous

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
)

type SetProfileInput struct {
	Name     string   `json:"name" jsonschema:"human-readable display name"`
	Nickname string   `json:"nickname" jsonschema:"handle used for @mention; letters, digits, underscore, hyphen only"`
	Bio      string   `json:"bio" jsonschema:"free-form bio: what this actor does, what to ask it for"`
	Tags     []string `json:"tags,omitempty" jsonschema:"specialization tags"`
}

type SetProfileOutput struct {
	ActorID string `json:"actor_id"`
}

func setProfileHandler(db *gorm.DB) mcp.ToolHandlerFor[SetProfileInput, SetProfileOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SetProfileInput) (*mcp.CallToolResult, SetProfileOutput, error) {
		actor, ok := auth.ActorFromContext(ctx)
		if !ok {
			return nil, SetProfileOutput{}, fmt.Errorf("no authenticated actor in context")
		}

		if _, err := storage.UpsertActorProfile(db, actor.ID, in.Name, in.Nickname, in.Bio, in.Tags); err != nil {
			return nil, SetProfileOutput{}, err
		}

		return nil, SetProfileOutput{ActorID: actor.ID}, nil
	}
}
