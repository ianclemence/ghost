package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestVisionToolName(t *testing.T) {
	tool := NewVisionTool("/tmp")
	if tool.Name() != "vision" {
		t.Fatalf("expected name 'vision', got %s", tool.Name())
	}
}

func TestVisionToolDescription(t *testing.T) {
	tool := NewVisionTool("/tmp")
	if tool.Description() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestVisionToolParameters(t *testing.T) {
	tool := NewVisionTool("/tmp")
	params := tool.Parameters()
	if params == nil {
		t.Fatal("expected non-nil parameters")
	}
}

func TestVisionToolMissingImageURL(t *testing.T) {
	tool := NewVisionTool("/tmp")
	result := tool.Execute(context.Background(), map[string]interface{}{
		"question": "What is this?",
	})
	if !result.IsError {
		t.Fatal("expected error for missing image_url")
	}
}

func TestVisionToolMissingQuestion(t *testing.T) {
	tool := NewVisionTool("/tmp")
	result := tool.Execute(context.Background(), map[string]interface{}{
		"image_url": "test.png",
	})
	if !result.IsError {
		t.Fatal("expected error for missing question")
	}
}

func TestVisionToolInvalidURL(t *testing.T) {
	tool := NewVisionTool("/tmp")
	result := tool.Execute(context.Background(), map[string]interface{}{
		"image_url": "ftp://example.com/image.png",
		"question":  "What is this?",
	})
	if !result.IsError {
		t.Fatal("expected error for invalid URL scheme")
	}
}

func TestVisionToolSSRFProtection(t *testing.T) {
	tool := NewVisionTool("/tmp")
	result := tool.Execute(context.Background(), map[string]interface{}{
		"image_url": "http://169.254.169.254/metadata/image.png",
		"question":  "What is this?",
	})
	if !result.IsError {
		t.Fatal("expected error for metadata endpoint")
	}
}

func TestVisionToolSecretDetection(t *testing.T) {
	tool := NewVisionTool("/tmp")
	result := tool.Execute(context.Background(), map[string]interface{}{
		"image_url": "https://example.com/image?key=sk-abc123",
		"question":  "What is this?",
	})
	if !result.IsError {
		t.Fatal("expected error for URL with secret")
	}
}

func TestVisionToolLocalFile(t *testing.T) {
	tool := NewVisionTool("/tmp")
	result := tool.Execute(context.Background(), map[string]interface{}{
		"image_url": "nonexistent.png",
		"question":  "What is this?",
	})
	if !result.IsError {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestVisionToolInvalidImageType(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("not an image"), 0644)

	tool := NewVisionTool("/tmp")
	result := tool.Execute(context.Background(), map[string]interface{}{
		"image_url": testFile,
		"question":  "What is this?",
	})
	if !result.IsError {
		t.Fatal("expected error for invalid image type")
	}
}

func TestVisionToolValidPNG(t *testing.T) {
	tmpDir := t.TempDir()
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	testFile := filepath.Join(tmpDir, "test.png")
	os.WriteFile(testFile, pngHeader, 0644)

	tool := NewVisionTool("/tmp")
	result := tool.Execute(context.Background(), map[string]interface{}{
		"image_url": testFile,
		"question":  "What is this?",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
}

func TestVisionToolValidJPEG(t *testing.T) {
	tmpDir := t.TempDir()
	jpegHeader := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	testFile := filepath.Join(tmpDir, "test.jpg")
	os.WriteFile(testFile, jpegHeader, 0644)

	tool := NewVisionTool("/tmp")
	result := tool.Execute(context.Background(), map[string]interface{}{
		"image_url": testFile,
		"question":  "What is this?",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
}

func TestVisionToolDetectMimeType(t *testing.T) {
	tool := NewVisionTool("/tmp")

	tests := []struct {
		data     []byte
		expected string
	}{
		{[]byte{0x89, 0x50, 0x4E, 0x47}, "image/png"},
		{[]byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg"},
		{[]byte{0x47, 0x49, 0x46, 0x38}, "image/gif"},
		{[]byte{0x42, 0x4D}, "image/bmp"},
		{[]byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'E', 'B', 'P'}, "image/webp"},
	}

	for _, tt := range tests {
		result := tool.detectMimeType(tt.data)
		if result != tt.expected {
			t.Errorf("detectMimeType(%v) = %s, want %s", tt.data, result, tt.expected)
		}
	}
}

func TestVisionToolIsValidImageType(t *testing.T) {
	tool := NewVisionTool("/tmp")

	tests := []struct {
		data  []byte
		valid bool
	}{
		{[]byte{0x89, 0x50, 0x4E, 0x47}, true},
		{[]byte{0xFF, 0xD8, 0xFF, 0xE0}, true},
		{[]byte{0x47, 0x49, 0x46, 0x38}, true},
		{[]byte{0x42, 0x4D}, true},
		{[]byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'E', 'B', 'P'}, true},
		{[]byte{0x00, 0x00, 0x00, 0x00}, false},
	}

	for _, tt := range tests {
		result := tool.isValidImageType(tt.data)
		if result != tt.valid {
			t.Errorf("isValidImageType(%v) = %v, want %v", tt.data, result, tt.valid)
		}
	}
}

func TestDecodeDataURL(t *testing.T) {
	// 1x1 transparent PNG
	const pngB64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8z8BQDwAFgQIAHcT8dQAAAABJRU5ErkJggg=="
	data, mime, err := decodeDataURL("data:image/png;base64," + pngB64)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if mime != "image/png" {
		t.Errorf("mime = %q, want image/png", mime)
	}
	if len(data) == 0 {
		t.Error("expected decoded bytes")
	}
	if _, _, err := decodeDataURL("not-a-data-url"); err == nil {
		t.Error("expected error for malformed data url")
	}
}
