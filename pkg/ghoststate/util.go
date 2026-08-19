package ghoststate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
)

// writeFileAtomic writes data to path via temp file + rename with the given
// permissions, so a crash never leaves a truncated file.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create parent dir for %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(dir, ".ghost-state-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return os.Rename(tmpName, path)
}

// buildArchive packs manifest.json (first) followed by every manifest file
// into a gzip tar archive. staging maps logical archive paths to files on
// disk holding the exact bytes to embed.
func buildArchive(staging map[string]string, m *Manifest) ([]byte, error) {
	manifestData, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	entries := make([]string, 0, len(m.Files)+1)
	entries = append(entries, manifestArchiveLogical)
	for _, f := range m.Files {
		entries = append(entries, f.Path)
	}
	sort.Strings(entries[1:]) // manifest.json stays first, rest deterministic

	for _, name := range entries {
		var data []byte
		if name == manifestArchiveLogical {
			data = manifestData
		} else {
			src := staging[name]
			data, err = os.ReadFile(src)
			if err != nil {
				tw.Close()
				gz.Close()
				return nil, fmt.Errorf("read staged file %s: %w", name, err)
			}
		}
		mode := int64(0644)
		if name == manifestArchiveLogical || name == configSecretsLogical || name == configJSONLogical {
			mode = 0600
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data))}); err != nil {
			tw.Close()
			gz.Close()
			return nil, fmt.Errorf("write tar header for %s: %w", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			tw.Close()
			gz.Close()
			return nil, fmt.Errorf("write tar body for %s: %w", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		gz.Close()
		return nil, fmt.Errorf("close tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("close gzip: %w", err)
	}
	return buf.Bytes(), nil
}

// readArchive unpacks an archive and returns its manifest plus every embedded
// file by logical path. Paths are validated during extraction so a crafted
// archive can never escape the staging map.
func readArchive(plain []byte) (*Manifest, map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(plain))
	if err != nil {
		return nil, nil, fmt.Errorf("open archive: %w", err)
	}
	defer gz.Close()

	files := make(map[string][]byte)
	var m *Manifest
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		name := hdr.Name
		if name == manifestArchiveLogical {
			data := make([]byte, hdr.Size)
			if err := readFull(tr, data); err != nil {
				return nil, nil, fmt.Errorf("read manifest: %w", err)
			}
			var parsed Manifest
			if err := json.Unmarshal(data, &parsed); err != nil {
				return nil, nil, fmt.Errorf("parse manifest: %w", err)
			}
			m = &parsed
			continue
		}
		if err := validateLogicalPath(name); err != nil {
			return nil, nil, err
		}
		data := make([]byte, hdr.Size)
		if err := readFull(tr, data); err != nil {
			return nil, nil, fmt.Errorf("read file %s: %w", name, err)
		}
		files[name] = data
	}

	if m == nil {
		return nil, nil, fmt.Errorf("archive is missing %s", manifestArchiveLogical)
	}
	if err := m.Validate(); err != nil {
		return nil, nil, err
	}
	return m, files, nil
}

// readFull reads exactly len(dst) bytes from the tar entry. tar.Reader may
// signal the final read with (n, io.EOF) when n fills the buffer, which is a
// clean completion, not an error.
func readFull(r io.Reader, dst []byte) error {
	read := 0
	for read < len(dst) {
		n, err := r.Read(dst[read:])
		read += n
		if err != nil {
			if err == io.EOF && read == len(dst) {
				return nil
			}
			return err
		}
	}
	return nil
}

// validateLogicalPath rejects paths that could escape the workspace when an
// archive is imported on an untrusted machine.
func validateLogicalPath(name string) error {
	if name == "" || name[0] == '/' || name[0] == '.' && (len(name) == 1 || name[1] == '.' || name[1] == '/') {
		return fmt.Errorf("archive contains unsafe path %q", name)
	}
	cleaned := path.Clean(name)
	if cleaned == "." || cleaned == ".." || hasPrefixSegment(cleaned, "..") {
		return fmt.Errorf("archive contains unsafe path %q", name)
	}
	return nil
}

func hasPrefixSegment(p, prefix string) bool {
	return p == prefix || len(p) > len(prefix) && p[:len(prefix)+1] == prefix+"/"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
