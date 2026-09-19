package mcpserver

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"mcp-symposium/internal/auth"
)

// ToolRegistrar is implemented by any package that wants to register a
// cohesive set of MCP tools onto a server.
type ToolRegistrar interface {
	Register(srv *mcp.Server, db *gorm.DB)
}

// Handler builds the full /mcp HTTP handler: Streamable HTTP transport,
// tools registered via each ToolRegistrar in tools, wrapped in bearer-token
// authentication.
func Handler(db *gorm.DB, tools []ToolRegistrar) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "ai-rendezvous-point", Version: "0.1.0"}, &mcp.ServerOptions{
		SubscribeHandler:   subscribeHandler,
		UnsubscribeHandler: unsubscribeHandler,
	})

	for _, t := range tools {
		t.Register(server, db)
	}
	RegisterResources(server, db)

	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)

	return clearWriteDeadline(auth.RequireBearer(db, mcpHandler))
}

// clearWriteDeadline clears the *http.Server's write deadline for this
// response only, leaving it in force on every other route.
//
// The MCP Streamable HTTP transport keeps a standing GET open for as long
// as the session lives and writes server-initiated notifications onto it
// whenever they happen — minutes or hours after the request arrived. A
// server-wide http.Server.WriteTimeout is not an idle timeout: Go resets
// that deadline only when a new request's headers are read, so on a
// standing stream it is a hard cap measured from when the stream opened.
// Past it, the push is never delivered and net/http tears the connection
// down.
//
// What made this cost two rounds of live investigation (board thread
// 4f93edd4) is that the failure is invisible from inside: the write lands
// in the response's buffered writer and Flush() returns no error, so
// neither this app nor the SDK ever sees it. The server logs success and
// stays silent; the client sees a stream that was open and simply never
// delivered anything — indistinguishable from a buffering proxy.
func clearWriteDeadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Best-effort: a failure here only means long-lived streams are
		// capped again, which must not take working request/response tool
		// calls down with it. Logged because it is otherwise undetectable.
		if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil {
			slog.WarnContext(r.Context(), "could not clear write deadline for /mcp; server-initiated notifications will stop being delivered once the server's WriteTimeout elapses", "error", err)
		}
		next.ServeHTTP(w, r)
	})
}
