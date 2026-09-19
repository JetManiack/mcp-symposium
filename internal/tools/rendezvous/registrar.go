package rendezvous

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/auth"
	"mcp-symposium/internal/storage"
)

const catchUpURIPrefix = "rendezvous://catchup/"

// catchUpResourceURI builds the per-actor catch-up resource URI used when
// firing ResourceUpdated notifications from the tool write paths.
func catchUpResourceURI(actorID string) string {
	return catchUpURIPrefix + actorID
}

// Registrar implements mcpserver.ToolRegistrar and registers all
// rendezvous MCP tools onto the server.
type Registrar struct{}

// NewRegistrar constructs a Registrar. The db argument is unused at
// construction time — tools receive db via Register — but is present to
// satisfy the common "NewXxx(db)" convention so callers compose naturally.
func NewRegistrar(_ *gorm.DB) *Registrar {
	return &Registrar{}
}

// Register adds every rendezvous tool to srv.
func (r *Registrar) Register(srv *mcp.Server, db *gorm.DB) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_thread",
		Description: "Create a new discussion thread; the caller becomes its author and first watcher",
	}, recorded(db, "create_thread", createThreadHandler(db, srv)))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "reply",
		Description: "Reply to an existing thread; the caller becomes a watcher of that thread",
	}, recorded(db, "reply", replyHandler(db, srv)))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "catch_up",
		Description: "Get unread replies and new mentions for the calling agent across every thread it watches",
	}, recorded(db, "catch_up", catchUpHandler(db)))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_thread",
		Description: "Fetch a thread's full details: title, body, status, tags, and every reply in order",
	}, recorded(db, "get_thread", getThreadHandler(db)))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_threads",
		Description: "List threads newest-first, optionally filtered by status and/or tags; paginated",
	}, recorded(db, "list_threads", listThreadsHandler(db)))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "resolve_thread",
		Description: "Mark a thread resolved",
	}, recorded(db, "resolve_thread", resolveThreadHandler(db)))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "reopen_thread",
		Description: "Reopen a previously resolved thread",
	}, recorded(db, "reopen_thread", reopenThreadHandler(db)))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "watch_thread",
		Description: "Subscribe the caller to a thread's future replies, visible via catch_up",
	}, recorded(db, "watch_thread", watchThreadHandler(db)))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "unwatch_thread",
		Description: "Unsubscribe the caller from a thread; its future replies stop appearing in catch_up",
	}, recorded(db, "unwatch_thread", unwatchThreadHandler(db)))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search",
		Description: "Full-text search across thread titles/bodies and reply bodies, ranked by relevance",
	}, recorded(db, "search", searchHandler(db)))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_profile",
		Description: "Set the caller's own onboarding profile: name, @mention nickname, bio, and specialization tags",
	}, recorded(db, "set_profile", setProfileHandler(db)))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_profiles",
		Description: "List every actor (agent or human) with their profile, if set, so you know who to @mention",
	}, recorded(db, "list_profiles", listProfilesHandler(db)))
}

// recorded wraps a typed tool handler with audit-record persistence. It
// writes a ToolCall row asynchronously so the tool's own latency is
// unaffected. If the actor cannot be read from context (should not happen
// in normal operation) the record is still written with an empty ActorID.
func recorded[In, Out any](db *gorm.DB, tool string,
	h mcp.ToolHandlerFor[In, Out]) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (
		*mcp.CallToolResult, Out, error) {
		start := time.Now()
		result, out, err := h(ctx, req, in)
		go func() {
			actorID := ""
			if actor, ok := auth.ActorFromContext(ctx); ok {
				actorID = actor.ID
			}

			inJSON, _ := json.Marshal(in)
			outJSON, _ := json.Marshal(out)
			outSize := len(outJSON)

			const maxPreview = 4096
			preview := string(outJSON)
			truncated := false
			if len(preview) > maxPreview {
				runes := []rune(preview)
				if len(runes) > maxPreview/4 {
					runes = runes[:maxPreview/4]
				}
				preview = string(runes)
				truncated = true
			}

			isError := err != nil || (result != nil && result.IsError)
			if dbErr := db.WithContext(context.Background()).Create(&storage.ToolCall{
				ID:         uuid.NewString(),
				ActorID:    actorID,
				Tool:       tool,
				InputJSON:  string(inJSON),
				OutputJSON: preview,
				OutputSize: outSize,
				Truncated:  truncated,
				IsError:    isError,
				DurationMS: time.Since(start).Milliseconds(),
				CalledAt:   start,
			}).Error; dbErr != nil {
				slog.Warn("failed to record tool call", "tool", tool, "error", fmt.Sprintf("%v", dbErr))
			}
		}()
		return result, out, err
	}
}
