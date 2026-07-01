package tools

import (
	"context"
	"testing"
)

func TestImageGenToolName(t *testing.T) {
	tool := NewImageGenTool("/tmp", "", "")
	if tool.Name() != "image_generate" {
		t.Fatalf("expected name 'image_generate', got %s", tool.Name())
	}
}

func TestImageGenToolDescription(t *testing.T) {
	tool := NewImageGenTool("/tmp", "", "")
	if tool.Description() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestImageGenToolParameters(t *testing.T) {
	tool := NewImageGenTool("/tmp", "", "")
	params := tool.Parameters()
	if params == nil {
		t.Fatal("expected non-nil parameters")
	}
}

func TestImageGenToolMissingPrompt(t *testing.T) {
	tool := NewImageGenTool("/tmp", "key", "")
	result := tool.Execute(context.Background(), map[string]interface{}{})
	if !result.IsError {
		t.Fatal("expected error for missing prompt")
	}
}

func TestImageGenToolMissingAPIKey(t *testing.T) {
	tool := NewImageGenTool("/tmp", "", "")
	result := tool.Execute(context.Background(), map[string]interface{}{
		"prompt": "A cat",
	})
	if !result.IsError {
		t.Fatal("expected error for missing API key")
	}
}

func TestImageGenToolInvalidAPIBase(t *testing.T) {
	tool := NewImageGenTool("/tmp", "test-key", "http://invalid.example.com")
	result := tool.Execute(context.Background(), map[string]interface{}{
		"prompt": "A cat",
	})
	if !result.IsError {
		t.Fatal("expected error for invalid API base")
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello World", "Hello_World"},
		{"test@#$%", "test"},
		{"", "image"},
		{"a", "a"},
		{"hello world 123", "hello_world_123"},
	}

	for _, tt := range tests {
		result := sanitizeFilename(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestImageGenToolDefaultSize(t *testing.T) {
	tool := NewImageGenTool("/tmp", "test-key", "http://invalid.example.com")
	if tool.maxFileSize != 10*1024*1024 {
		t.Fatalf("expected maxFileSize 10MB, got %d", tool.maxFileSize)
	}
}
