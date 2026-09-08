package voice

// Local speech-to-text behind the same Transcriber interface the cloud
// engines use. The sidecar is whisper.cpp's whisper-server, run on
// loopback with --inference-path /v1/audio/transcriptions, so it speaks
// the same OpenAI-compatible multipart shape as Groq: file, model, and
// response_format fields in, {"text": ...} out. The runtime never knows
// (or cares) whether transcription ran locally or in the cloud.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
)

// DefaultLocalSTTPort is the loopback port the whisper-server sidecar
// listens on. Chosen next to Ollama's 11434; loopback-only, no firewall
// rule needed.
const DefaultLocalSTTPort = 11435

// Engine selection values for speech-to-text.
const (
	EngineAuto     = "auto"
	EngineLocal    = "local"
	EngineGroq     = "groq"
	EngineMoonshot = "moonshot"
	EngineOff      = "off"
)

// SelectConfig carries everything needed to pick a transcriber, as plain
// values so this package stays free of config-layer imports.
type SelectConfig struct {
	Engine      string
	LocalURL    string
	MoonshotKey string
	GroqKey     string
}

// LocalBaseURL renders the sidecar base URL for a port (0 = default).
func LocalBaseURL(port int) string {
	if port <= 0 {
		port = DefaultLocalSTTPort
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// LocalTranscriber posts audio files to a local whisper-server.
type LocalTranscriber struct {
	baseURL    string
	httpClient *http.Client
}

// NewLocalTranscriber builds a transcriber for a whisper-server base URL
// (e.g. http://127.0.0.1:11435). Availability is probed live: a missing
// or stopped sidecar fails closed, never silently.
func NewLocalTranscriber(baseURL string) *LocalTranscriber {
	return &LocalTranscriber{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 5 * time.Minute},
	}
}

// IsAvailable reports whether the sidecar answers on loopback right now.
func (t *LocalTranscriber) IsAvailable() bool {
	if t == nil || t.baseURL == "" {
		return false
	}
	host := strings.TrimPrefix(strings.TrimPrefix(t.baseURL, "http://"), "https://")
	conn, err := net.DialTimeout("tcp", host, 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Transcribe sends one audio file to the sidecar and returns its text.
func (t *LocalTranscriber) Transcribe(ctx context.Context, audioFilePath string) (*TranscriptionResponse, error) {
	audioFile, err := os.Open(audioFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open audio file: %w", err)
	}
	defer audioFile.Close()

	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)
	part, err := writer.CreateFormFile("file", filepath.Base(audioFilePath))
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := io.Copy(part, audioFile); err != nil {
		return nil, fmt.Errorf("failed to copy file content: %w", err)
	}
	// whisper-server ignores the model name (one model per process); the
	// field is required by the OpenAI-compatible shape.
	if err := writer.WriteField("model", "whisper-1"); err != nil {
		return nil, fmt.Errorf("failed to write model field: %w", err)
	}
	if err := writer.WriteField("response_format", "json"); err != nil {
		return nil, fmt.Errorf("failed to write response_format field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", t.baseURL+"/v1/audio/transcriptions", &requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("local transcription unavailable: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("local transcription failed (status %d)", resp.StatusCode)
	}
	var result TranscriptionResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	logger.DebugCF("voice", "Local transcription completed", map[string]interface{}{
		"text_length": len(result.Text),
	})
	return &result, nil
}

// AutoTranscriber tries the primary (local) engine first and falls back
// to the secondary (cloud) engine per call, so a sidecar that restarts
// is picked back up without restarting Ghost.
type AutoTranscriber struct {
	primary  Transcriber
	fallback Transcriber
}

// NewAutoTranscriber wraps a primary and a fallback. Either may be nil,
// but not both.
func NewAutoTranscriber(primary, fallback Transcriber) *AutoTranscriber {
	return &AutoTranscriber{primary: primary, fallback: fallback}
}

// IsAvailable is true when either engine can run right now.
func (a *AutoTranscriber) IsAvailable() bool {
	if a == nil {
		return false
	}
	return (a.primary != nil && a.primary.IsAvailable()) ||
		(a.fallback != nil && a.fallback.IsAvailable())
}

// Transcribe uses the primary engine when it is up, else the fallback.
func (a *AutoTranscriber) Transcribe(ctx context.Context, audioFilePath string) (*TranscriptionResponse, error) {
	if a.primary != nil && a.primary.IsAvailable() {
		return a.primary.Transcribe(ctx, audioFilePath)
	}
	if a.fallback != nil && a.fallback.IsAvailable() {
		return a.fallback.Transcribe(ctx, audioFilePath)
	}
	return nil, fmt.Errorf("speech recognition unavailable")
}

// NormalizeEngine canonicalizes an engine preference, defaulting to auto.
func NormalizeEngine(engine string) string {
	engine = strings.ToLower(strings.TrimSpace(engine))
	switch engine {
	case EngineLocal, EngineGroq, EngineMoonshot, EngineOff:
		return engine
	default:
		return EngineAuto
	}
}

// SelectTranscriber picks the transcriber for an engine preference:
// local-first with cloud fallback by default ("auto"), a single engine
// when pinned, nil when off or when nothing is configured.
func SelectTranscriber(cfg SelectConfig) Transcriber {
	engine := NormalizeEngine(cfg.Engine)
	var local Transcriber
	if cfg.LocalURL != "" {
		local = NewLocalTranscriber(cfg.LocalURL)
	}
	cloud := func() Transcriber {
		if cfg.MoonshotKey != "" {
			return NewMoonshotTranscriber(cfg.MoonshotKey)
		}
		if cfg.GroqKey != "" {
			return NewGroqTranscriber(cfg.GroqKey)
		}
		return nil
	}
	switch engine {
	case EngineOff:
		return nil
	case EngineLocal:
		return local
	case EngineGroq:
		if cfg.GroqKey != "" {
			return NewGroqTranscriber(cfg.GroqKey)
		}
		return nil
	case EngineMoonshot:
		if cfg.MoonshotKey != "" {
			return NewMoonshotTranscriber(cfg.MoonshotKey)
		}
		return nil
	default:
		c := cloud()
		if local == nil {
			return c
		}
		if c == nil {
			return local
		}
		return NewAutoTranscriber(local, c)
	}
}

// DescribeTranscriber names the engine behind a transcriber for logs.
func DescribeTranscriber(t Transcriber) string {
	switch t.(type) {
	case *AutoTranscriber:
		return "local-first (cloud fallback)"
	case *LocalTranscriber:
		return "local"
	case *MoonshotTranscriber:
		return "moonshot"
	case *GroqTranscriber:
		return "groq"
	default:
		return "none"
	}
}
