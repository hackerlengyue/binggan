package system

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"time"
)

// Go's macOS verifier creates an SSL policy even for ExtKeyUsageAny. A root
// with a Unicode common name can fail that policy before it is installed.
// Check CA trust using the system's basic X.509 policy, as installation does.
func verifySystemCertificate(parent context.Context, cert *x509.Certificate) (bool, error) {
	f, err := os.CreateTemp("", "sz-ca-trust-*.crt")
	if err != nil {
		return false, err
	}
	defer os.Remove(f.Name())
	if err = pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}); err != nil {
		f.Close()
		return false, err
	}
	if err = f.Close(); err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	out, err := securityExec(ctx, "verify-cert", "-c", f.Name(), "-p", "basic", "-L")
	if err == nil {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, fmt.Errorf("无法检查系统证书信任：%w", ctx.Err())
	}
	for _, reason := range []string{"CSSMERR_TP_NOT_TRUSTED", "CSSMERR_TP_INVALID_ANCHOR_CERT", "CSSMERR_TP_CERT_EXPIRED", "CSSMERR_TP_CERT_NOT_VALID_YET"} {
		if strings.Contains(string(out), reason) {
			return false, nil
		}
	}
	return false, commandError(ctx, "无法检查系统证书信任", out, err)
}
