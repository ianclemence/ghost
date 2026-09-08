package voice

// Provisioning for local speech synthesis: a pinned sherpa-onnx static
// build (prebuilt sherpa-onnx-offline-tts binary) plus a Piper voice
// bundle, both fetched once and installed idempotently. No daemon, no
// resident RAM: each utterance spawns the engine, synthesizes, and exits.

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// SherpaVersion pins the sherpa-onnx release the TTS engine comes from.
	SherpaVersion = "v1.13.6"
	// TTSBinName is the engine executable extracted from the bundle.
	TTSBinName = "sherpa-onnx-offline-tts"
	// TTSVoiceBundle is the provisioned voice (Piper, single speaker).
	// lessac-medium is the warm, clear default used across the local
	// assistant ecosystem; amy-low is the fallback if latency demands it.
	TTSVoiceBundle = "vits-piper-en_US-lessac-medium"

	// Pinned sha256 digests captured at provisioning time from the pinned
	// upstream releases; a fetch that is tampered with or corrupted in
	// transit fails closed instead of running an unexpected binary.
	sherpaOfflineTTSSHA256ARM64 = "38a085528ab825ed422b16c2cd3afc58381292bd882fda2c542854d3101da242"

	// lessacModelFileName is the acoustic model file inside the voice
	// bundle; its digest is what EnsureTTSVoice verifies.
	lessacModelFileName = "en_US-lessac-medium.onnx"
	lessacModelSHA256   = "4ba07d8549906668ee855fd9abf9faf66c5db74742712ff026a159f7277fca9f"
)

// ttsEngineSHA256 returns the pinned engine digest for a platform, or ""
// when none is pinned yet (verification is then skipped).
func ttsEngineSHA256(goarch string) string {
	if goarch == "arm64" {
		return sherpaOfflineTTSSHA256ARM64
	}
	return ""
}

// ttsAssetURL returns the static sherpa-onnx tarball URL for this
// platform. Only linux builds are published upstream.
func ttsAssetURL(version, goos, goarch string) (string, error) {
	if goos != "linux" {
		return "", fmt.Errorf("no prebuilt TTS engine for %s; install sherpa-onnx manually", goos)
	}
	var asset string
	switch goarch {
	case "arm64":
		asset = "sherpa-onnx-" + version + "-linux-aarch64-static.tar.bz2"
	case "amd64":
		asset = "sherpa-onnx-" + version + "-linux-x64-static.tar.bz2"
	default:
		return "", fmt.Errorf("no prebuilt TTS engine for linux/%s; install sherpa-onnx manually", goarch)
	}
	return fmt.Sprintf("https://github.com/k2-fsa/sherpa-onnx/releases/download/%s/%s", version, asset), nil
}

// ttsVoiceURL returns the voice bundle download URL.
func ttsVoiceURL() string {
	return "https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/" + TTSVoiceBundle + ".tar.bz2"
}

// EnsureTTSBin installs the synthesis engine binary into binDir unless it
// is already there (force reinstalls). Returns the installed path.
func EnsureTTSBin(binDir string, force bool) (string, error) {
	if binDir == "" {
		binDir = DefaultSidecarBinDir
	}
	dest := filepath.Join(binDir, TTSBinName)
	if !force {
		if st, err := os.Stat(dest); err == nil && !st.IsDir() && st.Mode()&0111 != 0 {
			return dest, verifySHA256(dest, ttsEngineSHA256(runtime.GOARCH))
		}
	}
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return "", err
	}
	url, err := ttsAssetURL(SherpaVersion, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	// The static bundle is large (hundreds of MB); the archive is removed
	// right after the one needed binary is extracted.
	archive, err := downloadToTemp(url, binDir)
	if err != nil {
		return "", fmt.Errorf("download TTS engine: %w", err)
	}
	defer os.Remove(archive)
	if err := extractBinary(archive, TTSBinName, dest); err != nil {
		return "", fmt.Errorf("install TTS engine: %w", err)
	}
	return dest, verifySHA256(dest, ttsEngineSHA256(runtime.GOARCH))
}

// EnsureTTSVoice downloads the voice bundle into modelsDir/tts unless
// present (force redownloads) and points the active-tts symlink at it.
// Returns the bundle directory.
func EnsureTTSVoice(modelsDir string, force bool) (string, error) {
	if strings.TrimSpace(modelsDir) == "" {
		modelsDir = DefaultTTSModelsDir
	}
	bundleDir := filepath.Join(modelsDir, TTSVoiceBundle)
	verify := func() error {
		mf, err := voiceModelFile(bundleDir)
		if err != nil {
			return err
		}
		expected := ""
		if filepath.Base(mf) == lessacModelFileName {
			expected = lessacModelSHA256
		}
		return verifySHA256(mf, expected)
	}
	if !force {
		if _, err := voiceModelFile(bundleDir); err == nil {
			if err := verify(); err != nil {
				return "", err
			}
			return bundleDir, pointActiveTTSVoice(modelsDir)
		}
	}
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		return "", err
	}
	archive, err := downloadToTemp(ttsVoiceURL(), modelsDir)
	if err != nil {
		return "", fmt.Errorf("download voice bundle: %w", err)
	}
	defer os.Remove(archive)
	if err := extractDir(archive, modelsDir); err != nil {
		return "", fmt.Errorf("install voice bundle: %w", err)
	}
	if _, err := voiceModelFile(bundleDir); err != nil {
		return "", fmt.Errorf("voice bundle incomplete: %w", err)
	}
	if err := verify(); err != nil {
		return "", err
	}
	return bundleDir, pointActiveTTSVoice(modelsDir)
}

// pointActiveTTSVoice repoints the stable active-tts symlink at the
// provisioned bundle (relative target keeps the tree relocatable).
func pointActiveTTSVoice(modelsDir string) error {
	link := filepath.Join(modelsDir, ActiveTTSVoiceLink)
	_ = os.Remove(link)
	return os.Symlink(TTSVoiceBundle, link)
}

// extractDir unpacks a .tar.gz/.tar.bz2 archive under destDir, refusing
// absolute paths and directory traversal. Callers download with
// downloadToTemp, which only produces .tar.gz — but release assets may
// use either compression, so both are accepted.
func extractDir(archive, destDir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	tr, closeTar, err := tarReader(f)
	if err != nil {
		return err
	}
	defer closeTar()
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(hdr.Name)
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
			return fmt.Errorf("refusing unsafe archive path %q", hdr.Name)
		}
		target := filepath.Join(destDir, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, tr)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
	return nil
}
