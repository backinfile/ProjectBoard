package ids

import (
	"crypto/rand"
	"encoding/hex"
)

func New(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}
