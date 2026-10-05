package system

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
	"unicode"
)

// CertificateTrusted uses the current OS trust settings on macOS and Windows.
// No explicit root pool is supplied: a bundled CA alone must not imply trust.
func CertificateTrusted(certificate string) (bool, error) {
	return CertificateTrustedContext(context.Background(), certificate)
}

// CertificateTrustedContext lets monitor shutdown cancel a trust check that
// is waiting on the OS certificate verifier.
func CertificateTrustedContext(ctx context.Context, certificate string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	block, _ := pem.Decode([]byte(certificate))
	if block == nil || block.Type != "CERTIFICATE" {
		return false, fmt.Errorf("证书格式无效")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, fmt.Errorf("无法读取证书：%w", err)
	}
	return verifySystemCertificate(ctx, cert)
}

// CertificateProgress reports actual installation stages, never simulated percentages.
type CertificateProgress func(message, detail string)

func outputHasFingerprint(out, fingerprint string) bool {
	var compact strings.Builder
	for _, r := range strings.ToLower(out) {
		if unicode.Is(unicode.ASCII_Hex_Digit, r) {
			compact.WriteRune(r)
		}
	}
	return strings.Contains(compact.String(), strings.ToLower(fingerprint))
}

func (p CertificateProgress) Report(message, detail string) {
	if p != nil {
		p(message, detail)
	}
}
