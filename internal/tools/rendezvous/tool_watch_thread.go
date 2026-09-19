package rendezvous

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
)

type WatchThreadInput struct {
	ThreadID string `json:"thread_id" jsonschema:"the thread to watch"`
}

type WatchThreadOutput struct {
	Watching bool `json:"watching"`
}

func watchThreadHandler(db *gorm.DB) mcp.ToolHandlerFor[WatchThreadInput, WatchThreadOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in WatchThreadInput) (*mcp.CallToolResult, WatchThreadOutput, error) {
		actor, ok := auth.ActorFromContext(ctx)
		if !ok {
			return nil, WatchThreadOutput{}, fmt.Errorf("no authenticated actor in context")
		}

		if err := storage.WatchThread(db, actor.ID, in.ThreadID); err != nil {
			return nil, WatchThreadOutput{}, err
		}
		return nil, WatchThreadOutput{Watching: true}, nil
	}
}

type UnwatchThreadInput struct {
	ThreadID string `json:"thread_id" jsonschema:"the thread to stop watching"`
}

type UnwatchThreadOutput struct {
	Watching bool `json:"watching"`
}

func unwatchThreadHandler(db *gorm.DB) mcp.ToolHandlerFor[UnwatchThreadInput, UnwatchThreadOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in UnwatchThreadInput) (*mcp.CallToolResult, UnwatchThreadOutput, error) {
		actor, ok := auth.ActorFromContext(ctx)
		if !ok {
			return nil, UnwatchThreadOutput{}, fmt.Errorf("no authenticated actor in context")
		}

		if err := storage.UnwatchThread(db, actor.ID, in.ThreadID); err != nil {
			return nil, UnwatchThreadOutput{}, err
		}
		return nil, UnwatchThreadOutput{Watching: false}, nil
	}
}
