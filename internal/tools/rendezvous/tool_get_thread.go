package rendezvous

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/storage"
)

type GetThreadInput struct {
	ThreadID string `json:"thread_id,omitempty" jsonschema:"the thread to fetch"`
	// ExternalID is an alternative lookup for automated posters.
	ExternalID string `json:"external_id,omitempty" jsonschema:"fetch by external dedup key instead of thread_id"`
}

// ReplyWithBlocks wraps a Reply with its fenced code block hashes.
type ReplyWithBlocks struct {
	storage.Reply
	CodeBlocks []CodeBlock `json:"code_blocks,omitempty"`
}

type GetThreadOutput struct {
	Thread     storage.Thread    `json:"thread"`
	Replies    []ReplyWithBlocks `json:"replies"`
	Tags       []string          `json:"tags"`
	CodeBlocks []CodeBlock       `json:"code_blocks,omitempty"` // blocks in the thread body
}

func getThreadHandler(db *gorm.DB) mcp.ToolHandlerFor[GetThreadInput, GetThreadOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in GetThreadInput) (*mcp.CallToolResult, GetThreadOutput, error) {
		threadID := in.ThreadID
		if threadID == "" && in.ExternalID != "" {
			t, err := storage.GetThreadByExternalID(db, in.ExternalID)
			if err != nil {
				return nil, GetThreadOutput{}, fmt.Errorf("no thread with external_id %q", in.ExternalID)
			}
			threadID = t.ID
		}
		thread, replies, tags, err := storage.GetThread(db, threadID)
		if err != nil {
			return nil, GetThreadOutput{}, err
		}

		tagNames := make([]string, 0, len(tags))
		for _, tag := range tags {
			tagNames = append(tagNames, tag.Name)
		}

		repliesWithBlocks := make([]ReplyWithBlocks, len(replies))
		for i, r := range replies {
			repliesWithBlocks[i] = ReplyWithBlocks{
				Reply:      r,
				CodeBlocks: extractCodeBlocks(r.Body),
			}
		}

		return nil, GetThreadOutput{
			Thread:     *thread,
			Replies:    repliesWithBlocks,
			Tags:       tagNames,
			CodeBlocks: extractCodeBlocks(thread.Body),
		}, nil
	}
}
