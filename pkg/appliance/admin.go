package appliance

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/crypto/bcrypt"
)

const (
	// AdminHashFile is the file that stores the bcrypt hash of the admin
	// password. Kept separate from .env and config.json so the admin
	// credential and the API bridge secret never share a value.
	AdminHashFile = "admin.hash"
	// BcryptCost is the work factor used when hashing the admin password.
	BcryptCost = 12
)

// AdminHashPath returns the path to the admin password hash file.
func AdminHashPath(ghostDir string) string {
	return filepath.Join(ghostDir, "data", AdminHashFile)
}

// SetAdminPassword hashes the given password with bcrypt and writes it to
// the admin hash file. The write is atomic (temp file + rename) so a crash
// never leaves a truncated hash.
func SetAdminPassword(ghostDir, password string) error {
	if password == "" {
		return errors.New("admin password cannot be empty")
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

	return os.Rename(tmpName, path)
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
