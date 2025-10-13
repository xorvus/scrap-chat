package utils

import (
	"crypto/rand"
	"encoding/hex"
)

const zxTokenLength = 16

func GenerateZX() string {
	b := make([]byte, zxTokenLength/2)
	if _, err := rand.Read(b); err != nil {
		return generateFallbackZX()
	}
	return hex.EncodeToString(b)
}

func generateFallbackZX() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, zxTokenLength)
	if _, err := rand.Read(b); err != nil {
		return "0000000000000000"
	}
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b)
}
