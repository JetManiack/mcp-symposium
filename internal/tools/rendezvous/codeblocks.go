package rendezvous

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// CodeBlock describes one fenced code block found in a Markdown body.
// Bytes and SHA256 are over the block content as defined below.
//
// Content rule: the bytes between the line after the opening ```<lang> fence
// and the closing ``` fence, including the final '\n' before the closing
// fence. This means sha256(block.Content) == sha256sum of the file when the
// file ends with a single newline — the convention used for byte-for-byte
// config sync between agents.
type CodeBlock struct {
	Index  int    `json:"index"`
	Lang   string `json:"lang,omitempty"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// extractCodeBlocks parses fenced code blocks from a Markdown body and
// returns a descriptor for each one. Nested fences are not supported —
// the first closing ``` that appears on its own line ends the block.
func extractCodeBlocks(body string) []CodeBlock {
	var blocks []CodeBlock
	lines := strings.Split(body, "\n")
	idx := 0
	i := 0
	for i < len(lines) {
		line := lines[i]
		if !strings.HasPrefix(line, "```") {
			i++
			continue
		}
		lang := strings.TrimSpace(strings.TrimPrefix(line, "```"))
		i++
		// Collect content lines until closing fence or EOF.
		var content []string
		for i < len(lines) {
			if strings.TrimSpace(lines[i]) == "```" {
				i++ // consume closing fence
				break
			}
			content = append(content, lines[i])
			i++
		}
		// Reconstruct bytes: join with \n and add trailing \n per the rule.
		raw := strings.Join(content, "\n") + "\n"
		sum := sha256.Sum256([]byte(raw))
		blocks = append(blocks, CodeBlock{
			Index:  idx,
			Lang:   lang,
			Bytes:  len(raw),
			SHA256: fmt.Sprintf("%x", sum),
		})
		idx++
	}
	return blocks
}
