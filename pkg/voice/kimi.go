package voice

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/providers"
)

type MoonshotTranscriber struct {
	provider *providers.MoonshotProvider
}

func NewMoonshotTranscriber(apiKey string) *MoonshotTranscriber {
	// Use default Moonshot base URL
	return &MoonshotTranscriber{
		provider: providers.NewMoonshotProvider(apiKey, ""),
	}
}

func (t *MoonshotTranscriber) Transcribe(ctx context.Context, audioFilePath string) (*TranscriptionResponse, error) {
	logger.InfoCF("voice", "Starting Moonshot transcription", map[string]interface{}{"file": audioFilePath})

	data, err := os.ReadFile(audioFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read audio file: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	ext := strings.ToLower(filepath.Ext(audioFilePath))
	mimeType := "video/mp4" // Default fallback

	switch ext {
	case ".ogg":
		mimeType = "audio/ogg"
	case ".mp3":
		mimeType = "audio/mpeg"
	case ".wav":
		mimeType = "audio/wav"
	case ".mp4":
		mimeType = "video/mp4"
	}

	// Kimi docs specify video_url for multimodal content
	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, encoded)

	messages := []providers.Message{
		{
			Role:    "system",
			Content: "You are an expert audio transcriber. Transcribe the audio content verbatim. Only output the transcription text, no explanations.",
		},
		{
			Role: "user",
			MultiContent: []providers.ContentPart{
				{
					Type: "text",
					Text: "Please transcribe this audio file.",
				},
				{
					Type: "video_url",
					VideoURL: &providers.VideoURL{
						URL: dataURL,
					},
				},
			},
		},
	}

	// Disable thinking for transcription to be faster
	options := map[string]interface{}{
		"thinking": false,
	}

	resp, err := t.provider.Chat(ctx, messages, nil, "kimi-k2.5", options)
	if err != nil {
		return nil, fmt.Errorf("moonshot transcription failed: %w", err)
	}

	return &TranscriptionResponse{
		Text: resp.Content,
	}, nil
}

func (t *MoonshotTranscriber) IsAvailable() bool {
	return true
}
