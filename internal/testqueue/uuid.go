package testqueue

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// NewQueueUUID returns a UUIDv7 suitable for use as a queue partition key.
func NewQueueUUID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generating queue uuid randomness: %w", err)
	}

	nowMillis := time.Now().UnixMilli()
	if nowMillis < 0 {
		return "", fmt.Errorf("generating queue uuid timestamp: %d is before unix epoch", nowMillis)
	}
	millis := uint64(nowMillis)
	bytes[0] = byte(millis >> 40)
	bytes[1] = byte(millis >> 32)
	bytes[2] = byte(millis >> 24)
	bytes[3] = byte(millis >> 16)
	bytes[4] = byte(millis >> 8)
	bytes[5] = byte(millis)
	bytes[6] = (bytes[6] & 0x0f) | 0x70
	bytes[8] = (bytes[8] & 0x3f) | 0x80

	hexed := hex.EncodeToString(bytes[:])
	return hexed[0:8] + "-" + hexed[8:12] + "-" + hexed[12:16] + "-" + hexed[16:20] + "-" + hexed[20:32], nil
}
