package captureproxy

import (
	"crypto/tls"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyKeyOnlyCertificateRemainsReadable(t *testing.T) {
	dir := t.TempDir()
	before, err := LoadCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sz-capture-ca.key")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" {
		t.Fatal("missing private key block")
	}
	if err = os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := LoadCA(dir)
	if err != nil || after.Fingerprint != before.Fingerprint {
		t.Fatal("legacy certificate changed", err)
	}
}

func TestCertificateBundleRepairsMissingPublicExport(t *testing.T) {
	dir := t.TempDir()
	before, err := LoadCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(before.Path); err != nil {
		t.Fatal(err)
	}
	after, err := LoadCA(dir)
	if err != nil || after.Fingerprint != before.Fingerprint {
		t.Fatal("committed CA was not recovered", err)
	}
}

func TestCertificateReplacementRecoversAfterExportFailure(t *testing.T) {
	dir := t.TempDir()
	before, err := LoadCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A nonempty directory deterministically makes certificate replacement fail,
	// after the private-key write has succeeded.
	if err = os.Remove(before.Path); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(before.Path, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(before.Path, "blocker"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = ReplaceCA(dir); err == nil {
		t.Fatal("expected certificate export failure")
	}
	if err = os.RemoveAll(before.Path); err != nil {
		t.Fatal(err)
	}
	after, err := LoadCA(dir)
	if err != nil {
		t.Fatalf("restart could not recover certificate pair: %v", err)
	}
	if after.Fingerprint == before.Fingerprint {
		t.Fatal("replacement was not retained")
	}
	if _, err = tls.LoadX509KeyPair(after.Path, filepath.Join(dir, "sz-capture-ca.key")); err != nil {
		t.Fatal("exported certificate and key disagree", err)
	}
	info, err := os.Stat(filepath.Join(dir, "sz-capture-ca.key"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private key permissions", info, err)
	}
}
