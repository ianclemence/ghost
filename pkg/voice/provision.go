package voice

// Provisioning for the local speech-to-text sidecar: a pinned
// whisper.cpp release (prebuilt whisper-server binary) plus a ggml
// Whisper model, both fetched once and installed idempotently. The
// running service always loads ActiveModelLink, so switching models is
// a repointed symlink, never a config edit.

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// WhisperVersion pins the whisper.cpp release the sidecar comes from.
	// The ubuntu bin tarballs ship a ready-to-run whisper-server.
	WhisperVersion = "v1.8.7"
	// DefaultSTTModel is the ggml model provisioned when none is chosen.
	DefaultSTTModel = "base.en"
	// ActiveModelLink is the stable path the service loads; it symlinks
	// to whichever downloaded model is active.
	ActiveModelLink = "active.ggml.bin"
	// SidecarBinary is the installed whisper-server executable name.
	SidecarBinary = "whisper-server"
	// DefaultModelsDir is where speech models live on the appliance.
	DefaultModelsDir = "/var/ghost/models"
	// DefaultSidecarBinDir is where the sidecar binary is installed.
	DefaultSidecarBinDir = "/usr/local/bin"
)

// sttModels maps model names to their canonical ggml download URLs.
var sttModels = map[string]string{
	"tiny.en":  "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-tiny.en.bin",
	"tiny":     "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-tiny.bin",
	"base.en":  "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin",
	"base":     "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.bin",
	"small.en": "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.en.bin",
	"small":    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.bin",
}

// ModelFileName returns the on-disk filename for a model name.
func ModelFileName(model string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(model))
	name = strings.TrimSuffix(name, ".bin")
	if _, ok := sttModels[name]; !ok {
		return "", fmt.Errorf("unknown speech model %q (try tiny.en, base.en, or small.en)", model)
	}
	return "ggml-" + name + ".bin", nil
}

// sidecarAssetURL returns the release tarball URL holding whisper-server
// for this platform. Only linux builds are published upstream.
func sidecarAssetURL(version, goos, goarch string) (string, error) {
	if goos != "linux" {
		return "", fmt.Errorf("no prebuilt whisper-server for %s; build whisper.cpp from source", goos)
	}
	var asset string
	switch goarch {
	case "arm64":
		asset = "whisper-bin-ubuntu-arm64.tar.gz"
	case "amd64":
		asset = "whisper-bin-ubuntu-x64.tar.gz"
	default:
		return "", fmt.Errorf("no prebuilt whisper-server for linux/%s; build whisper.cpp from source", goarch)
	}
	return fmt.Sprintf("https://github.com/ggml-org/whisper.cpp/releases/download/%s/%s", version, asset), nil
}

// EnsureSidecar installs the whisper-server binary into binDir unless it
// is already there (force reinstalls). Returns the installed path.
func EnsureSidecar(binDir, version string, force bool) (string, error) {
	if binDir == "" {
		binDir = DefaultSidecarBinDir
	}
	if version == "" {
		version = WhisperVersion
	}
	dest := filepath.Join(binDir, SidecarBinary)
	if !force {
		if st, err := os.Stat(dest); err == nil && !st.IsDir() && st.Mode()&0111 != 0 {
			return dest, nil
		}
	}
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return "", err
	}
	url, err := sidecarAssetURL(version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	tmp, err := downloadToTemp(url, binDir)
	if err != nil {
		return "", fmt.Errorf("download whisper-server: %w", err)
	}
	defer os.Remove(tmp)
	if err := extractBinary(tmp, SidecarBinary, dest); err != nil {
		return "", fmt.Errorf("install whisper-server: %w", err)
	}
	return dest, nil
}

// EnsureModel downloads a ggml model into modelsDir unless present
// (force redownloads). Returns the model file path.
func EnsureModel(modelsDir, model string, force bool) (string, error) {
	if modelsDir == "" {
		modelsDir = DefaultModelsDir
	}
	if strings.TrimSpace(model) == "" {
		model = DefaultSTTModel
	}
	file, err := ModelFileName(model)
	if err != nil {
		return "", err
	}
	name := strings.ToLower(strings.TrimSpace(model))
	name = strings.TrimSuffix(name, ".bin")
	dest := filepath.Join(modelsDir, file)
	if !force {
		if st, err := os.Stat(dest); err == nil && !st.IsDir() && st.Size() > 0 {
			return dest, nil
		}
	}
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		return "", err
	}
	tmp, err := downloadToTemp(sttModels[name], modelsDir)
	if err != nil {
		return "", fmt.Errorf("download speech model %s: %w", name, err)
	}
	defer os.Remove(tmp)
	if err := os.Rename(tmp, dest); err != nil {
		return "", err
	}
	// tmp was created 0600; models only need to be readable.
	_ = os.Chmod(dest, 0644)
	return dest, nil
}

// PointActiveModel repoints the stable active-model symlink at an
// already-downloaded model file.
func PointActiveModel(modelsDir, model string) error {
	if modelsDir == "" {
		modelsDir = DefaultModelsDir
	}
	file, err := ModelFileName(model)
	if err != nil {
		return err
	}
	if st, err := os.Stat(filepath.Join(modelsDir, file)); err != nil || st.IsDir() || st.Size() == 0 {
		return fmt.Errorf("model %s is not downloaded yet", file)
	}
	link := filepath.Join(modelsDir, ActiveModelLink)
	_ = os.Remove(link)
	// Relative target keeps the directory relocatable.
	return os.Symlink(file, link)
}

// downloadToTemp fetches url into a temp file inside dir (same
// filesystem, so callers can atomically rename it into place).
func downloadToTemp(url, dir string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	tmp, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	wrote, err := io.Copy(tmp, resp.Body)
	_ = tmp.Close()
	if err != nil {
		os.Remove(tmpName)
		return "", err
	}
	if wrote == 0 {
		os.Remove(tmpName)
		return "", fmt.Errorf("empty download")
	}
	return tmpName, nil
}

// extractBinary pulls the single named executable out of a .tar.gz.
func extractBinary(archive, name, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != name {
			continue
		}
		out, err := os.OpenFile(dest+".new", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, tr)
		closeErr := out.Close()
		if copyErr != nil {
			os.Remove(dest + ".new")
			return copyErr
		}
		if closeErr != nil {
			os.Remove(dest + ".new")
			return closeErr
		}
		return os.Rename(dest+".new", dest)
	}
	return fmt.Errorf("%s not found in archive", name)
}
