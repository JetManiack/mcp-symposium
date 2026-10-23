package rendezvous

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestExtractCodeBlocks(t *testing.T) {
	yamlContent := "key: value\nother: 123\n"
	sum := sha256.Sum256([]byte(yamlContent))
	wantSHA := fmt.Sprintf("%x", sum)

	body := "Some text.\n\n```yaml\n" + yamlContent + "```\n\nMore text."
	blocks := extractCodeBlocks(body)
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(blocks))
	}
	b := blocks[0]
	if b.Index != 0 {
		t.Errorf("index: want 0, got %d", b.Index)
	}
	if b.Lang != "yaml" {
		t.Errorf("lang: want yaml, got %q", b.Lang)
	}
	if b.Bytes != len(yamlContent) {
		t.Errorf("bytes: want %d, got %d", len(yamlContent), b.Bytes)
	}
	if b.SHA256 != wantSHA {
		t.Errorf("sha256: want %s, got %s", wantSHA, b.SHA256)
	}
}

func TestExtractCodeBlocksMultiple(t *testing.T) {
	body := "```go\nfunc main() {}\n```\n\nmiddle\n\n```json\n{}\n```"
	blocks := extractCodeBlocks(body)
	if len(blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Lang != "go" {
		t.Errorf("block 0 lang: want go, got %q", blocks[0].Lang)
	}
	if blocks[1].Lang != "json" {
		t.Errorf("block 1 lang: want json, got %q", blocks[1].Lang)
	}
	if blocks[0].Index != 0 || blocks[1].Index != 1 {
		t.Errorf("indices: want 0,1 got %d,%d", blocks[0].Index, blocks[1].Index)
	}
}

func TestExtractCodeBlocksNoBlocks(t *testing.T) {
	body := "Just plain text, no fences."
	blocks := extractCodeBlocks(body)
	if len(blocks) != 0 {
		t.Errorf("want 0 blocks, got %d", len(blocks))
	}
}

func TestExtractCodeBlocksEmptyBlock(t *testing.T) {
	body := "```\n```"
	blocks := extractCodeBlocks(body)
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(blocks))
	}
	// Empty block: just "\n"
	if blocks[0].Bytes != 1 {
		t.Errorf("bytes: want 1 (trailing newline), got %d", blocks[0].Bytes)
	}
}
