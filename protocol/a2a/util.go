package a2a

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

func randomID() string {
	var buf [10]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return time.Now().Format("20060102150405")
	}
	return hex.EncodeToString(buf[:])
}

func timeNowUTC() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}
