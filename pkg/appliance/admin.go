package appliance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	// AdminHashFile is the file that stores the bcrypt hash of the admin
	// password. Kept separate from .env and config.json for security.
	AdminHashFile = "admin.hash"
	// AdminMetaFile stores admin metadata (creation time, last password change).
	AdminMetaFile = "admin.meta"
	// BcryptCost is the work factor used when hashing the admin password.
	BcryptCost = 12
)

// AdminMeta holds metadata about the admin credential.
type AdminMeta struct {
	CreatedAt   time.Time `json:"created_at"`
	LastChanged time.Time `json:"last_changed"`
}

// AdminHashPath returns the path to the admin password hash file.
func AdminHashPath(ghostDir string) string {
	return filepath.Join(ghostDir, "data", AdminHashFile)
}

// AdminMetaPath returns the path to the admin metadata file.
func AdminMetaPath(ghostDir string) string {
	return filepath.Join(ghostDir, "data", AdminMetaFile)
}

// LoadAdminMeta reads the admin metadata from disk. Returns nil if not found.
func LoadAdminMeta(ghostDir string) *AdminMeta {
	data, err := os.ReadFile(AdminMetaPath(ghostDir))
	if err != nil {
		return nil
	}
	var meta AdminMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil
	}
	return &meta
}

// saveAdminMeta writes admin metadata to disk atomically.
func saveAdminMeta(ghostDir string, meta *AdminMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	path := AdminMetaPath(ghostDir)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".admin-meta-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// RecordAdminCreated records the initial creation time of the admin credential.
func RecordAdminCreated(ghostDir string) error {
	meta := LoadAdminMeta(ghostDir)
	if meta == nil {
		meta = &AdminMeta{}
	}
	now := time.Now().UTC()
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	meta.LastChanged = now
	return saveAdminMeta(ghostDir, meta)
}

// RecordPasswordChanged records when the admin password was last changed.
func RecordPasswordChanged(ghostDir string) error {
	meta := LoadAdminMeta(ghostDir)
	if meta == nil {
		meta = &AdminMeta{}
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now().UTC()
	}
	meta.LastChanged = time.Now().UTC()
	return saveAdminMeta(ghostDir, meta)
}

// SetAdminPassword validates, hashes the given password with bcrypt, and writes
// it to the admin hash file. Records creation timestamp if this is the first
// password set. The write is atomic (temp file + rename) so a crash never
// leaves a truncated hash.
func SetAdminPassword(ghostDir, password string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return err
	}

	path := AdminHashPath(ghostDir)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".admin-hash-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(hash); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		return err
	}

	// Record timestamps: creation on first set, changed on subsequent sets.
	if LoadAdminMeta(ghostDir) == nil {
		return RecordAdminCreated(ghostDir)
	}
	return RecordPasswordChanged(ghostDir)
}

// VerifyAdminPassword reports whether the given password matches the stored
// admin hash. Returns false if no admin password has been configured yet.
func VerifyAdminPassword(ghostDir, password string) (bool, error) {
	path := AdminHashPath(ghostDir)
	hash, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	if err := bcrypt.CompareHashAndPassword(hash, []byte(password)); err != nil {
		return false, nil
	}
	return true, nil
}

// AdminConfigured reports whether an admin password hash exists on disk.
func AdminConfigured(ghostDir string) bool {
	_, err := os.Stat(AdminHashPath(ghostDir))
	return err == nil
}

// RemoveAdminPassword deletes the admin credential and its metadata. It is
// used to force a fresh setup (factory reset) or to clear a forgotten
// password so the wizard can re-run. Returns nil if nothing was configured.
func RemoveAdminPassword(ghostDir string) error {
	errHash := os.Remove(AdminHashPath(ghostDir))
	if errHash != nil && !os.IsNotExist(errHash) {
		return errHash
	}
	errMeta := os.Remove(AdminMetaPath(ghostDir))
	if errMeta != nil && !os.IsNotExist(errMeta) {
		return errMeta
	}
	return nil
}
