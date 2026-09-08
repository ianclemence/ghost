package channels

// Shared voice-note transcription: every channel funnels audio files
// through here so markers, timeouts, and failure behavior stay uniform.
// A 90s budget covers the longest notes the channels allow (WeChat caps
// voice at 60s; local base.en runs ~1.4x realtime) on any engine.

import (
	"context"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/voice"
)

const voiceTranscriptionTimeout = 90 * time.Second

// transcribeVoiceFile runs one audio file through the transcriber and
// returns the marker text for the chat (telegram-style markers). A missing
// or down transcriber yields a bare marker, never silence about the cause.
func transcribeVoiceFile(ctx context.Context, tr voice.Transcriber, logPrefix, path, noun string) string {
	if tr == nil || !tr.IsAvailable() {
		return "[" + noun + "]"
	}
	tctx, cancel := context.WithTimeout(ctx, voiceTranscriptionTimeout)
	defer cancel()
	res, err := tr.Transcribe(tctx, path)
	if err != nil {
		logger.ErrorCF(logPrefix, "Voice transcription failed", map[string]interface{}{
			"error": err.Error(),
			"path":  path,
		})
		return "[" + noun + " (transcription failed)]"
	}
	return "[" + noun + " transcription: " + res.Text + "]"
}

// audioExtensionForType maps an audio MIME type to a file extension so
// downloaded voice notes keep a proper suffix for format probing. Empty
// string means unknown — callers must skip rather than guess.
func audioExtensionForType(contentType string) string {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	switch ct {
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/wav", "audio/x-wav", "audio/wave", "audio/vnd.wave":
		return ".wav"
	case "audio/ogg", "application/ogg", "application/x-ogg":
		return ".ogg"
	case "audio/opus":
		return ".opus"
	case "audio/mp4", "audio/x-m4a", "audio/aac":
		return ".m4a"
	case "audio/amr", "audio/amr-wb":
		return ".amr"
	case "audio/flac":
		return ".flac"
	default:
		return ""
	}
}
