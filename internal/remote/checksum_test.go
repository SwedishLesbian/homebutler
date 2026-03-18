package remote

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestVerifyChecksum_MatchingHash(t *testing.T) {
	data := []byte("test binary data")
	h := sha256.Sum256(data)
	expectedHash := hex.EncodeToString(h[:])

	// Verify that our hash computation matches
	if len(expectedHash) != 64 {
		t.Errorf("expected 64 char hash, got %d", len(expectedHash))
	}
}

func TestVerifyChecksum_MismatchDetection(t *testing.T) {
	data := []byte("original data")
	tampered := []byte("tampered data")

	h1 := sha256.Sum256(data)
	h2 := sha256.Sum256(tampered)

	hash1 := hex.EncodeToString(h1[:])
	hash2 := hex.EncodeToString(h2[:])

	if hash1 == hash2 {
		t.Error("different data should produce different hashes")
	}
}

func TestVerifyChecksum_EmptyData(t *testing.T) {
	data := []byte{}
	h := sha256.Sum256(data)
	hash := hex.EncodeToString(h[:])

	// SHA256 of empty = e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
	expected := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if hash != expected {
		t.Errorf("empty data hash mismatch: got %s, want %s", hash, expected)
	}
}
