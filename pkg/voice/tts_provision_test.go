package voice

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTTSAssetURL(t *testing.T) {
	url, err := ttsAssetURL("v1.13.6", "linux", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.6/sherpa-onnx-v1.13.6-linux-aarch64-static.tar.bz2"
	if url != want {
		t.Fatalf("got %q want %q", url, want)
	}
	url, err = ttsAssetURL("v1.13.6", "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	want = "https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.6/sherpa-onnx-v1.13.6-linux-x64-static.tar.bz2"
	if url != want {
		t.Fatalf("got %q want %q", url, want)
	}
	if _, err := ttsAssetURL("v1.13.6", "darwin", "arm64"); err == nil {
		t.Fatal("unsupported os must error")
	}
	if _, err := ttsAssetURL("v1.13.6", "linux", "riscv64"); err == nil {
		t.Fatal("unsupported arch must error")
	}
	if got := ttsVoiceURL(); got == "" || !strings.Contains(got, TTSVoiceBundle) {
		t.Fatalf("voice URL wrong: %q", got)
	}
}

func writeTestTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(content))})
		_, _ = tw.Write([]byte(content))
	}
	_ = tw.Close()
	_ = gz.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestExtractDir(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "v.tar.gz")
	writeTestTarGz(t, archive, map[string]string{
		"bundle/model.onnx":   "model",
		"bundle/tokens.txt":   "tokens",
		"bundle/sub/deep.txt": "deep",
	})
	dest := filepath.Join(dir, "out")
	if err := extractDir(archive, dest); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"bundle/model.onnx", "bundle/tokens.txt", "bundle/sub/deep.txt"} {
		if _, err := os.Stat(filepath.Join(dest, want)); err != nil {
			t.Fatalf("missing %s: %v", want, err)
		}
	}
}

func TestExtractDirRejectsTraversal(t *testing.T) {
	for _, evil := range []string{"../evil.txt", "/abs.txt", "ok/../../evil.txt"} {
		dir := t.TempDir()
		archive := filepath.Join(dir, "v.tar.gz")
		writeTestTarGz(t, archive, map[string]string{evil: "x"})
		if err := extractDir(archive, filepath.Join(dir, "out")); err == nil {
			t.Fatalf("must refuse %q", evil)
		}
	}
}
