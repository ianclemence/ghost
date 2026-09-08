package voice

// Provisioning for the local speech-to-text sidecar: a pinned
// whisper.cpp release (prebuilt whisper-server binary) plus a ggml
// Whisper model, both fetched once and installed idempotently. The
// running service always loads ActiveModelLink, so switching models is
// a repointed symlink, never a config edit.

import (
	"archive/tar"
	"compress/bzip2"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// verifySHA256 confirms a file's sha256 matches an expected digest. An
// empty expected digest means none is pinned yet and the check passes
// (a skip is deliberate, never a silent trust of a known-bad hash).
func verifySHA256(path, expected string) error {
	if expected == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s (re-run setup to re-download)", path, got, expected)
	}
	return nil
}

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

// Pinned sha256 digests for the artifacts Ghost ships by default. Digests
// are captured at provisioning time from the pinned upstream release, so a
// fetch that is tampered with or corrupted in transit fails closed instead
// of running an unexpected binary or model. Non-default models have no
// pinned digest yet and pass through at the same trust level as before
// (TLS to the pinned upstream path); add digests as the set stabilizes.
const (
	whisperServerSHA256ARM64 = "cc472696fddb8d9a66753c8f6b1c8a5690b4e23becabeaa17fd83e70fab9abf3"

	ggmlBaseEnSHA256 = "a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002"
	ggmlTinyEnSHA256 = "921e4cf8686fdd993dcd081a5da5b6c365bfde1162e72b08d75ac75289920b1f"
)

// sttModelDigests pins the models Ghost provisions by default (base.en)
// and documents as the low-RAM fallback (tiny.en).
var sttModelDigests = map[string]string{
	"base.en": ggmlBaseEnSHA256,
	"tiny.en": ggmlTinyEnSHA256,
}

// whisperServerSHA256 returns the pinned digest for a platform, or "" when
// none is pinned yet (verification is then skipped, never invented).
func whisperServerSHA256(goarch string) string {
	if goarch == "arm64" {
		return whisperServerSHA256ARM64
	}
	return ""
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
			return dest, verifySHA256(dest, whisperServerSHA256(runtime.GOARCH))
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
	return dest, verifySHA256(dest, whisperServerSHA256(runtime.GOARCH))
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
			return dest, verifySHA256(dest, sttModelDigests[name])
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
	return dest, verifySHA256(dest, sttModelDigests[name])
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

// tarReader opens a .tar.gz or .tar.bz2 archive, sniffing the
// compression from magic bytes (download temp files carry no suffix).
func tarReader(f *os.File) (*tar.Reader, func(), error) {
	noop := func() {}
	var magic [3]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return nil, noop, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, noop, err
	}
	if magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, noop, err
		}
		return tar.NewReader(gz), func() { gz.Close() }, nil
	}
	if magic[0] == 'B' && magic[1] == 'Z' && magic[2] == 'h' {
		return tar.NewReader(bzip2.NewReader(f)), noop, nil
	}
	return nil, noop, fmt.Errorf("unknown archive compression")
}

// extractBinary pulls the single named executable out of a .tar.gz or
// .tar.bz2 archive.
func extractBinary(archive, name, dest string) error {
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
