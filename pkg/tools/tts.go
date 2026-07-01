package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type TTSConfig struct {
	Enabled      bool   `json:"enabled"`
	Provider     string `json:"provider"`
	DefaultVoice string `json:"default_voice"`
	OutputFormat string `json:"output_format"`
}

type TTSTool struct {
	config    TTSConfig
	workspace string
}

func NewTTSTool(cfg TTSConfig, workspace string) *TTSTool {
	if cfg.Provider == "" {
		cfg.Provider = "edge-tts"
	}
	if cfg.DefaultVoice == "" {
		cfg.DefaultVoice = "en-US-AriaNeural"
	}
	if cfg.OutputFormat == "" {
		cfg.OutputFormat = "mp3"
	}
	return &TTSTool{
		config:    cfg,
		workspace: workspace,
	}
}

func (t *TTSTool) Name() string {
	return "tts"
}

func (t *TTSTool) Description() string {
	return "Convert text to speech using Edge TTS (free, no API key required). Returns an audio file."
}

func (t *TTSTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{
				"type":        "string",
				"description": "Text to convert to speech (max 5000 characters)",
			},
			"voice": map[string]interface{}{
				"type":        "string",
				"description": "Voice name (e.g., en-US-AriaNeural, zh-CN-XiaoxiaoNeural). Default: en-US-AriaNeural",
			},
			"format": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"mp3", "opus"},
				"description": "Output audio format (default: mp3)",
			},
		},
		"required": []string{"text"},
	}
}

func (t *TTSTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	text, _ := args["text"].(string)
	if text == "" {
		return ErrorResult("text is required")
	}

	if len(text) > 5000 {
		text = text[:5000]
	}

	voice := t.config.DefaultVoice
	if v, ok := args["voice"].(string); ok && v != "" {
		voice = v
	}

	format := t.config.OutputFormat
	if f, ok := args["format"].(string); ok && f != "" {
		format = f
	}

	audioPath, err := t.synthesize(ctx, text, voice, format)
	if err != nil {
		return ErrorResult(fmt.Sprintf("TTS synthesis failed: %v", err)).WithError(err)
	}

	result := map[string]interface{}{
		"text":  text,
		"voice": voice,
		"format": format,
		"file":  audioPath,
	}
	raw, _ := json.Marshal(result)

	toolResult := UserResult(string(raw))
	toolResult.ForLLM = fmt.Sprintf("Audio file generated: %s", audioPath)
	return toolResult
}

func (t *TTSTool) synthesize(ctx context.Context, text, voice, format string) (string, error) {
	mediaDir := filepath.Join(t.workspace, "media")
	if err := os.MkdirAll(mediaDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create media directory: %w", err)
	}

	timestamp := time.Now().UnixMilli()
	outputFile := filepath.Join(mediaDir, fmt.Sprintf("tts-%d.%s", timestamp, format))

	cmd := exec.CommandContext(ctx, "edge-tts",
		"--voice", voice,
		"--text", text,
		"--write-media", outputFile,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("edge-tts failed: %w, output: %s", err, string(output))
	}

	if _, err := os.Stat(outputFile); os.IsNotExist(err) {
		return "", fmt.Errorf("audio file not created")
	}

	return outputFile, nil
}

func (t *TTSTool) ListVoices() []string {
	return []string{
		"en-US-AriaNeural",
		"en-US-GuyNeural",
		"en-US-JennyNeural",
		"en-GB-SoniaNeural",
		"en-GB-RyanNeural",
		"zh-CN-XiaoxiaoNeural",
		"zh-CN-YunxiNeural",
		"ja-JP-NanamiNeural",
		"ja-JP-KeitaNeural",
		"ko-KR-SunHiNeural",
		"ko-KR-InJoonNeural",
		"fr-FR-DeniseNeural",
		"fr-FR-HenriNeural",
		"de-DE-KatjaNeural",
		"de-DE-ConradNeural",
		"es-ES-ElviraNeural",
		"es-ES-AlvaroNeural",
		"pt-BR-FranciscaNeural",
		"pt-BR-AntonioNeural",
		"it-IT-IsabellaNeural",
		"it-IT-DiegoNeural",
		"ru-RU-SvetlanaNeural",
		"ru-RU-DmitryNeural",
		"ar-SA-ZariyahNeural",
		"ar-SA-HamedNeural",
		"hi-IN-SwaraNeural",
		"hi-IN-MadhurNeural",
	}
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
}
