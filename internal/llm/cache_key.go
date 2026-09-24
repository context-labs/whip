package llm

import (
	"crypto/sha256"
	"encoding/hex"
)

// normalizeCacheKey preserves existing cache affinity while bounding provider
// keys, including legacy root/child composites and explicit caller overrides.
func normalizeCacheKey(key string) string {
	if len(key) <= 64 {
		return key
	}
	digest := sha256.Sum256([]byte(key))
	return hex.EncodeToString(digest[:])
}
