package rendezvous

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
)

type ReplyInput struct {
	ThreadID string   `json:"thread_id" jsonschema:"the thread to reply to"`
	Body     string   `json:"body" jsonschema:"the reply body"`
	Watchers []string `json:"watchers,omitempty" jsonschema:"actor IDs to add as watchers of this thread"`
}

type ReplyOutput struct {
	ReplyID  string                `json:"reply_id"`
	Mentions storage.MentionReport `json:"mentions"`
}

func replyHandler(db *gorm.DB, server *mcp.Server) mcp.ToolHandlerFor[ReplyInput, ReplyOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ReplyInput) (*mcp.CallToolResult, ReplyOutput, error) {
		actor, ok := auth.ActorFromContext(ctx)
		if !ok {
			return nil, ReplyOutput{}, fmt.Errorf("no authenticated actor in context")
		}

		reply, err := storage.AddReply(db, in.ThreadID, actor.ID, in.Body, in.Watchers)
		if err != nil {
			return nil, ReplyOutput{}, err
		}

		notifyReplyTargets(ctx, server, db, in.ThreadID, reply.ID, actor.ID)

		return nil, ReplyOutput{ReplyID: reply.ID, Mentions: reply.MentionReport}, nil
	}
}

// notifyReplyTargets pushes a resources/updated notification to every
// watcher of threadID (except the replier itself) and every actor
// mentioned in replyID's body. Best-effort: a lookup or notification
// failure here must never fail the reply itself, since the reply already
// committed successfully.
func notifyReplyTargets(ctx context.Context, server *mcp.Server, db *gorm.DB, threadID, replyID, replierID string) {
	notify := map[string]bool{}

	if watcherIDs, err := storage.WatchersOf(db, threadID); err == nil {
		for _, id := range watcherIDs {
			if id != replierID {
				notify[id] = true
			}
		}
	}
	if mentionedIDs, err := storage.MentionedActorIDsForReply(db, replyID); err == nil {
		for _, id := range mentionedIDs {
			notify[id] = true
		}
	}

	for id := range notify {
		server.ResourceUpdated(ctx, &mcp.ResourceUpdatedNotificationParams{URI: catchUpResourceURI(id)})
	}
}
