package tests

import (
	"testing"

	"github.com/xorvus/scrap-chat/internal/utils"
)

func TestGenerateZX(t *testing.T) {
	token := utils.GenerateZX()

	if len(token) != 16 {
		t.Errorf("expected token length 16, got %d", len(token))
	}

	token2 := utils.GenerateZX()
	if token == token2 {
		t.Error("expected unique tokens, got duplicates")
	}
}

func TestGenerateZXUniqueness(t *testing.T) {
	tokens := make(map[string]bool)
	iterations := 1000

	for i := 0; i < iterations; i++ {
		token := utils.GenerateZX()
		if tokens[token] {
			t.Fatalf("duplicate token generated: %s", token)
		}
		tokens[token] = true
	}
}

func BenchmarkGenerateZX(b *testing.B) {
	for i := 0; i < b.N; i++ {
		utils.GenerateZX()
	}
}
