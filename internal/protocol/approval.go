package protocol

import (
	"crypto/sha256"
	"strconv"
)

// ApprovalMessage binds an existing human signature to one daemon generation,
// connection challenge, operation, and exact transmitted payload byte sequence.
func ApprovalMessage(method string, generation int64, nonce, payload []byte) []byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte("whip privileged request v2\x00"))
	_, _ = hash.Write([]byte(method))
	_, _ = hash.Write([]byte(strconv.FormatInt(generation, 10)))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(nonce)
	_, _ = hash.Write(payload)
	return hash.Sum(nil)
}
