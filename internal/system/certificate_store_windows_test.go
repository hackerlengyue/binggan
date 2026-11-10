package system

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"math/big"
	"testing"
	"time"
)

// Read only: never install a test root or change the developer/CI trust store.
func TestWindowsCertificateLookupDoesNotTrustAnUninstalledCertificate(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), IsCA: true, BasicConstraintsValid: true, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	for _, store := range []string{"CA", "Root"} {
		found, err := certificateInUserStore(t.Context(), store, cert)
		if err != nil || found {
			t.Fatalf("%s: found=%v err=%v", store, found, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := certificateInUserStore(ctx, "CA", cert); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := LaunchHelper(ctx, `C:\missing.exe`, "", ""); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled launcher attempted elevation", err)
	}
}
