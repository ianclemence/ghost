// Package pairing implements secure device pairing for Ghost.
//
// Flow:
//   1. Ghost Pod (web UI) calls POST /v1/pairing/start → gets short-lived token + pairing_id
//   2. Ghost Pod displays token as QR code (ghost://pair?token=...&host=...&port=...)
//   3. Mobile app scans QR, calls POST /v1/pairing/redeem with token
//   4. Backend validates token, creates paired_device, returns connection config
//   5. Mobile stores credentials in SecureStore
//
// Tokens are single-use, expire after 5 minutes, and are stored as SHA-256 hashes.
package pairing

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	TokenLength    = 32 // bytes → 64 hex chars
	TokenExpiry    = 5 * time.Minute
	DeviceIDLength = 12 // bytes → 24 hex chars
)

// PendingPairing represents an unused pairing token.
type PendingPairing struct {
	ID           string    `json:"id"`
	TokenHash    string    `json:"-"` // SHA-256 hex, never sent to client
	DisplayName  string    `json:"display_name"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
}

// PairedDevice represents a successfully paired mobile device.
type PairedDevice struct {
	ID           string    `json:"id"`
	DeviceID     string    `json:"device_id"`
	DisplayName  string    `json:"display_name"`
	Credential   string    `json:"-"` // bcrypt-ish hash, never sent after pairing
	PairedAt     time.Time `json:"paired_at"`
	LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
}

// PairingResult is returned to the mobile app after successful redemption.
type PairingResult struct {
	DeviceID    string `json:"device_id"`
	Credential  string `json:"credential"` // plaintext, shown once
	PairedAt    string `json:"paired_at"`
}

// DB methods that operate on the pairing tables.
// These are attached to *sql.DB so the existing db.DB wrapper works unchanged.

// CreatePendingPairing generates a new short-lived pairing token.
// Returns the plaintext token (displayed as QR) and the pairing ID.
func CreatePendingPairing(database *sql.DB, displayName string) (token string, pairingID string, err error) {
	// Generate random token
	buf := make([]byte, TokenLength)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	token = hex.EncodeToString(buf)

	// Hash for storage
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	// Generate pairing ID
	idBuf := make([]byte, 8)
	if _, err := rand.Read(idBuf); err != nil {
		return "", "", fmt.Errorf("generate pairing id: %w", err)
	}
	pairingID = hex.EncodeToString(idBuf)

	now := time.Now().UTC()
	expiresAt := now.Add(TokenExpiry)

	_, err = database.Exec(`
		INSERT INTO pending_pairings (id, token_hash, display_name, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		pairingID, tokenHash, displayName, expiresAt, now,
	)
	if err != nil {
		return "", "", fmt.Errorf("insert pairing: %w", err)
	}

	return token, pairingID, nil
}

// RedeemPairing validates a token, creates a paired device, and deletes the pending pairing.
// Returns the device credential (plaintext, shown to user once).
func RedeemPairing(database *sql.DB, token string) (*PairingResult, error) {
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	// Find and delete pending pairing (single-use)
	var pairingID, displayName string
	var expiresAt time.Time
	err := database.QueryRow(`
		DELETE FROM pending_pairings
		WHERE token_hash = ? AND expires_at > ?
		RETURNING id, display_name, expires_at`,
		tokenHash, time.Now().UTC(),
	).Scan(&pairingID, &displayName, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("invalid or expired token")
	}
	if err != nil {
		return nil, fmt.Errorf("redeem pairing: %w", err)
	}

	// Generate device credential
	credBuf := make([]byte, TokenLength)
	if _, err := rand.Read(credBuf); err != nil {
		return nil, fmt.Errorf("generate credential: %w", err)
	}
	credential := hex.EncodeToString(credBuf)

	// Hash credential for storage
	credHash := sha256.Sum256([]byte(credential))
	credHashHex := hex.EncodeToString(credHash[:])

	// Generate device ID
	deviceBuf := make([]byte, DeviceIDLength)
	if _, err := rand.Read(deviceBuf); err != nil {
		return nil, fmt.Errorf("generate device id: %w", err)
	}
	deviceID := hex.EncodeToString(deviceBuf)

	now := time.Now().UTC()
	_, err = database.Exec(`
		INSERT INTO paired_devices (id, device_id, display_name, credential_hash, paired_at)
		VALUES (?, ?, ?, ?, ?)`,
		deviceID, deviceID, displayName, credHashHex, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert device: %w", err)
	}

	return &PairingResult{
		DeviceID:   deviceID,
		Credential: credential,
		PairedAt:   now.Format(time.RFC3339),
	}, nil
}

// ValidateCredential checks a device credential against stored hashes.
// Returns true if the credential matches an active (non-revoked) device.
func ValidateCredential(database *sql.DB, deviceID, credential string) (bool, error) {
	var credHash string
	var revokedAt sql.NullTime
	err := database.QueryRow(`
		SELECT credential_hash, revoked_at
		FROM paired_devices
		WHERE device_id = ?`,
		deviceID,
	).Scan(&credHash, &revokedAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("validate credential: %w", err)
	}
	if revokedAt.Valid {
		return false, nil
	}

	// Constant-time compare
	hash := sha256.Sum256([]byte(credential))
	gotHash := hex.EncodeToString(hash[:])

	if len(credHash) != len(gotHash) {
		return false, nil
	}
	result := byte(0)
	for i := 0; i < len(credHash); i++ {
		result |= credHash[i] ^ gotHash[i]
	}
	return result == 0, nil
}

// UpdateLastSeen updates the last_seen_at timestamp for a device.
func UpdateLastSeen(database *sql.DB, deviceID string) error {
	_, err := database.Exec(`
		UPDATE paired_devices SET last_seen_at = ? WHERE device_id = ?`,
		time.Now().UTC(), deviceID,
	)
	return err
}

// RevokeDevice marks a device as revoked.
func RevokeDevice(database *sql.DB, deviceID string) error {
	result, err := database.Exec(`
		UPDATE paired_devices SET revoked_at = ? WHERE device_id = ? AND revoked_at IS NULL`,
		time.Now().UTC(), deviceID,
	)
	if err != nil {
		return fmt.Errorf("revoke device: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("device not found or already revoked")
	}
	return nil
}

// ListDevices returns all non-revoked paired devices.
func ListDevices(database *sql.DB) ([]PairedDevice, error) {
	rows, err := database.Query(`
		SELECT id, device_id, display_name, paired_at, last_seen_at, revoked_at
		FROM paired_devices
		WHERE revoked_at IS NULL
		ORDER BY paired_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()

	var devices []PairedDevice
	for rows.Next() {
		var d PairedDevice
		if err := rows.Scan(&d.ID, &d.DeviceID, &d.DisplayName, &d.PairedAt, &d.LastSeenAt, &d.RevokedAt); err != nil {
			continue
		}
		devices = append(devices, d)
	}
	return devices, nil
}

// CleanupExpired deletes expired pending pairings.
func CleanupExpired(database *sql.DB) error {
	_, err := database.Exec(`
		DELETE FROM pending_pairings WHERE expires_at < ?`,
		time.Now().UTC(),
	)
	return err
}
