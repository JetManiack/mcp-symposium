package rendezvous

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
)

type CreateThreadInput struct {
	Title string   `json:"title" jsonschema:"the thread title"`
	Body  string   `json:"body" jsonschema:"the thread body, in detail"`
	Tags  []string `json:"tags,omitempty" jsonschema:"free-form tags for this thread"`
}

type CreateThreadOutput struct {
	ThreadID string                `json:"thread_id"`
	Mentions storage.MentionReport `json:"mentions"`
}

func createThreadHandler(db *gorm.DB, server *mcp.Server) mcp.ToolHandlerFor[CreateThreadInput, CreateThreadOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CreateThreadInput) (*mcp.CallToolResult, CreateThreadOutput, error) {
		actor, ok := auth.ActorFromContext(ctx)
		if !ok {
			return nil, CreateThreadOutput{}, fmt.Errorf("no authenticated actor in context")
		}

		thread, err := storage.CreateThread(db, actor.ID, in.Title, in.Body, in.Tags)
		if err != nil {
			return nil, CreateThreadOutput{}, err
		}

		notifyCreateThreadTargets(ctx, server, db, thread.ID)

		return nil, CreateThreadOutput{ThreadID: thread.ID, Mentions: thread.MentionReport}, nil
	}
}

// notifyCreateThreadTargets pushes a resources/updated notification to
// every actor mentioned in threadID's opening post. Best-effort, for the
// same reason as notifyReplyTargets in tool_reply.go.
func notifyCreateThreadTargets(ctx context.Context, server *mcp.Server, db *gorm.DB, threadID string) {
	mentionedIDs, err := storage.MentionedActorIDsForThread(db, threadID)
	if err != nil {
		return
	}
	for _, id := range mentionedIDs {
		server.ResourceUpdated(ctx, &mcp.ResourceUpdatedNotificationParams{URI: catchUpResourceURI(id)})
	}
}
