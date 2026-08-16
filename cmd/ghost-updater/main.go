package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"
)

var (
	version    = "dev"
	updateURL  = "https://releases.ghost.example.com"
	publicKey  = "" // Ed25519 public key for signature verification
	checkInterval = 6 * time.Hour
)

// UpdateManifest describes a new release.
type UpdateManifest struct {
	Version     string `json:"version"`
	DownloadURL string `json:"download_url"`
	Signature   string `json:"signature"`
	SHA256      string `json:"sha256"`
	ReleaseNotes string `json:"release_notes"`
}

func main() {
	url := flag.String("url", "", "Update server URL")
	interval := flag.Duration("interval", checkInterval, "Check interval")
	force := flag.Bool("force", false, "Force update check")
	flag.Parse()

	if *url != "" {
		updateURL = *url
	}

	log.Printf("Ghost Updater v%s starting", version)
	log.Printf("Check interval: %s", *interval)

	// Run update check loop
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	// Check immediately on start
	if *force {
		checkAndUpdate()
	}

	for range ticker.C {
		checkAndUpdate()
	}
}

func checkAndUpdate() {
	log.Println("Checking for updates...")

	manifest, err := fetchManifest()
	if err != nil {
		log.Printf("Failed to fetch manifest: %v", err)
		return
	}

	if manifest.Version == version {
		log.Println("Already up to date")
		return
	}

	log.Printf("New version available: %s (current: %s)", manifest.Version, version)
	log.Printf("Release notes: %s", manifest.ReleaseNotes)

	if err := performUpdate(manifest); err != nil {
		log.Printf("Update failed: %v", err)
		rollback()
		return
	}

	log.Println("Update completed successfully")
}

func fetchManifest() (*UpdateManifest, error) {
	resp, err := http.Get(updateURL + "/manifest.json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var manifest UpdateManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, err
	}

	return &manifest, nil
}

func performUpdate(manifest *UpdateManifest) error {
	// 1. Download new binary
	log.Println("Downloading new version...")
	binaryPath, err := downloadBinary(manifest.DownloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(binaryPath)

	// 2. Verify signature
	if publicKey != "" {
		log.Println("Verifying signature...")
		if err := verifySignature(binaryPath, manifest.Signature); err != nil {
			return fmt.Errorf("signature verification failed: %w", err)
		}
	}

	// 3. Verify checksum
	log.Println("Verifying checksum...")
	if err := verifyChecksum(binaryPath, manifest.SHA256); err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	// 4. Backup current binary
	currentBinary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get current binary path: %w", err)
	}

	backupPath := currentBinary + ".backup"
	if err := copyFile(currentBinary, backupPath); err != nil {
		return fmt.Errorf("failed to backup current binary: %w", err)
	}
	log.Printf("Backed up current binary to %s", backupPath)

	// 5. Replace binary
	if err := copyFile(binaryPath, currentBinary); err != nil {
		// Restore from backup
		copyFile(backupPath, currentBinary)
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	// 6. Restart Ghost service
	log.Println("Restarting Ghost service...")
	if err := restartGhost(); err != nil {
		log.Printf("Warning: failed to restart service: %v", err)
		// Don't fail the update, service might be managed differently
	}

	// 7. Health check after restart
	log.Println("Waiting for Ghost to start...")
	time.Sleep(30 * time.Second)

	if err := healthCheck(); err != nil {
		log.Println("Health check failed, rolling back...")
		return fmt.Errorf("health check failed after update: %w", err)
	}

	log.Println("Update verified successfully")
	return nil
}

func downloadBinary(url string) (string, error) {
	// Append platform-specific suffix
	suffix := getPlatformSuffix()
	downloadURL := url + "/" + suffix

	resp, err := http.Get(downloadURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	// Create temp file
	tmpFile, err := os.CreateTemp("", "ghost-update-*")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	// Write with progress
	written, err := io.Copy(tmpFile, resp.Body)
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}
	log.Printf("Downloaded %d bytes", written)

	// Make executable
	if err := os.Chmod(tmpFile.Name(), 0755); err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}

	return tmpFile.Name(), nil
}

func getPlatformSuffix() string {
	arch := runtime.GOARCH
	osName := runtime.GOOS

	// Common naming convention
	switch arch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	}

	return fmt.Sprintf("ghost-%s-%s", osName, arch)
}

func verifySignature(binaryPath, signatureHex string) error {
	if publicKey == "" {
		return nil // No public key configured, skip verification
	}

	// Read binary
	binaryData, err := os.ReadFile(binaryPath)
	if err != nil {
		return err
	}

	// Decode signature
	signature, err := hex.DecodeString(signatureHex)
	if err != nil {
		return fmt.Errorf("invalid signature format: %w", err)
	}

	// Decode public key
	pubKeyBytes, err := hex.DecodeString(publicKey)
	if err != nil {
		return fmt.Errorf("invalid public key format: %w", err)
	}

	// Verify
	if !ed25519.Verify(pubKeyBytes, binaryData, signature) {
		return fmt.Errorf("signature does not match")
	}

	return nil
}

func verifyChecksum(binaryPath, expectedSHA256 string) error {
	if expectedSHA256 == "" {
		return nil // No checksum to verify
	}

	file, err := os.Open(binaryPath)
	if err != nil {
		return err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}

	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expectedSHA256 {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedSHA256, actual)
	}

	return nil
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	// Preserve permissions
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, srcInfo.Mode())
}

func restartGhost() error {
	if runtime.GOOS == "linux" {
		cmd := exec.Command("systemctl", "restart", "ghost")
		return cmd.Run()
	}
	return fmt.Errorf("service restart not supported on %s", runtime.GOOS)
}

func healthCheck() error {
	// Try to hit the health endpoint
	for i := 0; i < 10; i++ {
		resp, err := http.Get("http://localhost:8766/v1/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("health check failed after 10 attempts")
}

func rollback() {
	currentBinary, err := os.Executable()
	if err != nil {
		log.Printf("Failed to get executable path for rollback: %v", err)
		return
	}

	backupPath := currentBinary + ".backup"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		log.Println("No backup found, cannot rollback")
		return
	}

	log.Println("Rolling back to previous version...")
	if err := copyFile(backupPath, currentBinary); err != nil {
		log.Printf("Rollback failed: %v", err)
		return
	}

	if err := restartGhost(); err != nil {
		log.Printf("Failed to restart after rollback: %v", err)
		return
	}

	log.Println("Rollback completed")
	os.Remove(backupPath)
}
