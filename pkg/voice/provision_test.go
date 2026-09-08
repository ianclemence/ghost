package voice

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestModelFileName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"base.en", "ggml-base.en.bin"},
		{"tiny", "ggml-tiny.bin"},
		{"small.en.bin", "ggml-small.en.bin"},
		{" BASE.EN ", "ggml-base.en.bin"},
	} {
		got, err := ModelFileName(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("ModelFileName(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	if _, err := ModelFileName("large-v9"); err == nil {
		t.Fatal("unknown model must error")
	}
}

func TestSidecarAssetURL(t *testing.T) {
	url, err := sidecarAssetURL("v1.8.7", "linux", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://github.com/ggml-org/whisper.cpp/releases/download/v1.8.7/whisper-bin-ubuntu-arm64.tar.gz"
	if url != want {
		t.Fatalf("got %q want %q", url, want)
	}
	if _, err := sidecarAssetURL("v1.8.7", "linux", "riscv64"); err == nil {
		t.Fatal("unsupported arch must error")
	}
	if _, err := sidecarAssetURL("v1.8.7", "darwin", "arm64"); err == nil {
		t.Fatal("unsupported os must error")
	}
}

func TestPointActiveModel(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ggml-base.en.bin"), []byte("model"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := PointActiveModel(dir, "base.en"); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(dir, ActiveModelLink))
	if err != nil {
		t.Fatal(err)
	}
	if target != "ggml-base.en.bin" {
		t.Fatalf("symlink points at %q", target)
	}
	if err := PointActiveModel(dir, "tiny.en"); err == nil {
		t.Fatal("missing model must error")
	}
}

func TestExtractBinary(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "bins.tar.gz")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	payload := []byte("#!/bin/sh\necho hi\n")
	_ = tw.WriteHeader(&tar.Header{Name: "pkg/whisper-server", Mode: 0755, Size: int64(len(payload))})
	_, _ = tw.Write(payload)
	_ = tw.WriteHeader(&tar.Header{Name: "pkg/README", Mode: 0644, Size: 2})
	_, _ = tw.Write([]byte("hi"))
	_ = tw.Close()
	_ = gz.Close()
	if err := os.WriteFile(archive, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "whisper-server")
	if err := extractBinary(archive, "whisper-server", dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("wrong content %q", got)
	}
	if err := extractBinary(archive, "nope", dest); err == nil {
		t.Fatal("missing entry must error")
	}
}
