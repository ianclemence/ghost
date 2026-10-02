package uploads

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveDetectsTypeKeepsNameAndStoresInWorkspace(t *testing.T) {
	ws := t.TempDir()
	pdf := []byte("%PDF-1.4\n%mock\n")
	it, err := Save(ws, pdf, "Lease agreement.pdf", "application/octet-stream", "mobile")
	if err != nil {
		t.Fatal(err)
	}
	if it.MIME != "application/pdf" || it.Kind != "document" || it.Name != "Lease agreement.pdf" {
		t.Fatalf("unexpected item: %+v", it)
	}
	if _, err := os.Stat(filepath.Join(ws, it.Path)); err != nil {
		t.Fatalf("file must be readable at its workspace-relative path: %v", err)
	}
	if !IsStored(ws, it.Path) || !IsStored(ws, filepath.Join(ws, it.Path)) {
		t.Fatal("IsStored must recognise both relative and absolute paths")
	}
}

func TestTheBytesOutvoteTheClientsClaim(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
	it, err := Save(t.TempDir(), png, "invoice.pdf", "application/pdf", "mobile")
	if err != nil {
		t.Fatal(err)
	}
	if it.Kind != "image" || it.MIME != "image/png" {
		t.Fatalf("a PNG named .pdf must be treated as an image, got %+v", it)
	}
}

func TestOfficeAndSpreadsheetFilesAreRecognisedByNameWhenSniffedAsZip(t *testing.T) {
	zip := []byte("PK\x03\x04rest-of-zip")
	for name, want := range map[string]string{"report.docx": "document", "budget.xlsx": "spreadsheet", "deck.pptx": "document", "data.csv": "spreadsheet", "bundle.zip": "archive"} {
		it, err := Save(t.TempDir(), zip, name, "", "t")
		if name == "data.csv" {
			it, err = Save(t.TempDir(), []byte("a,b\n1,2\n"), name, "", "t")
		}
		if err != nil || it.Kind != want {
			t.Errorf("%s: kind %q want %q (%v)", name, it.Kind, want, err)
		}
	}
}

func TestNamesCannotEscapeOrHide(t *testing.T) {
	ws := t.TempDir()
	for _, bad := range []string{"../../etc/passwd", "..\\..\\boot.ini", ".ssh_config", "a/b/c.txt", "", "\x00evil"} {
		it, err := Save(ws, []byte("hello world"), bad, "", "t")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(it.Name, "/") || strings.Contains(it.Name, "..") || strings.HasPrefix(it.Name, ".") {
			t.Errorf("%q stored as %q", bad, it.Name)
		}
		if !strings.HasPrefix(filepath.Join(ws, it.Path), Dir(ws)) {
			t.Errorf("%q escaped the uploads root: %s", bad, it.Path)
		}
	}
}

func TestLimitsAndEmptyFiles(t *testing.T) {
	ws := t.TempDir()
	if _, err := Save(ws, nil, "x.txt", "", "t"); err == nil {
		t.Fatal("an empty file must be refused")
	}
	if _, err := Save(ws, make([]byte, MaxBytes+1), "big.bin", "", "t"); err == nil {
		t.Fatal("an oversize file must be refused")
	}
}

func TestListDeleteAndPurge(t *testing.T) {
	ws := t.TempDir()
	a, _ := Save(ws, []byte("first file"), "a.txt", "", "t")
	time.Sleep(1100 * time.Millisecond)
	b, _ := Save(ws, []byte("second file"), "b.txt", "", "t")
	got := List(ws)
	if len(got) != 2 || got[0].ID != b.ID {
		t.Fatalf("list must be newest first: %+v", got)
	}
	if err := Delete(ws, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(Dir(ws), a.ID)); !os.IsNotExist(err) {
		t.Fatal("delete must remove the file and its metadata")
	}
	if err := Delete(ws, "../../etc"); err == nil {
		t.Fatal("a non-id must never reach the filesystem")
	}
	if n := Purge(ws, time.Nanosecond); n != 1 || len(List(ws)) != 0 {
		t.Fatalf("purge removed %d", n)
	}
}

func TestImportCopiesLocalFile(t *testing.T) {
	ws := t.TempDir()
	src := filepath.Join(t.TempDir(), "notes.md")
	os.WriteFile(src, []byte("# hello\n"), 0644)
	it, err := Import(ws, src, "terminal")
	if err != nil || it.Name != "notes.md" || it.Source != "terminal" {
		t.Fatalf("%+v %v", it, err)
	}
	if _, err := Import(ws, t.TempDir(), "terminal"); err == nil {
		t.Fatal("a folder must be refused")
	}
}

func TestThumbnailShrinksPhotosAndRefusesTheRest(t *testing.T) {
	ws := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 800, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{uint8(x / 4), uint8(y / 2), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	it, err := Save(ws, buf.Bytes(), "wide.png", "image/png", "test")
	if err != nil {
		t.Fatal(err)
	}
	data, err := Thumbnail(ws, it, 200)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("thumbnail must be a readable image: %v", err)
	}
	if b := got.Bounds(); b.Dx() != 200 || b.Dy() != 100 {
		t.Fatalf("an 800x400 photo at max 200 must be 200x100, got %dx%d", b.Dx(), b.Dy())
	}
	doc, err := Save(ws, []byte("%PDF-1.4 hello"), "a.pdf", "application/pdf", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Thumbnail(ws, doc, 200); !errors.Is(err, ErrNoThumbnail) {
		t.Fatalf("a document has no thumbnail, got %v", err)
	}
	it.Size = MaxThumbSource + 1
	if _, err := Thumbnail(ws, it, 200); !errors.Is(err, ErrNoThumbnail) {
		t.Fatalf("an oversize photo must be refused, got %v", err)
	}
}
