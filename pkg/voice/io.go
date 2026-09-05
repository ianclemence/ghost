package voice

// Channel-neutral voice interfaces: voice is an input/output channel into
// the SAME Ghost runtime — never a separate agent architecture.
//
// Pipeline: Voice Input → SpeechRecognizer → canonical user message →
// Ghost Runtime → canonical events → response text → SpeechSynthesizer.
// The brain produces a normal response; TTS is presentation. Vendors
// (local or cloud speech) plug in behind these interfaces.

import (
	"context"
	"errors"
	"os"

	"github.com/ianclemence/ghost/pkg/bus"
)

// Transcript is a recognition result. Partial transcripts support
// future streaming; final ones enter the runtime as messages.
type Transcript struct {
	Text    string `json:"text"`
	Partial bool   `json:"partial"`
}

// SpeechRecognizer converts audio to text. Local and cloud engines share
// this interface; the runtime never knows which one ran.
type SpeechRecognizer interface {
	Recognize(ctx context.Context, audio []byte, mimeType string) (Transcript, error)
}

// SpeechSynthesizer converts response text to audio. Presentation only.
type SpeechSynthesizer interface {
	Synthesize(ctx context.Context, text string) (audio []byte, mimeType string, err error)
}

// FileTranscriberAdapter wraps the existing file-based Transcriber
// (Groq/Moonshot) as a SpeechRecognizer so current engines work through
// the new interface without modification.
type FileTranscriberAdapter struct {
	inner Transcriber
}

// NewFileTranscriberAdapter adapts a Transcriber. Nil inner fails closed.
func NewFileTranscriberAdapter(t Transcriber) *FileTranscriberAdapter {
	return &FileTranscriberAdapter{inner: t}
}

func (a *FileTranscriberAdapter) Recognize(ctx context.Context, audio []byte, mimeType string) (Transcript, error) {
	if a.inner == nil || !a.inner.IsAvailable() {
		return Transcript{}, errors.New("speech recognition unavailable")
	}
	if len(audio) == 0 {
		return Transcript{}, errors.New("empty audio")
	}
	f, err := os.CreateTemp("", "ghost-voice-*.bin")
	if err != nil {
		return Transcript{}, err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write(audio); err != nil {
		f.Close()
		return Transcript{}, err
	}
	f.Close()
	resp, err := a.inner.Transcribe(ctx, name)
	if err != nil {
		return Transcript{}, err
	}
	return Transcript{Text: resp.Text}, nil
}

// ToInbound converts a final transcript into the canonical inbound
// message — the exact shape text channels produce. Voice identity rides
// in SenderID/Metadata like every other channel.
func ToInbound(t Transcript, senderID, chatID, sessionKey string) (bus.InboundMessage, error) {
	if t.Partial {
		return bus.InboundMessage{}, errors.New("partial transcripts do not enter the runtime")
	}
	if t.Text == "" {
		return bus.InboundMessage{}, errors.New("empty transcript")
	}
	return bus.InboundMessage{
		Channel: "voice", SenderID: senderID, ChatID: chatID,
		Content: t.Text, SessionKey: sessionKey,
		Metadata: map[string]string{"input": "voice"},
	}, nil
}
