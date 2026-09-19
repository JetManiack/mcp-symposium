package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// findFreeListener opens a listener on an OS-assigned free port, letting
// the test discover an address without a fixed port that could collide
// with a port already in use on the test machine.
func findFreeListener() (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:0")
}

// waitForServer polls addr until it accepts TCP connections or t fails.
func waitForServer(t interface{ Fatalf(string, ...any) }, addr string) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s did not start in time", addr)
}

// TestServeCommand_StartsHTTPServerOnConfiguredAddr starts the real serve
// command against an ephemeral port and confirms the /mcp route answers
// (401 without a token — the point is that the route exists and the auth
// middleware from Task 6 is wired in, not that this call succeeds).
func TestServeCommand_StartsHTTPServerOnConfiguredAddr(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	listener, err := findFreeListener()
	if err != nil {
		t.Fatalf("findFreeListener() error = %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	cmd := newRootCommand()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Run(ctx, []string{"rendezvous", "--listen-addr", addr, "--db-dsn", dbPath, "--auth-stub"})
	}()

	waitForServer(t, addr)

	resp, err := http.Post("http://"+addr+"/mcp", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /mcp error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (missing bearer token)", resp.StatusCode, http.StatusUnauthorized)
	}

	cancel()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down after context cancellation")
	}
}

// TestServeCommand_ExposesLivezAndReadyzUnauthenticated confirms both
// health routes answer 2xx with no auth header, since kubelet probes send
// none.
func TestServeCommand_ExposesLivezAndReadyzUnauthenticated(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	listener, err := findFreeListener()
	if err != nil {
		t.Fatalf("findFreeListener() error = %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	cmd := newRootCommand()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Run(ctx, []string{"rendezvous", "--listen-addr", addr, "--db-dsn", dbPath, "--auth-stub"})
	}()

	waitForServer(t, addr)

	for _, path := range []string{"/livez", "/readyz"} {
		resp, err := http.Get("http://" + addr + path)
		if err != nil {
			t.Fatalf("GET %s error = %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d, want %d", path, resp.StatusCode, http.StatusOK)
		}
	}

	cancel()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down after context cancellation")
	}
}

func TestServeCommand_MountsRestAPI(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	listener, err := findFreeListener()
	if err != nil {
		t.Fatalf("findFreeListener() error = %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	cmd := newRootCommand()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Run(ctx, []string{"rendezvous", "--listen-addr", addr, "--db-dsn", dbPath, "--auth-stub"})
	}()

	waitForServer(t, addr)

	resp, err := http.Get("http://" + addr + "/api/me")
	if err != nil {
		t.Fatalf("GET /api/me error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d (stub auth should authenticate every request)", resp.StatusCode, http.StatusOK)
	}

	cancel()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down after context cancellation")
	}
}

func TestServeCommand_ServesFrontend(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	listener, err := findFreeListener()
	if err != nil {
		t.Fatalf("findFreeListener() error = %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	cmd := newRootCommand()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Run(ctx, []string{"rendezvous", "--listen-addr", addr, "--db-dsn", dbPath, "--auth-stub"})
	}()

	waitForServer(t, addr)

	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("GET / error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET / status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read / body error = %v", err)
	}
	if !strings.Contains(string(body), `id="root"`) {
		t.Error("/ response does not contain the React mount point (id=\"root\")")
	}

	bundleResp, err := http.Get("http://" + addr + "/js/app.bundle.js")
	if err != nil {
		t.Fatalf("GET /js/app.bundle.js error = %v", err)
	}
	defer bundleResp.Body.Close()
	if bundleResp.StatusCode != http.StatusOK {
		t.Errorf("GET /js/app.bundle.js status = %d, want %d", bundleResp.StatusCode, http.StatusOK)
	}
	bundleBody, err := io.ReadAll(bundleResp.Body)
	if err != nil {
		t.Fatalf("read /js/app.bundle.js body error = %v", err)
	}
	if !strings.Contains(string(bundleBody), "/api/actors") {
		t.Error("/js/app.bundle.js does not reference /api/actors — the Agents screen may not actually be bundled")
	}

	cancel()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down after context cancellation")
	}
}

// TestServeCommand_DBUnreachableAtBoot_ServesNotReadyInsteadOfExiting
// confirms that an unreachable database at boot no longer crashes the
// process (the CrashLoopBackOff behavior board thread 1086abc1 asked to
// soften) — the server keeps listening, /livez stays healthy, /readyz
// reports not-ready, and routes that need the database answer 503
// rather than panicking or hanging.
func TestServeCommand_DBUnreachableAtBoot_ServesNotReadyInsteadOfExiting(t *testing.T) {
	listener, err := findFreeListener()
	if err != nil {
		t.Fatalf("findFreeListener() error = %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	cmd := newRootCommand()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Port 1 requires root to bind and nothing listens there in a test
	// environment, so this connection is refused immediately rather than
	// timing out slowly.
	badDSN := "postgres://nouser:nopass@127.0.0.1:1/nonexistent"

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Run(ctx, []string{"rendezvous", "--listen-addr", addr, "--db-dsn", badDSN, "--auth-stub"})
	}()

	waitForServer(t, addr)

	select {
	case err := <-errCh:
		t.Fatalf("process exited (err=%v) instead of serving degraded", err)
	case <-time.After(200 * time.Millisecond):
		// Expected: still running.
	}

	livezResp, err := http.Get("http://" + addr + "/livez")
	if err != nil {
		t.Fatalf("GET /livez error = %v", err)
	}
	livezResp.Body.Close()
	if livezResp.StatusCode != http.StatusOK {
		t.Errorf("GET /livez status = %d, want %d", livezResp.StatusCode, http.StatusOK)
	}

	readyResp, err := http.Get("http://" + addr + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz error = %v", err)
	}
	readyResp.Body.Close()
	if readyResp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("GET /readyz status = %d, want %d", readyResp.StatusCode, http.StatusServiceUnavailable)
	}

	apiResp, err := http.Get("http://" + addr + "/api/me")
	if err != nil {
		t.Fatalf("GET /api/me error = %v", err)
	}
	apiResp.Body.Close()
	if apiResp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("GET /api/me status = %d, want %d (database not yet available)", apiResp.StatusCode, http.StatusServiceUnavailable)
	}

	cancel()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down after context cancellation")
	}
}

// TestServeCommand_DBBecomesReachable_TransitionsFromNotReadyToReady
// confirms the recovery half of the same behavior: once the database
// that was unreachable at boot becomes reachable, the server picks it
// up on its own — no restart — and starts serving normally.
func TestServeCommand_DBBecomesReachable_TransitionsFromNotReadyToReady(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	// A regular file where storage.Open needs to create a directory
	// (openSQLite MkdirAlls dsn's parent) makes Open fail deterministically
	// until the test removes it below — a controllable stand-in for "the
	// database is briefly unreachable".
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}
	dbPath := filepath.Join(blocker, "test.db")

	listener, err := findFreeListener()
	if err != nil {
		t.Fatalf("findFreeListener() error = %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	cmd := newRootCommand()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Run(ctx, []string{"rendezvous", "--listen-addr", addr, "--db-dsn", dbPath, "--auth-stub"})
	}()

	waitForServer(t, addr)

	readyResp, err := http.Get("http://" + addr + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz error = %v", err)
	}
	readyResp.Body.Close()
	if readyResp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("GET /readyz status = %d, want %d before unblocking", readyResp.StatusCode, http.StatusServiceUnavailable)
	}

	if err := os.Remove(blocker); err != nil {
		t.Fatalf("remove blocker: %v", err)
	}
	if err := os.Mkdir(blocker, 0o750); err != nil {
		t.Fatalf("create real directory: %v", err)
	}

	deadline := time.Now().Add(6 * time.Second)
	var lastStatus int
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/readyz")
		if err == nil {
			lastStatus = resp.StatusCode
			resp.Body.Close()
			if lastStatus == http.StatusOK {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if lastStatus != http.StatusOK {
		t.Fatalf("never became ready after unblocking; last /readyz status = %d", lastStatus)
	}

	apiResp, err := http.Get("http://" + addr + "/api/me")
	if err != nil {
		t.Fatalf("GET /api/me error = %v", err)
	}
	apiResp.Body.Close()
	if apiResp.StatusCode != http.StatusOK {
		t.Errorf("GET /api/me status = %d, want %d now that the database is available", apiResp.StatusCode, http.StatusOK)
	}

	cancel()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down after context cancellation")
	}
}

// TestServeCommand_FailsFastWithoutOIDCConfigOrStubFlag confirms OIDC is
// the default: running with neither --auth-stub nor OIDC config must
// fail immediately with a clear error, never silently start
// unauthenticated or fall back to stub.
func TestServeCommand_FailsFastWithoutOIDCConfigOrStubFlag(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	listener, err := findFreeListener()
	if err != nil {
		t.Fatalf("findFreeListener() error = %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	cmd := newRootCommand()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = cmd.Run(ctx, []string{"rendezvous", "--listen-addr", addr, "--db-dsn", dbPath})
	if err == nil {
		t.Fatal("Run() error = nil, want an error when OIDC config is incomplete and --auth-stub isn't set")
	}
	if !strings.Contains(err.Error(), "oidc") && !strings.Contains(err.Error(), "OIDC") {
		t.Errorf("Run() error = %q, want it to mention the missing OIDC configuration", err.Error())
	}
}
