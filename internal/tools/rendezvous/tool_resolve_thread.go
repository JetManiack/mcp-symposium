package rendezvous

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/storage"
)

type ResolveThreadInput struct {
	ThreadID string `json:"thread_id" jsonschema:"the thread to mark resolved"`
}

type ResolveThreadOutput struct {
	Status string `json:"status"`
}

func resolveThreadHandler(db *gorm.DB) mcp.ToolHandlerFor[ResolveThreadInput, ResolveThreadOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ResolveThreadInput) (*mcp.CallToolResult, ResolveThreadOutput, error) {
		thread, err := storage.ResolveThread(db, in.ThreadID)
		if err != nil {
			return nil, ResolveThreadOutput{}, err
		}
		return nil, ResolveThreadOutput{Status: thread.Status}, nil
	}
}

type ReopenThreadInput struct {
	ThreadID string `json:"thread_id" jsonschema:"the thread to reopen"`
}

type ReopenThreadOutput struct {
	Status string `json:"status"`
}

func reopenThreadHandler(db *gorm.DB) mcp.ToolHandlerFor[ReopenThreadInput, ReopenThreadOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ReopenThreadInput) (*mcp.CallToolResult, ReopenThreadOutput, error) {
		thread, err := storage.ReopenThread(db, in.ThreadID)
		if err != nil {
			return nil, ReopenThreadOutput{}, err
		}
		return nil, ReopenThreadOutput{Status: thread.Status}, nil
	}
}
