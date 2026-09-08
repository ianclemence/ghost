package main

// Local speech-to-text provisioning: installs the whisper-server
// sidecar binary and ggml model, then points the stable active-model
// symlink at the chosen model. Idempotent — reruns skip work that is
// already done unless --force is given.

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ianclemence/ghost/pkg/voice"
)

func sttCmd() {
	if len(os.Args) < 3 {
		sttHelp()
		return
	}
	switch os.Args[2] {
	case "setup":
		sttSetupCmd(os.Args[3:])
	case "status":
		sttStatusCmd()
	default:
		fmt.Printf("Unknown stt command: %s\n", os.Args[2])
		sttHelp()
	}
}

func sttHelp() {
	fmt.Println("Usage: ghost stt <command>")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  setup    Install the whisper-server sidecar and speech model")
	fmt.Println("           [--model tiny.en|base.en|small.en] [--force]")
	fmt.Println("           [--models-dir DIR] [--bin-dir DIR]")
	fmt.Println("  status   Show local speech-to-text readiness")
}

func sttSetupCmd(args []string) {
	fs := flag.NewFlagSet("stt setup", flag.ContinueOnError)
	model := fs.String("model", voice.DefaultSTTModel, "ggml speech model to provision")
	force := fs.Bool("force", false, "redownload even if already installed")
	modelsDir := fs.String("models-dir", voice.DefaultModelsDir, "directory for speech models")
	binDir := fs.String("bin-dir", voice.DefaultSidecarBinDir, "directory for the sidecar binary")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	bin, err := voice.EnsureSidecar(*binDir, "", *force)
	if err != nil {
		fmt.Printf("Sidecar unavailable: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Sidecar ready: %s\n", bin)

	path, err := voice.EnsureModel(*modelsDir, *model, *force)
	if err != nil {
		fmt.Printf("Model unavailable: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Model ready: %s\n", path)

	if err := voice.PointActiveModel(*modelsDir, *model); err != nil {
		fmt.Printf("Could not activate model: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Active model: %s\n", filepath.Join(*modelsDir, voice.ActiveModelLink))
	fmt.Println("Local speech-to-text provisioned.")
}

func sttStatusCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Engine preference: %s\n", voice.NormalizeEngine(cfg.STT.Engine))

	bin := filepath.Join(voice.DefaultSidecarBinDir, voice.SidecarBinary)
	if st, err := os.Stat(bin); err == nil && !st.IsDir() {
		fmt.Printf("Sidecar binary: %s ✓\n", bin)
	} else {
		fmt.Printf("Sidecar binary: %s ✗ (run: sudo ghost stt setup)\n", bin)
	}

	link := filepath.Join(voice.DefaultModelsDir, voice.ActiveModelLink)
	if target, err := os.Readlink(link); err == nil {
		fmt.Printf("Active model: %s ✓\n", target)
	} else {
		fmt.Printf("Active model: none ✗ (run: sudo ghost stt setup)\n")
	}

	if _, err := exec.LookPath("ffmpeg"); err == nil {
		fmt.Println("ffmpeg (voice-note conversion): ✓")
	} else {
		fmt.Println("ffmpeg (voice-note conversion): ✗ (install: sudo apt-get install -y ffmpeg)")
	}

	local := voice.NewLocalTranscriber(voice.LocalBaseURL(cfg.STT.Port))
	if local.IsAvailable() {
		fmt.Printf("Sidecar answering on %s ✓\n", voice.LocalBaseURL(cfg.STT.Port))
	} else {
		fmt.Printf("Sidecar answering on %s ✗ (is ghost-stt running?)\n", voice.LocalBaseURL(cfg.STT.Port))
	}

	tr := voice.SelectTranscriber(voice.SelectConfig{
		Engine:      cfg.STT.Engine,
		LocalURL:    voice.LocalBaseURL(cfg.STT.Port),
		MoonshotKey: cfg.Providers.Moonshot.APIKey,
		GroqKey:     cfg.Providers.Groq.APIKey,
	})
	fmt.Printf("Effective engine: %s\n", voice.DescribeTranscriber(tr))
}
