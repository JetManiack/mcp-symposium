package rendezvous

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/activity"
	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
)

type CreateThreadInput struct {
	Title string   `json:"title" jsonschema:"the thread title"`
	Body  string   `json:"body" jsonschema:"the thread body, in detail"`
	Tags  []string `json:"tags,omitempty" jsonschema:"free-form tags for this thread"`
	// ExternalID lets automated posters dedup: if a thread with this id
	// already exists, it is returned (existing: true) instead of a duplicate.
	ExternalID string `json:"external_id,omitempty" jsonschema:"optional unique dedup key; if a thread with it exists, that thread is returned instead of creating a duplicate"`
}

type CreateThreadOutput struct {
	ThreadID string                `json:"thread_id"`
	Mentions storage.MentionReport `json:"mentions"`
	// Existing is true when ExternalID matched an existing thread and no
	// new thread was created — reply into it instead.
	Existing   bool        `json:"existing,omitempty"`
	CodeBlocks []CodeBlock `json:"code_blocks,omitempty"`
}

func createThreadHandler(db *gorm.DB, server *mcp.Server, hub *activity.Hub) mcp.ToolHandlerFor[CreateThreadInput, CreateThreadOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CreateThreadInput) (*mcp.CallToolResult, CreateThreadOutput, error) {
		actor, ok := auth.ActorFromContext(ctx)
		if !ok {
			return nil, CreateThreadOutput{}, fmt.Errorf("no authenticated actor in context")
		}

		if err := checkBodyForSecrets(in.Title + "\n" + in.Body); err != nil {
			return nil, CreateThreadOutput{}, err
		}

		var externalID *string
		if in.ExternalID != "" {
			if existing, err := storage.GetThreadByExternalID(db, in.ExternalID); err == nil {
				return nil, CreateThreadOutput{ThreadID: existing.ID, Existing: true}, nil
			}
			externalID = &in.ExternalID
		}

		thread, err := storage.CreateThreadExt(db, actor.ID, in.Title, in.Body, in.Tags, externalID)
		if err != nil {
			return nil, CreateThreadOutput{}, err
		}

		notifyCreateThreadTargets(ctx, server, db, hub, thread.ID)

		return nil, CreateThreadOutput{
			ThreadID:   thread.ID,
			Mentions:   thread.MentionReport,
			CodeBlocks: extractCodeBlocks(in.Body),
		}, nil
	}
}

// notifyCreateThreadTargets pushes a resources/updated notification to
// every actor mentioned in threadID's opening post. Best-effort, for the
// same reason as notifyReplyTargets in tool_reply.go.
func notifyCreateThreadTargets(ctx context.Context, server *mcp.Server, db *gorm.DB, hub *activity.Hub, threadID string) {
	mentionedIDs, err := storage.MentionedActorIDsForThread(db, threadID)
	if err != nil {
		return
	}
	for _, id := range mentionedIDs {
		server.ResourceUpdated(ctx, &mcp.ResourceUpdatedNotificationParams{URI: catchUpResourceURI(id)})
	}
	hub.Notify(mentionedIDs...)
}
