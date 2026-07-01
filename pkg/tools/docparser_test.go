package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDocParserToolName(t *testing.T) {
	tool := NewDocParserTool("/tmp")
	if tool.Name() != "doc_parser" {
		t.Fatalf("expected name 'doc_parser', got %s", tool.Name())
	}
}

func TestDocParserToolDescription(t *testing.T) {
	tool := NewDocParserTool("/tmp")
	if tool.Description() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestDocParserToolParameters(t *testing.T) {
	tool := NewDocParserTool("/tmp")
	params := tool.Parameters()
	if params == nil {
		t.Fatal("expected non-nil parameters")
	}
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties")
	}
	if _, ok := props["file_path"]; !ok {
		t.Fatal("expected file_path property")
	}
	if _, ok := props["format"]; !ok {
		t.Fatal("expected format property")
	}
}

func TestDocParserMissingFilePath(t *testing.T) {
	tool := NewDocParserTool("/tmp")
	ctx := context.Background()

	result := tool.Execute(ctx, map[string]interface{}{})
	if !result.IsError {
		t.Fatal("expected error for missing file_path")
	}
}

func TestDocParserEmptyFilePath(t *testing.T) {
	tool := NewDocParserTool("/tmp")
	ctx := context.Background()

	result := tool.Execute(ctx, map[string]interface{}{
		"file_path": "",
	})
	if !result.IsError {
		t.Fatal("expected error for empty file_path")
	}
}

func TestDocParserFileNotFound(t *testing.T) {
	tool := NewDocParserTool("/tmp")
	ctx := context.Background()

	result := tool.Execute(ctx, map[string]interface{}{
		"file_path": "/nonexistent/file.docx",
	})
	if !result.IsError {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestDocParserUnsupportedFormat(t *testing.T) {
	tool := NewDocParserTool("/tmp")
	ctx := context.Background()

	tmpFile := filepath.Join("/tmp", "test.txt")
	os.WriteFile(tmpFile, []byte("test"), 0644)
	defer os.Remove(tmpFile)

	result := tool.Execute(ctx, map[string]interface{}{
		"file_path": tmpFile,
		"format":    "unsupported",
	})
	if !result.IsError {
		t.Fatal("expected error for unsupported format")
	}
}

func TestDocParserAutoDetect(t *testing.T) {
	tool := NewDocParserTool("/tmp")
	ctx := context.Background()

	tmpFile := filepath.Join("/tmp", "test.xyz")
	os.WriteFile(tmpFile, []byte("test"), 0644)
	defer os.Remove(tmpFile)

	result := tool.Execute(ctx, map[string]interface{}{
		"file_path": tmpFile,
		"format":    "auto",
	})
	if !result.IsError {
		t.Log("Result:", result.ForLLM)
	}
}

func TestDocParserParseIpynb(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewDocParserTool(tmpDir)
	ctx := context.Background()

	nbContent := `{
		"cells": [
			{
				"cell_type": "markdown",
				"source": ["# Hello World\n", "This is a test notebook"]
			},
			{
				"cell_type": "code",
				"source": ["print('hello')"]
			}
		]
	}`

	tmpFile := filepath.Join(tmpDir, "test.ipynb")
	os.WriteFile(tmpFile, []byte(nbContent), 0644)

	result := tool.Execute(ctx, map[string]interface{}{
		"file_path": tmpFile,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}

	var parsed map[string]interface{}
	json.Unmarshal([]byte(result.ForLLM), &parsed)
	if parsed["format"] != "ipynb" {
		t.Fatalf("expected format 'ipynb', got %v", parsed["format"])
	}
}

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"file.docx", "docx"},
		{"file.xlsx", "xlsx"},
		{"file.xls", "xlsx"},
		{"file.ipynb", "ipynb"},
		{"file.txt", ""},
		{"file.unknown", ""},
	}

	for _, tt := range tests {
		result := detectFormat(tt.path)
		if result != tt.expected {
			t.Errorf("detectFormat(%s) = %s, want %s", tt.path, result, tt.expected)
		}
	}
}

func TestExtractSharedStrings(t *testing.T) {
	data := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="3" uniqueCount="3">
	<si><t>Hello</t></si>
	<si><t>World</t></si>
	<si><t>Test</t></si>
</sst>`)

	strings, err := extractSharedStrings(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(strings) != 3 {
		t.Fatalf("expected 3 strings, got %d", len(strings))
	}
	if strings[0] != "Hello" || strings[1] != "World" || strings[2] != "Test" {
		t.Fatalf("unexpected strings: %v", strings)
	}
}

func TestExtractDocxText(t *testing.T) {
	data := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
	<w:body>
		<w:p>
			<w:r>
				<w:t>Hello</w:t>
			</w:r>
			<w:r>
				<w:t> World</w:t>
			</w:r>
		</w:p>
		<w:p>
			<w:r>
				<w:t>Second paragraph</w:t>
			</w:r>
		</w:p>
	</w:body>
</w:document>`)

	text, err := extractDocxText(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "Hello World\nSecond paragraph" {
		t.Fatalf("unexpected text: %s", text)
	}
}
