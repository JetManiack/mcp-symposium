package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcp-symposium/internal/health"
)

func TestLivez_ReturnsOK(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/livez", nil)

	health.Livez(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestReadyz_AllChecksPass_ReturnsOK(t *testing.T) {
	checker := health.ReadyChecker{
		Ping:            func(ctx context.Context) error { return nil },
		MigrationsReady: true,
		OIDCReady:       true,
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	checker.Readyz(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestReadyz_DBPingFails_ReturnsServiceUnavailableNamingDatabase(t *testing.T) {
	checker := health.ReadyChecker{
		Ping:            func(ctx context.Context) error { return errors.New("connection refused") },
		MigrationsReady: true,
		OIDCReady:       true,
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	checker.Readyz(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	var body struct {
		Status  string   `json:"status"`
		Failing []string `json:"failing"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !contains(body.Failing, "database") {
		t.Errorf("failing = %v, want it to name %q", body.Failing, "database")
	}
}

func TestReadyz_MigrationsNotReady_ReturnsServiceUnavailableNamingMigrations(t *testing.T) {
	checker := health.ReadyChecker{
		Ping:            func(ctx context.Context) error { return nil },
		MigrationsReady: false,
		OIDCReady:       true,
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	checker.Readyz(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	var body struct {
		Failing []string `json:"failing"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !contains(body.Failing, "migrations") {
		t.Errorf("failing = %v, want it to name %q", body.Failing, "migrations")
	}
}

func TestReadyz_OIDCNotReady_ReturnsServiceUnavailableNamingOIDCDiscovery(t *testing.T) {
	checker := health.ReadyChecker{
		Ping:            func(ctx context.Context) error { return nil },
		MigrationsReady: true,
		OIDCReady:       false,
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	checker.Readyz(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	var body struct {
		Failing []string `json:"failing"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !contains(body.Failing, "oidc_discovery") {
		t.Errorf("failing = %v, want it to name %q", body.Failing, "oidc_discovery")
	}
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
