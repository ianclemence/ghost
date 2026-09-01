package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
)

const (
	defaultFALBaseURL = "https://fal.run"
	defaultFALModel   = "fal-ai/flux/schnell"
)

// ImageGenerateTool generates images using FAL.ai API.
type ImageGenerateTool struct {
	apiKey     string
	model      string
	baseURL    string
	outputDir  string
	httpClient *http.Client
}

func NewImageGenerateTool(workspace string) *ImageGenerateTool {
	apiKey := os.Getenv("FAL_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("FAL_KEY")
	}

	return &ImageGenerateTool{
		apiKey:    apiKey,
		model:     defaultFALModel,
		baseURL:   defaultFALBaseURL,
		outputDir: filepath.Join(workspace, "generated"),
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (t *ImageGenerateTool) Name() string {
	return "image_generate"
}

func (t *ImageGenerateTool) Description() string {
	return "Generate images using AI. Provide a text prompt to create an image. Requires FAL_API_KEY environment variable."
}

func (t *ImageGenerateTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"prompt": map[string]interface{}{
				"type":        "string",
				"description": "Text prompt describing the image to generate",
			},
			"width": map[string]interface{}{
				"type":        "integer",
				"description": "Image width in pixels (default: 1024)",
			},
			"height": map[string]interface{}{
				"type":        "integer",
				"description": "Image height in pixels (default: 1024)",
			},
			"num_images": map[string]interface{}{
				"type":        "integer",
				"description": "Number of images to generate (default: 1)",
			},
			"seed": map[string]interface{}{
				"type":        "integer",
				"description": "Random seed for reproducibility",
			},
			"model": map[string]interface{}{
				"type":        "string",
				"description": "FAL model to use (default: fal-ai/flux/schnell)",
			},
		},
		"required": []string{"prompt"},
	}
}

func (t *ImageGenerateTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	if t.apiKey == "" {
		return ErrorResult("FAL_API_KEY environment variable is not set. Get your API key from https://fal.ai")
	}

	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		return ErrorResult("prompt is required")
	}

	width := 1024
	if w, ok := args["width"].(float64); ok && w > 0 {
		width = int(w)
	}

	height := 1024
	if h, ok := args["height"].(float64); ok && h > 0 {
		height = int(h)
	}

	numImages := 1
	if n, ok := args["num_images"].(float64); ok && n > 0 {
		numImages = int(n)
	}

	model := t.model
	if m, ok := args["model"].(string); ok && m != "" {
		model = m
	}

	// Build request
	requestBody := map[string]interface{}{
		"prompt":     prompt,
		"image_size": map[string]interface{}{"width": width, "height": height},
		"num_images": numImages,
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to marshal request: %v", err))
	}

	url := fmt.Sprintf("%s/%s", t.baseURL, model)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to create request: %v", err))
	}

	req.Header.Set("Authorization", "Key "+t.apiKey)
	req.Header.Set("Content-Type", "application/json")

	logger.InfoCF("image-gen", "Generating image", map[string]interface{}{
		"model":  model,
		"prompt": prompt[:min(50, len(prompt))],
	})

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return ErrorResult(fmt.Sprintf("request failed: %v", err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read response: %v", err))
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return ErrorResult(fmt.Sprintf("API error (HTTP %d): %s", resp.StatusCode, string(respBody)))
	}

	// Parse response
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return ErrorResult(fmt.Sprintf("failed to parse response: %v", err))
	}

	// Save images
	os.MkdirAll(t.outputDir, 0755)

	var savedFiles []string
	if images, ok := result["images"].([]interface{}); ok {
		for i, img := range images {
			if imgMap, ok := img.(map[string]interface{}); ok {
				if url, ok := imgMap["url"].(string); ok {
					fileName := fmt.Sprintf("generated_%s_%d.png", time.Now().Format("20060102_150405"), i)
					filePath := filepath.Join(t.outputDir, fileName)

					if err := t.downloadImage(ctx, url, filePath); err != nil {
						logger.ErrorCF("image-gen", "Failed to save image", map[string]interface{}{
							"error": err.Error(),
						})
						continue
					}
					savedFiles = append(savedFiles, filePath)
				}
			}
		}
	}

	if len(savedFiles) == 0 {
		return UserResult("Image generated but could not be saved. Check API response format.")
	}

	resultText := fmt.Sprintf("Generated %d image(s):\n", len(savedFiles))
	for _, f := range savedFiles {
		resultText += fmt.Sprintf("  - %s\n", f)
	}

	return UserResult(resultText)
}

func (t *ImageGenerateTool) downloadImage(ctx context.Context, url, filePath string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024))
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
