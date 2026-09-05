package voice

// Engine is the minimal push-to-talk voice stack: transcription in,
// synthesis out, one Ghost runtime between them. No audio is stored by
// default; unavailable providers produce honest errors, never silence.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"time"
)

// Engine wires a recognizer and an optional synthesizer.
type Engine struct {
	Recognizer SpeechRecognizer
	Speak      func(ctx context.Context, text string) (audio []byte, mime string, err error)
}

// EngineFromEnv builds the engine from server configuration: Groq key
// for recognition, edge-tts binary for synthesis. Either may be absent
// (honest unavailability per direction).
func EngineFromEnv(groqKey string) *Engine {
	e := &Engine{}
	if groqKey != "" {
		e.Recognizer = NewFileTranscriberAdapter(NewGroqTranscriber(groqKey))
	}
	if _, err := exec.LookPath("edge-tts"); err == nil {
		e.Speak = edgeTTSSpeak
	}
	return e
}

// InputAvailable reports whether speech recognition can run.
func (e *Engine) InputAvailable() bool { return e != nil && e.Recognizer != nil }

// OutputAvailable reports whether synthesis can run.
func (e *Engine) OutputAvailable() bool { return e != nil && e.Speak != nil }

// Transcribe converts push-to-talk audio to text (never stored).
func (e *Engine) Transcribe(ctx context.Context, audio []byte, mime string) (Transcript, error) {
	if !e.InputAvailable() {
		return Transcript{}, errors.New("voice input isn't set up yet (speech recognition unavailable)")
	}
	if len(audio) == 0 {
		return Transcript{}, errors.New("empty audio")
	}
	if len(audio) > 10<<20 {
		return Transcript{}, errors.New("audio too large (10MB max)")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return e.Recognizer.Recognize(ctx, audio, mime)
}

// edgeTTSSpeak synthesizes via the edge-tts CLI into a temp file that is
// always removed (no audio retention).
func edgeTTSSpeak(ctx context.Context, text string) ([]byte, string, error) {
	if len(text) > 5000 {
		text = text[:5000]
	}
	f, err := os.CreateTemp("", "ghost-tts-*.mp3")
	if err != nil {
		return nil, "", err
	}
	name := f.Name()
	f.Close()
	defer os.Remove(name)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "edge-tts", "--text", text, "--write-media", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, "", errors.New("speech synthesis unavailable right now")
	} else {
		_ = out
	}
	audio, err := os.ReadFile(name)
	if err != nil {
		return nil, "", errors.New("speech synthesis unavailable right now")
	}
	return audio, "audio/mpeg", nil
}
