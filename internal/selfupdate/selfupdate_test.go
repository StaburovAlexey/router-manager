package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestBinaryAssetName(t *testing.T) {
	got, err := binaryAssetName("linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if got != "vpn-router-linux-amd64" {
		t.Fatalf("asset = %q", got)
	}
	if _, err := binaryAssetName("darwin", "amd64"); err == nil {
		t.Fatal("expected unsupported OS error")
	}
}

func TestChecksumForAcceptsDistPath(t *testing.T) {
	text := "abc123  dist/vpn-router-linux-amd64\n"
	got, ok := checksumFor(text, "vpn-router-linux-amd64")
	if !ok || got != "abc123" {
		t.Fatalf("checksum = %q, ok = %v", got, ok)
	}
}

func TestVerifySHA256(t *testing.T) {
	data := []byte("vpn-router")
	sum := sha256.Sum256(data)
	if err := verifySHA256(data, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if err := verifySHA256(data, strings.Repeat("0", 64)); err == nil {
		t.Fatal("expected checksum mismatch")
	}
}

func TestSameVersion(t *testing.T) {
	if !sameVersion("0.1.0", "v0.1.0") {
		t.Fatal("versions must match with optional v prefix")
	}
	if sameVersion("dev", "v0.1.0") {
		t.Fatal("dev must not be treated as latest")
	}
}
