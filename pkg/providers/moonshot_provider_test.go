package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestMoonshotProvider_Chat_ThinkingParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req kimiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
			return
		}

		// Verify parameters when thinking is enabled
		if req.Thinking != nil && req.Thinking.Type == "enabled" {
			if req.Temperature != 1.0 {
				t.Errorf("Expected temperature 1.0 when thinking is enabled, got %v", req.Temperature)
			}
			if req.TopP != 0.95 {
				t.Errorf("Expected top_p 0.95 when thinking is enabled, got %v", req.TopP)
			}
			if req.N != 1 {
				t.Errorf("Expected n 1 when thinking is enabled, got %v", req.N)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(kimiResponse{
			ID: "test-id",
			Choices: []struct {
				Index        int         `json:"index"`
				Message      kimiMessage `json:"message"`
				FinishReason string      `json:"finish_reason"`
			}{
				{
					Index: 0,
					Message: kimiMessage{
						Role:    "assistant",
						Content: "Thinking mode test response",
					},
					FinishReason: "stop",
				},
			},
		})
	}))
	defer server.Close()

	p := NewMoonshotProvider("test-key", server.URL)
	options := map[string]interface{}{
		"thinking": true,
	}

	_, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "Hello"}}, nil, "kimi-k2.5", options)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
}

func TestMoonshotProvider_UploadFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/files" {
			t.Errorf("Expected POST /files, got %s %s", r.Method, r.URL.Path)
			return
		}

		err := r.ParseMultipartForm(10 << 20)
		if err != nil {
			t.Errorf("Failed to parse multipart form: %v", err)
			return
		}

		if r.FormValue("purpose") != "vision" {
			t.Errorf("Expected purpose vision, got %s", r.FormValue("purpose"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id": "file-123",
		})
	}))
	defer server.Close()

	p := NewMoonshotProvider("test-key", server.URL)
	
	// Create a dummy file
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.png")
	err := os.WriteFile(filePath, []byte("fake image data"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	fileID, err := p.UploadFile(context.Background(), filePath, "vision")
	if err != nil {
		t.Fatalf("UploadFile failed: %v", err)
	}

	if fileID != "file-123" {
		t.Errorf("Expected file ID file-123, got %s", fileID)
	}
}
