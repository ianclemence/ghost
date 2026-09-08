package main

// Local speech-synthesis provisioning: installs the sherpa TTS engine
// binary and the default voice bundle, then points the stable
// active-tts symlink at it. Idempotent — reruns skip work that is
// already done unless --force is given. No daemon: each utterance
// spawns the engine, synthesizes, and exits.

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ianclemence/ghost/pkg/voice"
)

func ttsCmd() {
	if len(os.Args) < 3 {
		ttsHelp()
		return
	}
	switch os.Args[2] {
	case "setup":
		ttsSetupCmd(os.Args[3:])
	case "status":
		ttsStatusCmd(os.Args[3:])
	default:
		fmt.Printf("Unknown tts command: %s\n", os.Args[2])
		ttsHelp()
	}
}

func ttsHelp() {
	fmt.Println("Usage: ghost tts <command>")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  setup    Install the TTS engine binary and default voice")
	fmt.Println("           [--force] [--models-dir DIR] [--bin-dir DIR]")
	fmt.Println("  status   Show local speech-synthesis readiness [--test]")
}

func ttsSetupCmd(args []string) {
	fs := flag.NewFlagSet("tts setup", flag.ContinueOnError)
	force := fs.Bool("force", false, "redownload even if already installed")
	modelsDir := fs.String("models-dir", voice.DefaultTTSModelsDir, "directory for TTS voices")
	binDir := fs.String("bin-dir", voice.DefaultSidecarBinDir, "directory for the TTS engine binary")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	bin, err := voice.EnsureTTSBin(*binDir, *force)
	if err != nil {
		fmt.Printf("TTS engine unavailable: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("TTS engine ready: %s\n", bin)

	bundle, err := voice.EnsureTTSVoice(*modelsDir, *force)
	if err != nil {
		fmt.Printf("Voice unavailable: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Voice ready: %s\n", bundle)
	fmt.Println("Local speech synthesis provisioned.")
}

func ttsStatusCmd(args []string) {
	fs := flag.NewFlagSet("tts status", flag.ContinueOnError)
	test := fs.Bool("test", false, "synthesize a short phrase to prove the loop")
	modelsDir := fs.String("models-dir", voice.DefaultTTSModelsDir, "directory holding TTS voices")
	binDir := fs.String("bin-dir", voice.DefaultSidecarBinDir, "directory holding the TTS engine binary")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Engine preference: %s\n", voice.NormalizeTTSEngine(cfg.TTS.Engine))

	bin := filepath.Join(*binDir, voice.TTSBinName)
	if st, err := os.Stat(bin); err == nil && !st.IsDir() {
		fmt.Printf("TTS engine binary: %s ✓\n", bin)
	} else {
		fmt.Printf("TTS engine binary: %s ✗ (run: sudo ghost tts setup)\n", bin)
	}

	link := filepath.Join(*modelsDir, voice.ActiveTTSVoiceLink)
	if target, err := os.Readlink(link); err == nil {
		fmt.Printf("Active voice: %s ✓\n", target)
	} else {
		fmt.Printf("Active voice: none ✗ (run: sudo ghost tts setup)\n")
	}

	if _, err := exec.LookPath("ffmpeg"); err == nil {
		fmt.Println("ffmpeg (mp3 delivery): ✓")
	} else {
		fmt.Println("ffmpeg (mp3 delivery): ✗ (WAV fallback will be used)")
	}
	if _, err := exec.LookPath("edge-tts"); err == nil {
		fmt.Println("edge-tts (cloud fallback): ✓")
	} else {
		fmt.Println("edge-tts (cloud fallback): ✗")
	}

	synth := voice.SelectSynthesizer(voice.SynthConfig{Engine: cfg.TTS.Engine, Speed: cfg.TTS.Speed, BinPath: bin, VoiceDir: *modelsDir})
	fmt.Printf("Effective engine: %s\n", voice.DescribeSynthesizer(synth))
	if synth == nil {
		return
	}
	if *test {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		start := time.Now()
		audio, mime, err := synth.Synthesize(ctx, "Ghost voice check.")
		if err != nil {
			fmt.Printf("Test synthesis failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Test synthesis ok: %d bytes, %s, took %s\n", len(audio), mime, time.Since(start).Round(time.Second))
	}
}
