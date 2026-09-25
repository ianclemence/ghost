package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/tools"
)

func browserRegistry(t *testing.T) *tools.ToolRegistry {
	t.Helper()
	reg := tools.NewToolRegistry()
	reg.Register(tools.NewBrowserTool(t.TempDir(), "navigate"))
	reg.Register(tools.NewBrowserTool(t.TempDir(), "snapshot"))
	reg.Register(tools.NewBrowserTool(t.TempDir(), "click"))
	return reg
}

func systemMessages(content string) []providers.Message {
	return []providers.Message{{Role: "system", Content: content}}
}

// The contract lands in the system prompt BEFORE the cache boundary —
// exactly when browser tools are visible — and never twice.
func TestInjectBrowserContractPlacement(t *testing.T) {
	base := "# Ghost\n\nYou are Ghost.\n\n" + systemPromptCacheBoundary + "\n\nTime: now"
	msgs := systemMessages(base)

	injectBrowserContract(msgs, browserRegistry(t))
	if !strings.Contains(msgs[0].Content, "# Browser control (") {
		t.Fatal("contract not injected when browser tools are active")
	}
	if strings.Index(msgs[0].Content, "# Browser control (") > strings.Index(msgs[0].Content, systemPromptCacheBoundary) {
		t.Fatal("contract must ride the stable prefix (before the cache boundary)")
	}
	if !strings.Contains(msgs[0].Content, "[browser.timeout]") {
		t.Fatal("contract must carry the outcome-unknown rule")
	}

	injectBrowserContract(msgs, browserRegistry(t))
	if strings.Count(msgs[0].Content, "# Browser control (") != 1 {
		t.Fatal("injection must be idempotent")
	}
	// Volatile tail untouched.
	if !strings.Contains(msgs[0].Content, "Time: now") {
		t.Fatal("volatile tail lost")
	}
}

// No browser tools → prompt untouched (turns pay nothing for a surface
// they cannot call).
func TestInjectBrowserContractAbsentWithoutTools(t *testing.T) {
	base := "# Ghost\n\n" + systemPromptCacheBoundary + "\n\nTime: now"
	msgs := systemMessages(base)
	empty := tools.NewToolRegistry()
	empty.Register(tools.NewReadFileTool(t.TempDir(), false))

	injectBrowserContract(msgs, empty)
	if msgs[0].Content != base {
		t.Fatalf("prompt modified without browser tools: %s", msgs[0].Content)
	}
	injectBrowserContract(msgs, nil)
	if msgs[0].Content != base {
		t.Fatal("nil registry must be a no-op")
	}
}

// Missing boundary still injects (tail append), never panics.
func TestInjectBrowserContractNoBoundary(t *testing.T) {
	msgs := systemMessages("# Ghost\n\nplain")
	injectBrowserContract(msgs, browserRegistry(t))
	if !strings.Contains(msgs[0].Content, "# Browser control (") {
		t.Fatal("fallback append missing")
	}
}

// The skills index carries the cheap discovery stub; the full contract
// stays conditional (covered above).
func TestSkillStubInPrompt(t *testing.T) {
	cb := NewContextBuilder(t.TempDir())
	prompt := cb.BuildSystemPrompt(nil)
	if !strings.Contains(prompt, "Browser control (Ghost-native") {
		t.Fatal("SkillStub (discovery) missing from the system prompt")
	}
	if strings.Contains(prompt, "## The ref loop (mandatory)") {
		t.Fatal("SkillCore must NOT be unconditional — it rides tool visibility")
	}
}

func TestScreenshotImageMessage(t *testing.T) {
	dir := t.TempDir()

	// Valid small PNG (magic bytes only — the gate reads bytes, not pixels).
	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("fake-png-body")...)
	path := filepath.Join(dir, "shot-manual-1.png")
	if err := os.WriteFile(path, png, 0600); err != nil {
		t.Fatal(err)
	}
	msg, ok := screenshotImageMessage(path)
	if !ok {
		t.Fatal("small screenshot must attach")
	}
	if msg.Role != "user" {
		t.Fatalf("role = %s (turn-local observation, expected user role)", msg.Role)
	}
	if len(msg.MultiContent) != 2 {
		t.Fatalf("parts = %d, want text preamble + image", len(msg.MultiContent))
	}
	if msg.MultiContent[0].Type != "text" || !strings.Contains(msg.MultiContent[0].Text, "UNTRUSTED") {
		t.Fatalf("preamble must mark page content untrusted: %q", msg.MultiContent[0].Text)
	}
	if msg.MultiContent[1].Type != "image_url" || msg.MultiContent[1].ImageURL == nil {
		t.Fatal("image part missing")
	}
	if !strings.HasPrefix(msg.MultiContent[1].ImageURL.URL, "data:image/png;base64,") {
		t.Fatalf("image url = %q", msg.MultiContent[1].ImageURL.URL)
	}

	// Missing file.
	if _, ok := screenshotImageMessage(filepath.Join(dir, "absent.png")); ok {
		t.Fatal("missing file must not attach")
	}

	// Oversize: honest fallback, no payload blowup.
	big := filepath.Join(dir, "big.png")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxScreenshotAttach + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, ok := screenshotImageMessage(big); ok {
		t.Fatal("oversize screenshot must not attach")
	}
}
