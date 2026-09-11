package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"time"
)

// Evidence constructors. Evidence is runtime-derived proof that an action
// actually happened, attached to a ToolResult and forwarded into the
// canonical event. A consequential capability that reports success without
// evidence is treated as unverified by the registry. Evidence carries no
// secrets and is safe for activity/event projection.

// FileWriteEvidence proves a file write: path, byte size, content hash, and
// time. The hash lets later verification detect drift.
func FileWriteEvidence(path string, content []byte) map[string]interface{} {
	sum := sha256.Sum256(content)
	return map[string]interface{}{
		"type":      "file_write",
		"path":      path,
		"size":      len(content),
		"sha256":    hex.EncodeToString(sum[:]),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
}

// FileWriteEvidenceFromDisk reads the file back and produces evidence from
// what actually landed on disk. Used by append/edit where the caller does
// not hold the full final content.
func FileWriteEvidenceFromDisk(path string) map[string]interface{} {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]interface{}{
			"type":      "file_write",
			"path":      path,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}
	}
	return FileWriteEvidence(path, data)
}

// MessageEvidence proves an outbound message was handed to the delivery
// layer: channel, recipient, content hash, and time. It is a runtime
// acknowledgement, not a claim about the external provider's UI.
func MessageEvidence(channel, chatID, content string) map[string]interface{} {
	sum := sha256.Sum256([]byte(content))
	return map[string]interface{}{
		"type":      "acknowledgement",
		"channel":   channel,
		"recipient": chatID,
		"sha256":    hex.EncodeToString(sum[:]),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
}

// ArtifactEvidence proves an artifact was created: identifier, kind, title,
// and time.
func ArtifactEvidence(id, kind, title string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "artifact",
		"artifact_id": id,
		"kind":        kind,
		"title":       title,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}
}

// DeviceEvidence proves a device state change request and the observed
// resulting state when available.
func DeviceEvidence(entity, requested, observed string) map[string]interface{} {
	ev := map[string]interface{}{
		"type":      "state_transition",
		"entity":    entity,
		"requested": requested,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	if observed != "" {
		ev["observed"] = observed
	}
	return ev
}
