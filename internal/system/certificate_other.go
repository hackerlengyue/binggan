//go:build !darwin

package system

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
)

func verifySystemCertificate(ctx context.Context, cert *x509.Certificate) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	_, err := cert.Verify(x509.VerifyOptions{KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}})
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err == nil {
		return true, nil
	}
	var unknown x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	if errors.As(err, &unknown) || errors.As(err, &invalid) {
		return false, nil
	}
	return false, fmt.Errorf("无法检查系统证书信任：%w", err)
}
