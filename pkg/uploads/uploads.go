// Package uploads is where files an owner sends to Ghost live.
//
// An attachment used to become an extensionless temp file that the file tools
// then refused to open (outside the workspace) and that was deleted seconds
// after the turn. Uploads now live in the workspace, keep their real name and
// a detected type, can be listed, and are deleted on request (or after the
// retention window). Ghost opens them with the ordinary workspace-confined
// file tools, so nothing about reading an upload widens what Ghost can reach.
package uploads

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// MaxBytes is the largest single upload. Generous for documents and photos,
// small enough that a base64 body cannot exhaust a Pod's memory.
const MaxBytes = 25 << 20

// DefaultRetention is how long an upload is kept unless the owner deletes it
// first. Long enough that "that PDF from last week" still works.
const DefaultRetention = 30 * 24 * time.Hour

// Item describes one stored upload.
type Item struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	MIME      string    `json:"mime"`
	Size      int64     `json:"size"`
	Path      string    `json:"path"` // workspace-relative, what file tools take
	Source    string    `json:"source,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

var idRe = regexp.MustCompile(`^[0-9a-f]{12}$`)

// Dir is the uploads root inside a workspace.
func Dir(workspace string) string { return filepath.Join(workspace, "uploads") }

// Save stores data as a new upload.
func Save(workspace string, data []byte, name, hintMIME, source string) (Item, error) {
	if len(data) == 0 {
		return Item{}, errors.New("the file is empty")
	}
	if len(data) > MaxBytes {
		return Item{}, fmt.Errorf("the file is %d MB; the limit is %d MB", len(data)>>20, MaxBytes>>20)
	}
	mime := Sniff(data, name, hintMIME)
	id := newID()
	safe := safeName(name, mime)
	dir := filepath.Join(Dir(workspace), id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return Item{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, safe), data, 0600); err != nil {
		os.RemoveAll(dir)
		return Item{}, err
	}
	it := Item{ID: id, Name: safe, Kind: Kind(safe, mime), MIME: mime, Size: int64(len(data)),
		Path: filepath.ToSlash(filepath.Join("uploads", id, safe)), Source: source, CreatedAt: time.Now().UTC()}
	meta, _ := json.Marshal(it)
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), meta, 0600); err != nil {
		os.RemoveAll(dir)
		return Item{}, err
	}
	return it, nil
}

// Import copies a local file (the terminal's /attach) into the store.
func Import(workspace, src, source string) (Item, error) {
	f, err := os.Open(src)
	if err != nil {
		return Item{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Item{}, err
	}
	if st.IsDir() {
		return Item{}, errors.New("that is a folder, not a file")
	}
	if st.Size() > MaxBytes {
		return Item{}, fmt.Errorf("the file is %d MB; the limit is %d MB", st.Size()>>20, MaxBytes>>20)
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil {
		return Item{}, err
	}
	return Save(workspace, data, filepath.Base(src), "", source)
}

// List returns stored uploads, newest first.
func List(workspace string) []Item {
	entries, err := os.ReadDir(Dir(workspace))
	if err != nil {
		return nil
	}
	var out []Item
	for _, e := range entries {
		if !e.IsDir() || !idRe.MatchString(e.Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(Dir(workspace), e.Name(), "meta.json"))
		if err != nil {
			continue
		}
		var it Item
		if json.Unmarshal(b, &it) == nil && it.ID == e.Name() {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// Delete removes one upload and everything stored with it.
func Delete(workspace, id string) error {
	if !idRe.MatchString(id) {
		return errors.New("not an upload id")
	}
	dir := filepath.Join(Dir(workspace), id)
	if _, err := os.Stat(dir); err != nil {
		return errors.New("no such upload")
	}
	return os.RemoveAll(dir)
}

// Purge deletes uploads older than maxAge and returns how many went.
func Purge(workspace string, maxAge time.Duration) int {
	n := 0
	for _, it := range List(workspace) {
		if time.Since(it.CreatedAt) > maxAge && Delete(workspace, it.ID) == nil {
			n++
		}
	}
	return n
}

// IsStored reports whether path lies inside the uploads root (absolute or
// workspace-relative), so a turn's cleanup never deletes an owner's file.
func IsStored(workspace, path string) bool {
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(workspace, path)
	}
	root := Dir(workspace) + string(os.PathSeparator)
	return strings.HasPrefix(filepath.Clean(abs)+string(os.PathSeparator), root)
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

var badName = regexp.MustCompile(`[^A-Za-z0-9._ \-()]`)

var mimeExt = map[string]string{
	"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp",
	"application/pdf": ".pdf", "text/plain": ".txt", "text/csv": ".csv", "application/json": ".json",
	"audio/mpeg": ".mp3", "audio/wav": ".wav", "video/mp4": ".mp4",
}

// safeName keeps the owner's file name recognisable while making it harmless
// as a path: no directories, no hidden files, only tame characters, and a
// real extension.
func safeName(name, mime string) string {
	base := filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	base = strings.TrimSpace(badName.ReplaceAllString(base, "_"))
	base = strings.TrimLeft(base, ".")
	if base == "" || base == "_" {
		base = "file"
	}
	if len(base) > 100 {
		ext := filepath.Ext(base)
		if len(ext) > 10 {
			ext = ""
		}
		base = base[:100-len(ext)] + ext
	}
	if filepath.Ext(base) == "" {
		base += mimeExt[mime]
	}
	return base
}

var extMIME = map[string]string{
	".pdf": "application/pdf", ".csv": "text/csv", ".md": "text/markdown", ".json": "application/json",
	".txt": "text/plain", ".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".odt":  "application/vnd.oasis.opendocument.text", ".rtf": "application/rtf",
	".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".wav": "audio/wav", ".ogg": "audio/ogg",
	".mp4": "video/mp4", ".mov": "video/quicktime", ".zip": "application/zip",
}

// Sniff decides a file's type from its bytes first, its name second, and the
// sender's claim last: a client's content-type is a hint, never the truth.
func Sniff(data []byte, name, hint string) string {
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	m := http.DetectContentType(head)
	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case m == "application/zip" || m == "application/octet-stream" || strings.HasPrefix(m, "text/plain"):
		if byName, ok := extMIME[ext]; ok {
			return byName
		}
	}
	if m == "application/octet-stream" && hint != "" && !strings.Contains(hint, "octet-stream") {
		return strings.ToLower(strings.TrimSpace(strings.SplitN(hint, ";", 2)[0]))
	}
	return strings.TrimSpace(strings.SplitN(m, ";", 2)[0])
}

// Kind groups a type for the UI and for choosing how Ghost should read it.
func Kind(name, mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	case mime == "application/pdf", strings.Contains(mime, "wordprocessingml"), strings.Contains(mime, "opendocument.text"), mime == "application/rtf", strings.Contains(mime, "presentationml"):
		return "document"
	case mime == "text/csv", strings.Contains(mime, "spreadsheetml"):
		return "spreadsheet"
	case strings.HasPrefix(mime, "text/"), mime == "application/json":
		return "text"
	case mime == "application/zip":
		return "archive"
	}
	return "other"
}
