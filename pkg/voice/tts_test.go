package voice

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareSpeechText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Hello there.", "Hello there."},
		{"fence dropped", "Hi.\n```go\nfmt.Println()\n```\nBye.", "Hi.\n\nBye."},
		{"inline code kept", "Run `ghost status` now.", "Run ghost status now."},
		{"link text kept", "See [the docs](https://x.test/y).", "See the docs."},
		{"header stripped", "# Title\nBody here.", "Title\nBody here."},
		{"emphasis stripped", "This is **bold** and *italic*.", "This is bold and italic."},
		{"list markers stripped", "- one\n- two", "one\ntwo"},
		{"quote stripped", "> quoted\nnext", "quoted\nnext"},
		{"whitespace collapsed", "too   many\t spaces", "too many spaces"},
		{"empty after cleanup", "```x```", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PrepareSpeechText(tt.in); got != tt.want {
				t.Errorf("got %q want %q", got, tt.want)
			}
		})
	}
}

// stubSynthBin writes a fake wav to the --output-filename argument, acting
// as the sherpa CLI without any inference.
func stubSynthBin(t *testing.T, dir string) string {
	t.Helper()
	script := filepath.Join(dir, "sherpa-onnx-offline-tts")
	body := "#!/bin/sh\nfor a in \"$@\"; do case $a in --output-filename=*) out=${a#*=};; esac; done\nprintf 'RIFFfakemonoWAVE' > \"$out\"\n"
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	return script
}

func fakeVoiceDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "en_US-test-low.onnx"), []byte("model"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokens.txt"), []byte("tokens"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "espeak-ng-data"), 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLocalSynthesizeWavFallback(t *testing.T) {
	// Hide ffmpeg so the WAV fallback path is deterministic.
	t.Setenv("PATH", t.TempDir())
	binDir := t.TempDir()
	stubSynthBin(t, binDir)
	s := NewLocalSynthesizer(filepath.Join(binDir, "sherpa-onnx-offline-tts"), fakeVoiceDir(t), 1.0)
	audio, mime, err := s.Synthesize(context.Background(), "Hello **there**.")
	if err != nil {
		t.Fatal(err)
	}
	if mime != "audio/wav" {
		t.Fatalf("mime = %q, want audio/wav", mime)
	}
	if !strings.HasPrefix(string(audio), "RIFF") {
		t.Fatal("expected stub wav bytes")
	}
	if _, _, err := s.Synthesize(context.Background(), "```only code```"); err == nil {
		t.Fatal("empty-after-cleanup must error")
	}
}

func TestWavToMp3(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	wav := filepath.Join(t.TempDir(), "in.wav")
	// Minimal silent mono 16-bit wav: 44-byte header + samples.
	hdr := []byte("RIFF\x24\x08\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x80>\x00\x00\x00}\x00\x00\x02\x00\x10\x00data\x00\x08\x00\x00")
	samples := make([]byte, 32000)
	if err := os.WriteFile(wav, append(hdr, samples...), 0644); err != nil {
		t.Fatal(err)
	}
	mp3, err := wavToMp3(context.Background(), wav)
	if err != nil {
		t.Fatal(err)
	}
	if len(mp3) < 3 || string(mp3[:3]) != "ID3" && mp3[0] != 0xFF {
		t.Fatal("expected mp3 bytes")
	}
}

type stubSynth struct {
	audio []byte
	mime  string
	err   error
}

func (s stubSynth) Synthesize(_ context.Context, _ string) ([]byte, string, error) {
	if s.err != nil {
		return nil, "", s.err
	}
	return s.audio, s.mime, nil
}

func TestAutoSynthesizer(t *testing.T) {
	primary := stubSynth{audio: []byte("local"), mime: "audio/mpeg"}
	fallback := stubSynth{audio: []byte("cloud"), mime: "audio/mpeg"}
	a, m, err := NewAutoSynthesizer(primary, fallback).Synthesize(context.Background(), "hi")
	if err != nil || string(a) != "local" || m != "audio/mpeg" {
		t.Fatalf("primary must win: %q %q %v", a, m, err)
	}
	broken := stubSynth{err: errors.New("boom")}
	a, _, err = NewAutoSynthesizer(broken, fallback).Synthesize(context.Background(), "hi")
	if err != nil || string(a) != "cloud" {
		t.Fatalf("must fall back: %q %v", a, err)
	}
	if (NewAutoSynthesizer(nil, nil)).Available() {
		t.Fatal("empty auto must be unavailable")
	}
	if _, _, err := NewAutoSynthesizer(nil, nil).Synthesize(context.Background(), "hi"); err == nil {
		t.Fatal("empty auto must error")
	}
}

func TestSelectSynthesizer(t *testing.T) {
	if SelectSynthesizer(SynthConfig{Engine: "off"}) != nil {
		t.Fatal("off must yield nil")
	}
	binDir := t.TempDir()
	stubSynthBin(t, binDir)
	voiceDir := fakeVoiceDir(t)
	got := SelectSynthesizer(SynthConfig{Engine: "local", BinPath: filepath.Join(binDir, "sherpa-onnx-offline-tts"), VoiceDir: voiceDir})
	if _, ok := got.(*LocalSynthesizer); !ok {
		t.Fatalf("local must yield LocalSynthesizer, got %T", got)
	}
	if SelectSynthesizer(SynthConfig{Engine: "local", BinPath: "/nonexistent", VoiceDir: voiceDir}) != nil {
		t.Fatal("missing binary must yield nil")
	}
}

func TestVoiceModelFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := voiceModelFile(dir); err == nil {
		t.Fatal("empty dir must error")
	}
	a := filepath.Join(dir, "a.onnx")
	b := filepath.Join(dir, "b.onnx")
	_ = os.WriteFile(a, []byte("x"), 0644)
	_ = os.WriteFile(b, []byte("x"), 0644)
	if _, err := voiceModelFile(dir); err == nil {
		t.Fatal("ambiguous dir must error")
	}
	_ = os.Remove(b)
	_ = os.WriteFile(filepath.Join(dir, "model.onnx"), []byte("x"), 0644)
	got, err := voiceModelFile(dir)
	if err != nil || got != filepath.Join(dir, "model.onnx") {
		t.Fatalf("model.onnx must win: %q %v", got, err)
	}
}
