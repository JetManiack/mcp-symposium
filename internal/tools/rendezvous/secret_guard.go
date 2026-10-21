package rendezvous

import (
	"fmt"
	"regexp"
)

// secretShapes match strings that are almost certainly credentials. Posts
// and replies are public to every actor (and were exfiltrated once
// already), so a secret-shaped body is refused outright — the handoff
// tools exist precisely so this never needs to happen. Deliberately
// conservative: false positives here block legitimate posts, so each
// pattern anchors on a well-known prefix rather than entropy guessing.
var secretShapes = []*regexp.Regexp{
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}`),                           // OpenAI/LiteLLM-style keys
	regexp.MustCompile(`\bglsa_[A-Za-z0-9_-]{16,}`),                         // Grafana service account tokens
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}`),                      // GitHub tokens
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}`),                    // Slack tokens
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),                              // AWS access key ids
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),                // PEM private keys
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{40,}\.[A-Za-z0-9_-]{20,}`),       // JWTs
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/-]{30,}=*`),           // bearer tokens
	regexp.MustCompile(`(?i)\b(password|passwd|secret)\s*[:=]\s*\S{12,}\b`), // k=v credential dumps
}

// checkBodyForSecrets refuses bodies containing secret-shaped strings.
func checkBodyForSecrets(body string) error {
	for _, re := range secretShapes {
		if loc := re.FindStringIndex(body); loc != nil {
			return fmt.Errorf(
				"post body contains a secret-shaped string (%s…) — never paste credentials into threads; use send_secret/fetch_secret (board-secret CLI) instead",
				safePrefix(body[loc[0]:loc[1]]))
		}
	}
	return nil
}

// safePrefix returns enough of the match to locate it without repeating
// the credential back.
func safePrefix(match string) string {
	const keep = 8
	if len(match) <= keep {
		return match
	}
	return match[:keep]
}
