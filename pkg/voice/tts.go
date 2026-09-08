package voice

// Local speech synthesis behind the same SpeechSynthesizer interface the
// cloud engine uses. Each utterance spawns the provisioned
// sherpa-onnx-offline-tts binary (Piper voice), writes a WAV, and
// transcodes to mp3 for delivery. No daemon, no resident RAM, no network:
// a reply costs one process, not one service.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
)

const (
	// DefaultTTSModelsDir is where speech-synthesis voices live.
	DefaultTTSModelsDir = "/var/ghost/models/tts"
	// DefaultSynthBin is the installed TTS engine binary.
	DefaultSynthBin = "/usr/local/bin/sherpa-onnx-offline-tts"
	// ActiveTTSVoiceLink is the stable path the synthesizer loads; it
	// symlinks to whichever voice bundle is active.
	ActiveTTSVoiceLink = "active-tts"
	// maxTTSText bounds a single synthesis call, matching the cloud path.
	maxTTSText = 5000
)

// TTS engine selection values.
const (
	TTSEngineAuto  = "auto"
	TTSEngineLocal = "local"
	TTSEngineEdge  = "edge"
	TTSEngineOff   = "off"
)

// SynthConfig carries everything needed to pick and run a synthesizer.
type SynthConfig struct {
	Engine   string
	Speed    float32
	BinPath  string
	VoiceDir string
}

// NormalizeTTSEngine canonicalizes an engine preference, defaulting to auto.
func NormalizeTTSEngine(engine string) string {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case TTSEngineLocal, TTSEngineEdge, TTSEngineOff:
		return strings.ToLower(strings.TrimSpace(engine))
	default:
		return TTSEngineAuto
	}
}

// synthBinPath resolves the engine binary path.
func synthBinPath(p string) string {
	if strings.TrimSpace(p) != "" {
		return p
	}
	return DefaultSynthBin
}

// synthVoiceDir resolves the active voice bundle directory: the
// active-tts symlink first, then the directory itself when it directly
// holds a model (custom layouts, dev setups).
func synthVoiceDir(modelsDir string) string {
	if strings.TrimSpace(modelsDir) == "" {
		modelsDir = DefaultTTSModelsDir
	}
	link := filepath.Join(modelsDir, ActiveTTSVoiceLink)
	if st, err := os.Stat(link); err == nil && st.IsDir() {
		return link
	}
	if _, err := voiceModelFile(modelsDir); err == nil {
		return modelsDir
	}
	return ""
}

// LocalSynthAvailable reports whether local synthesis can run: engine
// binary present and executable, active voice bundle with a model.
func LocalSynthAvailable(binPath, modelsDir string) bool {
	bin := synthBinPath(binPath)
	st, err := os.Stat(bin)
	if err != nil || st.IsDir() || st.Mode()&0111 == 0 {
		return false
	}
	dir := synthVoiceDir(modelsDir)
	if dir == "" {
		return false
	}
	_, err = voiceModelFile(dir)
	return err == nil
}

// LocalSynthesizer synthesizes via the provisioned sherpa CLI.
type LocalSynthesizer struct {
	bin      string
	voiceDir string
	speed    float32
	threads  int
}

// NewLocalSynthesizer builds a synthesizer for an engine binary and a
// voice bundle directory. Speed <= 0 means normal speed.
func NewLocalSynthesizer(binPath, voiceDir string, speed float32) *LocalSynthesizer {
	if speed <= 0 {
		speed = 1.0
	}
	threads := runtime.NumCPU()
	if threads < 1 {
		threads = 1
	}
	if threads > 4 {
		threads = 4
	}
	return &LocalSynthesizer{bin: binPath, voiceDir: voiceDir, speed: speed, threads: threads}
}

// Synthesize renders text to mp3 audio (WAV fallback when ffmpeg is
// missing). The returned mime always matches the bytes.
func (s *LocalSynthesizer) Synthesize(ctx context.Context, text string) ([]byte, string, error) {
	clean := PrepareSpeechText(text)
	if clean == "" {
		return nil, "", fmt.Errorf("nothing to speak after cleanup")
	}
	model, err := voiceModelFile(s.voiceDir)
	if err != nil {
		return nil, "", err
	}
	wav, err := os.CreateTemp("", "ghost-tts-*.wav")
	if err != nil {
		return nil, "", err
	}
	wavName := wav.Name()
	wav.Close()
	defer os.Remove(wavName)

	tctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(tctx, s.bin,
		"--vits-model="+model,
		"--vits-tokens="+filepath.Join(s.voiceDir, "tokens.txt"),
		"--vits-data-dir="+filepath.Join(s.voiceDir, "espeak-ng-data"),
		"--num-threads="+strconv.Itoa(s.threads),
		fmt.Sprintf("--speed=%g", s.speed),
		"--output-filename="+wavName,
		clean,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		logger.ErrorCF("voice", "Local synthesis failed", map[string]interface{}{"error": err.Error(), "output": string(out)})
		return nil, "", fmt.Errorf("local speech synthesis unavailable")
	}
	wavBytes, err := os.ReadFile(wavName)
	if err != nil || len(wavBytes) == 0 {
		return nil, "", fmt.Errorf("local speech synthesis produced no audio")
	}
	mp3, err := wavToMp3(ctx, wavName)
	if err != nil {
		return wavBytes, "audio/wav", nil
	}
	return mp3, "audio/mpeg", nil
}

// voiceModelFile finds the acoustic model in a voice bundle: model.onnx
// first, else the lone *.onnx when there is exactly one.
func voiceModelFile(dir string) (string, error) {
	if st, err := os.Stat(filepath.Join(dir, "model.onnx")); err == nil && !st.IsDir() {
		return filepath.Join(dir, "model.onnx"), nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.onnx"))
	if err != nil || len(matches) != 1 {
		return "", fmt.Errorf("no acoustic model in voice bundle %s", dir)
	}
	return matches[0], nil
}

// wavToMp3 transcodes with ffmpeg; anything missing or failing yields an
// error so callers fall back to the WAV bytes.
func wavToMp3(ctx context.Context, wavPath string) ([]byte, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, err
	}
	tctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(tctx, ffmpeg, "-y", "-v", "error", "-i", wavPath, "-codec:a", "libmp3lame", "-f", "mp3", "pipe:1")
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	if out.Len() == 0 {
		return nil, fmt.Errorf("empty mp3 output")
	}
	return out.Bytes(), nil
}

// FuncSynthesizer adapts a synthesis func to the interface (edge-tts path).
type FuncSynthesizer func(ctx context.Context, text string) ([]byte, string, error)

// Synthesize implements SpeechSynthesizer.
func (f FuncSynthesizer) Synthesize(ctx context.Context, text string) ([]byte, string, error) {
	return f(ctx, text)
}

// AutoSynthesizer tries the primary (local) engine first and falls back
// per call, so a broken provision heals to cloud speech instead of silence.
type AutoSynthesizer struct {
	primary  SpeechSynthesizer
	fallback SpeechSynthesizer
}

// NewAutoSynthesizer wraps a primary and a fallback. Either may be nil.
func NewAutoSynthesizer(primary, fallback SpeechSynthesizer) *AutoSynthesizer {
	return &AutoSynthesizer{primary: primary, fallback: fallback}
}

// Available reports whether either engine is present.
func (a *AutoSynthesizer) Available() bool {
	if a == nil {
		return false
	}
	return a.primary != nil || a.fallback != nil
}

// Synthesize uses the primary engine, falling back on any failure.
// Text that cleans to nothing errors immediately: falling back would
// speak the raw markdown the cleanup just removed.
func (a *AutoSynthesizer) Synthesize(ctx context.Context, text string) ([]byte, string, error) {
	if PrepareSpeechText(text) == "" {
		return nil, "", fmt.Errorf("nothing to speak after cleanup")
	}
	if a.primary != nil {
		if audio, mime, err := a.primary.Synthesize(ctx, text); err == nil {
			return audio, mime, nil
		}
	}
	if a.fallback != nil {
		return a.fallback.Synthesize(ctx, text)
	}
	return nil, "", fmt.Errorf("speech synthesis unavailable")
}

// SelectSynthesizer picks the synthesizer for an engine preference:
// local-first with cloud fallback by default ("auto"), a single engine
// when pinned, nil when off or when nothing is available.
func SelectSynthesizer(cfg SynthConfig) SpeechSynthesizer {
	engine := NormalizeTTSEngine(cfg.Engine)
	bin := synthBinPath(cfg.BinPath)
	voiceDir := synthVoiceDir(cfg.VoiceDir)
	var local SpeechSynthesizer
	if LocalSynthAvailable(bin, cfg.VoiceDir) {
		local = NewLocalSynthesizer(bin, voiceDir, cfg.Speed)
	}
	var edge SpeechSynthesizer
	if _, err := exec.LookPath("edge-tts"); err == nil {
		edge = FuncSynthesizer(edgeTTSSpeak)
	}
	switch engine {
	case TTSEngineOff:
		return nil
	case TTSEngineLocal:
		return local
	case TTSEngineEdge:
		return edge
	default:
		if local == nil {
			return edge
		}
		if edge == nil {
			return local
		}
		return NewAutoSynthesizer(local, edge)
	}
}

// DescribeSynthesizer names the engine for logs and status output.
func DescribeSynthesizer(s SpeechSynthesizer) string {
	switch s.(type) {
	case *AutoSynthesizer:
		return "local-first (edge fallback)"
	case *LocalSynthesizer:
		return "local"
	case FuncSynthesizer:
		return "edge-tts"
	default:
		return "none"
	}
}

var (
	markdownFenceRe  = regexp.MustCompile("(?s)```.*?```")
	markdownLinkRe   = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	markdownInlineRe = regexp.MustCompile("`([^`]*)`")
	markdownMarkRe   = regexp.MustCompile(`(?m)^\s{0,3}(#{1,6}\s+|>+\s*|[-*+]\s+|\d+[.)]\s+)`)
	markdownStrongRe = regexp.MustCompile(`\*\*([^*]+)\*\*|__([^_]+)__`)
	markdownEmRe     = regexp.MustCompile(`(^|[\s(>])\*(\S(?:[^*]*\S)?)\*($|[\s).,!?;:])|(^|[\s(>])_(\S(?:[^_]*\S)?)_($|[\s).,!?;:])`)
	spaceCollapseRe  = regexp.MustCompile(`[ \t]+`)
	blankLinesRe     = regexp.MustCompile(`\n{3,}`)
)

// PrepareSpeechText strips markdown and control characters so the spoken
// reply sounds like speech, not markup. The visible text reply is
// untouched; this only shapes what gets read aloud.
func PrepareSpeechText(text string) string {
	s := markdownFenceRe.ReplaceAllString(text, " ")
	s = markdownLinkRe.ReplaceAllString(s, "$1")
	s = markdownInlineRe.ReplaceAllString(s, "$1")
	s = markdownStrongRe.ReplaceAllString(s, "$1$2")
	s = markdownEmRe.ReplaceAllString(s, "$1$2$3$4$5$6")
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		ln = markdownMarkRe.ReplaceAllString(ln, "")
		lines[i] = spaceCollapseRe.ReplaceAllString(strings.TrimSpace(ln), " ")
	}
	s = strings.Join(lines, "\n")
	s = blankLinesRe.ReplaceAllString(strings.TrimSpace(s), "\n\n")
	return s
}
