package humanauth_test

import (
	"testing"

	"mcp-symposium/internal/humanauth"
)

// TestRandomOAuthState_ProducesDistinctNonEmptyValues is the one piece of
// the OIDC HTTP flow testable without a live Keycloak — everything else
// in oidc_handlers.go (NewOIDCHandlers's issuer discovery, the actual
// login/callback/logout round trip) requires real network access to an
// OIDC issuer and is verified manually against Keycloak instead (see this
// task's final step).
func TestRandomOAuthState_ProducesDistinctNonEmptyValues(t *testing.T) {
	a := humanauth.RandomOAuthStateForTesting()
	b := humanauth.RandomOAuthStateForTesting()

	if a == "" || b == "" {
		t.Fatal("RandomOAuthStateForTesting() returned an empty string")
	}
	if a == b {
		t.Error("RandomOAuthStateForTesting() returned the same value twice in a row, want distinct random values")
	}
}
