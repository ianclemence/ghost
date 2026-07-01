package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type VisionTool struct {
	workspace   string
	urlSafety   *URLSafety
	maxFileSize int64
}

func NewVisionTool(workspace string) *VisionTool {
	return &VisionTool{
		workspace:   workspace,
		urlSafety:   NewURLSafety(URLSafetyConfig{AllowPrivateURLs: false}),
		maxFileSize: 10 * 1024 * 1024, // 10MB
	}
}

func (t *VisionTool) Name() string {
	return "vision"
}

func (t *VisionTool) Description() string {
	return "Analyze an image from a URL or local file path. Ask questions about the image content."
}

func (t *VisionTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"image_url": map[string]interface{}{
				"type":        "string",
				"description": "Image URL or local file path",
			},
			"question": map[string]interface{}{
				"type":        "string",
				"description": "What to analyze about the image",
			},
		},
		"required": []string{"image_url", "question"},
	}
}

func (t *VisionTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	imageURL, ok := args["image_url"].(string)
	if !ok {
		return ErrorResult("image_url is required")
	}

	question, ok := args["question"].(string)
	if !ok {
		return ErrorResult("question is required")
	}

	var imageData []byte
	var mimeType string
	var err error

	if strings.HasPrefix(imageURL, "http://") || strings.HasPrefix(imageURL, "https://") {
		imageData, mimeType, err = t.fetchFromURL(ctx, imageURL)
	} else {
		imageData, mimeType, err = t.loadFromFile(imageURL)
	}

	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to load image: %v", err))
	}

	if len(imageData) > int(t.maxFileSize) {
		return ErrorResult(fmt.Sprintf("image too large: %d bytes (max: %d)", len(imageData), t.maxFileSize))
	}

	if !t.isValidImageType(imageData) {
		return ErrorResult("invalid image type (must be PNG, JPEG, GIF, BMP, or WebP)")
	}

	if mimeType == "" {
		mimeType = t.detectMimeType(imageData)
	}

	encoded := base64.StdEncoding.EncodeToString(imageData)

	result := map[string]interface{}{
		"image_url":  imageURL,
		"question":   question,
		"mime_type":  mimeType,
		"size_bytes": len(imageData),
		"base64":     encoded,
		"instruction": fmt.Sprintf("Analyze this image and answer: %s", question),
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")

	return &ToolResult{
		ForLLM:  fmt.Sprintf("Image loaded from %s (%s, %d bytes). Question: %s", imageURL, mimeType, len(imageData), question),
		ForUser: string(resultJSON),
	}
}

func (t *VisionTool) fetchFromURL(ctx context.Context, imageURL string) ([]byte, string, error) {
	if safe, reason := t.urlSafety.IsSafe(imageURL); !safe {
		return nil, "", fmt.Errorf("URL blocked: %s", reason)
	}

	if secrets := DetectSecretsInURL(imageURL); len(secrets) > 0 {
		return nil, "", fmt.Errorf("URL contains secrets: %v", secrets)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("stopped after 5 redirects")
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, t.maxFileSize+1))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read response: %w", err)
	}

	if int64(len(body)) > t.maxFileSize {
		return nil, "", fmt.Errorf("image too large: %d bytes", len(body))
	}

	contentType := resp.Header.Get("Content-Type")
	mimeType := ""
	if contentType != "" {
		parts := strings.Split(contentType, ";")
		mimeType = strings.TrimSpace(parts[0])
	}

	return body, mimeType, nil
}

func (t *VisionTool) loadFromFile(filePath string) ([]byte, string, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, "", fmt.Errorf("file not found: %w", err)
	}

	if info.Size() > t.maxFileSize {
		return nil, "", fmt.Errorf("file too large: %d bytes", info.Size())
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	mimeType := ""
	switch ext {
	case ".png":
		mimeType = "image/png"
	case ".jpg", ".jpeg":
		mimeType = "image/jpeg"
	case ".gif":
		mimeType = "image/gif"
	case ".bmp":
		mimeType = "image/bmp"
	case ".webp":
		mimeType = "image/webp"
	}

	return data, mimeType, nil
}

func (t *VisionTool) isValidImageType(data []byte) bool {
	if len(data) < 2 {
		return false
	}

	if data[0] == 0x89 && data[1] == 0x50 && len(data) >= 4 && data[2] == 0x4E && data[3] == 0x47 {
		return true // PNG
	}
	if data[0] == 0xFF && data[1] == 0xD8 && len(data) >= 3 && data[2] == 0xFF {
		return true // JPEG
	}
	if data[0] == 0x47 && data[1] == 0x49 && len(data) >= 4 && data[2] == 0x46 {
		return true // GIF
	}
	if data[0] == 0x42 && data[1] == 0x4D {
		return true // BMP
	}
	if len(data) >= 12 && string(data[8:12]) == "WEBP" {
		return true // WebP
	}

	return false
}

func (t *VisionTool) detectMimeType(data []byte) string {
	if len(data) < 2 {
		return "application/octet-stream"
	}

	if data[0] == 0x89 && data[1] == 0x50 && len(data) >= 4 && data[2] == 0x4E && data[3] == 0x47 {
		return "image/png"
	}
	if data[0] == 0xFF && data[1] == 0xD8 && len(data) >= 3 && data[2] == 0xFF {
		return "image/jpeg"
	}
	if data[0] == 0x47 && data[1] == 0x49 && len(data) >= 4 && data[2] == 0x46 {
		return "image/gif"
	}
	if data[0] == 0x42 && data[1] == 0x4D {
		return "image/bmp"
	}
	if len(data) >= 12 && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}

	return "application/octet-stream"
}
