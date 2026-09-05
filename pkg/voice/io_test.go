package voice

import (
	"context"
	"errors"
	"testing"
)

type fakeRecognizer struct{ text string }

func (f *fakeRecognizer) Recognize(ctx context.Context, audio []byte, mime string) (Transcript, error) {
	if len(audio) == 0 {
		return Transcript{}, errors.New("empty")
	}
	return Transcript{Text: f.text}, nil
}

type fakeTranscriber struct{ text string }

func (f *fakeTranscriber) Transcribe(ctx context.Context, path string) (*TranscriptionResponse, error) {
	return &TranscriptionResponse{Text: f.text}, nil
}
func (f *fakeTranscriber) IsAvailable() bool { return true }

func TestVoiceToInbound(t *testing.T) {
	rec := &fakeRecognizer{text: "what's the weather"}
	tr, err := rec.Recognize(context.Background(), []byte{1, 2, 3}, "audio/wav")
	if err != nil {
		t.Fatal(err)
	}
	msg, err := ToInbound(tr, "owner", "chat-1", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	// Voice enters the SAME shape as text channels.
	if msg.Channel != "voice" || msg.Content != "what's the weather" {
		t.Fatalf("bad inbound: %+v", msg)
	}
	// Partial transcripts never enter the runtime.
	if _, err := ToInbound(Transcript{Text: "hel", Partial: true}, "o", "c", "s"); err == nil {
		t.Fatal("partial must not enter runtime")
	}
}

func TestAdapterUnavailableFailsClosed(t *testing.T) {
	a := NewFileTranscriberAdapter(nil)
	if _, err := a.Recognize(context.Background(), []byte{1}, "audio/wav"); err == nil {
		t.Fatal("nil engine must fail closed")
	}
}

func TestAdapterUsesEngine(t *testing.T) {
	a := NewFileTranscriberAdapter(&fakeTranscriber{text: "hello ghost"})
	tr, err := a.Recognize(context.Background(), []byte{1, 2}, "audio/wav")
	if err != nil || tr.Text != "hello ghost" {
		t.Fatalf("adapter broken: %+v %v", tr, err)
	}
}

func TestEngineHonestUnavailable(t *testing.T) {
	e := &Engine{}
	if e.InputAvailable() || e.OutputAvailable() {
		t.Fatal("empty engine must report unavailable")
	}
	if _, err := e.Transcribe(context.Background(), []byte{1}, "audio/wav"); err == nil {
		t.Fatal("must fail honestly without recognizer")
	}
}

func TestEngineTranscribeEndToEnd(t *testing.T) {
	e := &Engine{Recognizer: NewFileTranscriberAdapter(&fakeTranscriber{text: "hello"})}
	tr, err := e.Transcribe(context.Background(), []byte{1, 2, 3}, "audio/wav")
	if err != nil || tr.Text != "hello" {
		t.Fatalf("engine broken: %+v %v", tr, err)
	}
	msg, err := ToInbound(tr, "owner", "chat-1", "sess-1")
	if err != nil || msg.Channel != "voice" {
		t.Fatal("voice must enter the canonical inbound shape")
	}
}

func TestEngineRejectsHugeAudio(t *testing.T) {
	e := &Engine{Recognizer: &fakeRecognizer{text: "x"}}
	if _, err := e.Transcribe(context.Background(), make([]byte, 11<<20), "audio/wav"); err == nil {
		t.Fatal("oversize audio must be rejected")
	}
}
