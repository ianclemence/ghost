package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// verifyStub lets us control whether Verify passes or fails.
type verifyStub struct {
	name      string
	verifyErr error
	verified  int
}

func (s *verifyStub) Name() string { return s.name }
func (s *verifyStub) Description() string {
	return "stub"
}
func (s *verifyStub) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (s *verifyStub) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	return NewToolResult("ok")
}
func (s *verifyStub) Verify(ctx context.Context, args map[string]interface{}) error {
	s.verified++
	return s.verifyErr
}

func TestVerifyRunsAfterSuccess(t *testing.T) {
	reg := NewToolRegistry()
	stub := &verifyStub{name: "write"}
	reg.Register(stub)
	res := reg.ExecuteWithContext(context.Background(), "write", map[string]interface{}{}, "", "", "", nil)
	if res.IsError {
		t.Fatalf("expected success, got %s", res.ForLLM)
	}
	if stub.verified != 1 {
		t.Fatalf("expected verify to run once, got %d", stub.verified)
	}
}

func TestVerifyFailureMarksError(t *testing.T) {
	reg := NewToolRegistry()
	stub := &verifyStub{name: "write", verifyErr: os.ErrNotExist}
	reg.Register(stub)
	res := reg.ExecuteWithContext(context.Background(), "write", map[string]interface{}{}, "", "", "", nil)
	if !res.IsError {
		t.Fatalf("expected error when verification fails")
	}
	if !strings.Contains(res.ForLLM, "verification failed") {
		t.Fatalf("expected actionable verification message, got %q", res.ForLLM)
	}
}

func TestNoVerifyForPlainTool(t *testing.T) {
	// A tool that doesn't implement VerifiableTool must not be verified.
	reg := NewToolRegistry()
	reg.Register(&stubTool{name: "read", timeout: time.Second})
	res := reg.ExecuteWithContext(context.Background(), "read", map[string]interface{}{}, "", "", "", nil)
	if res.IsError {
		t.Fatalf("expected success, got %s", res.ForLLM)
	}
}

func TestWriteFileVerify(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteFileTool(dir, true) // restrict=true; write inside dir
	path := "note.md"
	content := "# hello\nworld\n"

	r := tool.Execute(context.Background(), map[string]interface{}{"path": path, "content": content})
	if r.IsError {
		t.Fatalf("write failed: %s", r.ForLLM)
	}
	if err := tool.Verify(context.Background(), map[string]interface{}{"path": path, "content": content}); err != nil {
		t.Fatalf("verify should pass: %v", err)
	}
	// Remove the file — verification must now fail (content not present).
	_ = os.Remove(filepath.Join(dir, path))
	if err := tool.Verify(context.Background(), map[string]interface{}{"path": path, "content": content}); err == nil {
		t.Fatal("verify should fail after the file is gone")
	}
}

func TestAppendFileVerify(t *testing.T) {
	dir := t.TempDir()
	tool := NewAppendFileTool(dir, true)

	r := tool.Execute(context.Background(), map[string]interface{}{"path": "list.md", "content": "item-a"})
	if r.IsError {
		t.Fatalf("append failed: %s", r.ForLLM)
	}
	// Appended content must be a suffix of the file.
	if err := tool.Verify(context.Background(), map[string]interface{}{"path": "list.md", "content": "item-a"}); err != nil {
		t.Fatalf("verify should pass: %v", err)
	}
	// Content that was never appended must fail.
	if err := tool.Verify(context.Background(), map[string]interface{}{"path": "list.md", "content": "does-not-exist"}); err == nil {
		t.Fatal("verify should fail for content that wasn't appended")
	}
}
