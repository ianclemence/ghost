package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type ImageGenTool struct {
	workspace   string
	apiKey      string
	apiBase     string
	maxFileSize int64
}

func NewImageGenTool(workspace, apiKey, apiBase string) *ImageGenTool {
	if apiBase == "" {
		apiBase = "https://api.openai.com/v1"
	}
	return &ImageGenTool{
		workspace:   workspace,
		apiKey:      apiKey,
		apiBase:     apiBase,
		maxFileSize: 10 * 1024 * 1024,
	}
}

func (t *ImageGenTool) Name() string {
	return "image_generate"
}

func (t *ImageGenTool) Description() string {
	return "Generate an image from a text prompt using DALL-E. Returns the generated image as a file path."
}

func (t *ImageGenTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"prompt": map[string]interface{}{
				"type":        "string",
				"description": "Text description of the image to generate",
			},
			"size": map[string]interface{}{
				"type":        "string",
				"description": "Image size (1024x1024, 1792x1024, 1024x1792)",
				"enum":        []string{"1024x1024", "1792x1024", "1024x1792"},
			},
			"quality": map[string]interface{}{
				"type":        "string",
				"description": "Image quality (standard, hd)",
				"enum":        []string{"standard", "hd"},
			},
		},
		"required": []string{"prompt"},
	}
}

func (t *ImageGenTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	prompt, ok := args["prompt"].(string)
	if !ok {
		return ErrorResult("prompt is required")
	}

	if t.apiKey == "" {
		return ErrorResult("OpenAI API key not configured")
	}

	size := "1024x1024"
	if s, ok := args["size"].(string); ok {
		size = s
	}

	quality := "standard"
	if q, ok := args["quality"].(string); ok {
		quality = q
	}

	result, err := t.generateImage(ctx, prompt, size, quality)
	if err != nil {
		return ErrorResult(fmt.Sprintf("image generation failed: %v", err))
	}

	return &ToolResult{
		ForLLM:  fmt.Sprintf("Image generated successfully from prompt: %s", prompt),
		ForUser: result,
	}
}

func (t *ImageGenTool) generateImage(ctx context.Context, prompt, size, quality string) (string, error) {
	requestBody := map[string]interface{}{
		"model":           "dall-e-3",
		"prompt":          prompt,
		"n":               1,
		"size":            size,
		"quality":         quality,
		"response_format": "b64_json",
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	url := t.apiBase + "/images/generations"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.apiKey)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var response struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
			URL     string `json:"url"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(response.Data) == 0 {
		return "", fmt.Errorf("no image data in response")
	}

	imageData := response.Data[0]

	var imageBytes []byte
	if imageData.B64JSON != "" {
		imageBytes, err = base64.StdEncoding.DecodeString(imageData.B64JSON)
		if err != nil {
			return "", fmt.Errorf("failed to decode base64: %w", err)
		}
	} else if imageData.URL != "" {
		imageBytes, err = t.downloadImage(ctx, imageData.URL)
		if err != nil {
			return "", fmt.Errorf("failed to download image: %w", err)
		}
	} else {
		return "", fmt.Errorf("no image data in response")
	}

	filePath, err := t.saveImage(imageBytes, prompt)
	if err != nil {
		return "", fmt.Errorf("failed to save image: %w", err)
	}

	result := map[string]interface{}{
		"prompt":    prompt,
		"size":      size,
		"quality":   quality,
		"file_path": filePath,
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

func (t *ImageGenTool) downloadImage(ctx context.Context, imageURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, t.maxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if int64(len(body)) > t.maxFileSize {
		return nil, fmt.Errorf("image too large: %d bytes", len(body))
	}

	return body, nil
}

func (t *ImageGenTool) saveImage(data []byte, prompt string) (string, error) {
	cacheDir := filepath.Join(t.workspace, "cache", "images")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	sanitized := sanitizeFilename(prompt)
	if len(sanitized) > 50 {
		sanitized = sanitized[:50]
	}

	filename := fmt.Sprintf("%s_%d.png", sanitized, time.Now().UnixMilli())
	filePath := filepath.Join(cacheDir, filename)

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return filePath, nil
}

func sanitizeFilename(s string) string {
	result := make([]rune, 0, len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			result = append(result, r)
		} else if r == ' ' {
			result = append(result, '_')
		}
	}
	if len(result) == 0 {
		return "image"
	}
	return string(result)
}
