package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/urfave/cli/v3"
	"gorm.io/gorm"

	"mcp-symposium/internal/frontend"
	"mcp-symposium/internal/health"
	"mcp-symposium/internal/humanauth"
	"mcp-symposium/internal/mcpserver"
	"mcp-symposium/internal/restapi"
	"mcp-symposium/internal/storage"
	"mcp-symposium/internal/tools/rendezvous"
)

func newRootCommand() *cli.Command {
	return &cli.Command{
		Name:  "rendezvous",
		Usage: "AI Rendezvous Point MCP server",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "listen-addr",
				Value:   ":8080",
				Sources: cli.EnvVars("LISTEN_ADDR"),
			},
			&cli.StringFlag{
				Name:    "db-dsn",
				Value:   "data/rendezvous.db",
				Sources: cli.EnvVars("DB_DSN"),
			},
			&cli.BoolFlag{
				Name:    "auth-stub",
				Usage:   "use a fixed, always-admin test identity instead of real Keycloak/OIDC auth — local development only, never set this in a real deployment",
				Sources: cli.EnvVars("AUTH_STUB"),
			},
			&cli.StringFlag{
				Name:    "oidc-issuer",
				Usage:   "Keycloak realm issuer URL, e.g. https://keycloak.internal/realms/rendezvous",
				Sources: cli.EnvVars("OIDC_ISSUER"),
			},
			&cli.StringFlag{
				Name:    "oidc-client-id",
				Usage:   "client ID of the dedicated OIDC client configured in Keycloak for this app",
				Sources: cli.EnvVars("OIDC_CLIENT_ID"),
			},
			&cli.StringFlag{
				Name:    "oidc-client-secret",
				Usage:   "client secret of the dedicated OIDC client configured in Keycloak for this app",
				Sources: cli.EnvVars("OIDC_CLIENT_SECRET"),
			},
			&cli.StringFlag{
				Name:    "public-url",
				Usage:   "this server's externally-reachable base URL (used to build the OIDC redirect URI <public-url>/auth/callback — must match what's configured on the Keycloak client)",
				Sources: cli.EnvVars("PUBLIC_URL"),
			},
			&cli.StringFlag{
				Name:    "admin-group",
				Value:   "admins",
				Usage:   "Keycloak group (from the ID token's groups claim) whose members get role admin; everyone else gets viewer",
				Sources: cli.EnvVars("ADMIN_GROUP"),
			},
			&cli.StringFlag{
				Name:    "session-encryption-key",
				Usage:   "base64-encoded 32-byte key used to encrypt session refresh tokens at rest (generate one with: openssl rand -base64 32)",
				Sources: cli.EnvVars("SESSION_ENCRYPTION_KEY"),
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			dsn := cmd.String("db-dsn")
			db, err := storage.Open(dsn)
			if err != nil {
				slog.Error("storage.Open failed at boot; serving /livez and /readyz (not ready) while retrying in the background instead of exiting", "error", err)
				return serveDegradedUntilReady(ctx, cmd, dsn, err)
			}
			return serveReady(ctx, cmd, db)
		},
	}
}

// buildAppHandler builds every route this app serves once db is
// available — MCP, the REST API, the static frontend, and (if OIDC is
// enabled) the login/callback/logout routes — plus the ReadyChecker that
// reflects db and OIDC actually being usable. It's shared between the
// immediate-ready startup path (serveReady) and the recovers-after-retry
// path (serveDegradedUntilReady), so both build identical routes.
func buildAppHandler(ctx context.Context, cmd *cli.Command, db *gorm.DB) (http.Handler, health.ReadyChecker, error) {
	var authProvider humanauth.Provider
	var oidcHandlers humanauth.OIDCHandlers
	useOIDC := !cmd.Bool("auth-stub")

	if useOIDC {
		if cmd.String("oidc-issuer") == "" || cmd.String("oidc-client-id") == "" || cmd.String("oidc-client-secret") == "" || cmd.String("public-url") == "" || cmd.String("session-encryption-key") == "" {
			return nil, health.ReadyChecker{}, fmt.Errorf("OIDC auth requires --oidc-issuer, --oidc-client-id, --oidc-client-secret, --public-url, and --session-encryption-key (or pass --auth-stub for local development)")
		}
		encryptionKey, err := base64.StdEncoding.DecodeString(cmd.String("session-encryption-key"))
		if err != nil {
			return nil, health.ReadyChecker{}, fmt.Errorf("--session-encryption-key must be valid base64: %w", err)
		}
		if len(encryptionKey) != 32 {
			return nil, health.ReadyChecker{}, fmt.Errorf("--session-encryption-key must decode to exactly 32 bytes, got %d (generate one with: openssl rand -base64 32)", len(encryptionKey))
		}
		cfg := humanauth.OIDCConfig{
			Issuer:        cmd.String("oidc-issuer"),
			ClientID:      cmd.String("oidc-client-id"),
			ClientSecret:  cmd.String("oidc-client-secret"),
			PublicURL:     cmd.String("public-url"),
			AdminGroup:    cmd.String("admin-group"),
			EncryptionKey: encryptionKey,
		}
		provider, handlers, err := humanauth.NewOIDCHandlers(ctx, db, cfg)
		if err != nil {
			return nil, health.ReadyChecker{}, fmt.Errorf("configure OIDC: %w", err)
		}
		authProvider = provider
		oidcHandlers = handlers
	} else {
		// ⚠️ StubProvider is a mandatory-to-replace interim measure: it
		// authenticates every request as a fixed always-admin identity
		// with no real credential check. Only reachable via --auth-stub,
		// which must never be set outside local development. See
		// docs/superpowers/specs/2026-07-24-ai-rendezvous-point-oidc-design.md.
		authProvider = humanauth.StubProvider{}
		slog.Warn("human auth running in STUB mode — do not deploy to production")
	}

	frontendFS, err := frontend.FS(false)
	if err != nil {
		return nil, health.ReadyChecker{}, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, health.ReadyChecker{}, fmt.Errorf("get pooled db handle: %w", err)
	}
	readyChecker := health.ReadyChecker{
		Ping:            sqlDB.PingContext,
		MigrationsReady: true,
		OIDCReady:       true,
	}

	tools := []mcpserver.ToolRegistrar{
		rendezvous.NewRegistrar(db),
	}

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpserver.Handler(db, tools))
	mux.Handle("/api/", http.StripPrefix("/api", restapi.NewHandler(db, authProvider)))
	if useOIDC {
		mux.HandleFunc("/auth/login", oidcHandlers.Login)
		mux.HandleFunc("/auth/callback", oidcHandlers.Callback)
		mux.HandleFunc("/auth/logout", oidcHandlers.Logout)
	}
	// Serve static files; fall back to index.html for client-side routes
	// so BrowserRouter navigation works without a server-side route for each path.
	fileServer := http.FileServer(frontendFS)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f, err := frontendFS.Open(r.URL.Path)
		if err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// Unknown path — serve the SPA shell and let the client router handle it.
		r2 := *r
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, &r2)
	})

	return mux, readyChecker, nil
}

// newServer builds the *http.Server this app always serves with,
// regardless of which startup path constructed handler.
func newServer(addr string, handler http.Handler) *http.Server {
	// WriteTimeout applies to every route here EXCEPT /mcp, which clears
	// it per-response in mcpserver.withoutWriteDeadline. It has to: the
	// MCP transport holds a standing GET open for the life of a session
	// and writes notifications onto it long after the request arrived,
	// while WriteTimeout is a cap from when the stream opened rather than
	// an idle timeout. Anything else long-lived added later needs the same
	// exemption — the failure mode is silent (see that function).
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// serveReady is the happy-path startup: db is already open, so every
// route builds immediately and the server starts serving fully-ready
// from the very first accepted connection — unchanged from this app's
// behavior before boot failures could be non-fatal.
func serveReady(ctx context.Context, cmd *cli.Command, db *gorm.DB) error {
	appHandler, readyChecker, err := buildAppHandler(ctx, cmd, db)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/livez", health.Livez)
	mux.HandleFunc("/readyz", readyChecker.Readyz)
	mux.Handle("/", appHandler)

	server := newServer(cmd.String("listen-addr"), mux)
	serveErr := make(chan error, 1)
	go func() {
		slog.Info("starting server", "addr", server.Addr)
		serveErr <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// serveDegradedUntilReady is reached only when storage.Open fails at
// boot. Kubernetes needs a live pod to probe — a crash loop only delays
// recovery and never fixes an outage the app itself can't fix — so this
// starts listening immediately with /livez healthy and /readyz
// not-ready, and retries storage.Open with capped exponential backoff in
// the background. Once it succeeds, the real routes are swapped in
// atomically on the same listener (no restart) and control folds into
// the same serve/shutdown loop serveReady uses.
func serveDegradedUntilReady(ctx context.Context, cmd *cli.Command, dsn string, firstErr error) error {
	var mu sync.RWMutex
	checker := health.ReadyChecker{
		Ping:            func(context.Context) error { return firstErr },
		MigrationsReady: false,
		OIDCReady:       false,
	}
	var appHandler atomic.Pointer[http.Handler]

	mux := http.NewServeMux()
	mux.HandleFunc("/livez", health.Livez)
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		c := checker
		mu.RUnlock()
		c.Readyz(w, r)
	})
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := appHandler.Load(); h != nil {
			(*h).ServeHTTP(w, r)
			return
		}
		http.Error(w, "starting up: database not yet available", http.StatusServiceUnavailable)
	}))

	server := newServer(cmd.String("listen-addr"), mux)
	serveErr := make(chan error, 1)
	go func() {
		slog.Info("starting server (degraded: database not yet available)", "addr", server.Addr)
		serveErr <- server.ListenAndServe()
	}()

	dbReady := make(chan *gorm.DB, 1)
	go retryOpenUntilReady(ctx, dsn, &mu, &checker, dbReady)

	for {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return server.Shutdown(shutdownCtx)
		case err := <-serveErr:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case db := <-dbReady:
			handler, readyChecker, err := buildAppHandler(ctx, cmd, db)
			if err != nil {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				server.Shutdown(shutdownCtx)
				return err
			}
			var h http.Handler = handler
			appHandler.Store(&h)
			mu.Lock()
			checker = readyChecker
			mu.Unlock()
			slog.Info("database became available; now serving normally")
		}
	}
}

// retryOpenUntilReady retries storage.Open(dsn) with capped exponential
// backoff until it succeeds or ctx is done, updating checker's Ping (via
// mu) with the latest failure after every attempt so /readyz always
// names the current reason rather than the boot-time one. Sends the
// opened *gorm.DB on ready and returns; it never retries again after a
// success, matching the "captured once at startup" contract the rest of
// the readiness design relies on.
func retryOpenUntilReady(ctx context.Context, dsn string, mu *sync.RWMutex, checker *health.ReadyChecker, ready chan<- *gorm.DB) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		db, err := storage.Open(dsn)
		if err == nil {
			ready <- db
			return
		}

		slog.Error("storage.Open retry failed", "error", err)
		mu.Lock()
		checker.Ping = func(context.Context) error { return err }
		mu.Unlock()

		backoff = min(backoff*2, maxBackoff)
	}
}

func main() {
	if err := newRootCommand().Run(context.Background(), os.Args); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}
