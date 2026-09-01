package tasks

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

func now() int64 { return time.Now().Unix() }

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "job-" + time.Now().Format("20060102150405")
	}
	return "job-" + hex.EncodeToString(b)
}
