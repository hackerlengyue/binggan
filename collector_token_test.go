package main

import (
	"encoding/hex"
	"testing"
)

func TestCollectorTokenIsFreshAndUnpredictableLength(t *testing.T) {
	first, err := newCollectorToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newCollectorToken()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := hex.DecodeString(first)
	if err != nil || len(decoded) != 32 || first == second {
		t.Fatalf("invalid collector token: bytes=%d, duplicate=%v, error=%v", len(decoded), first == second, err)
	}
}
