package tunnel

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"time.haomen/binggan/v2/internal/capture"
	"time.haomen/binggan/v2/internal/system"
)

var ErrCertificateRequired = errors.New("请打开“证书管理”，先安装到系统，再信任证书，完成后将自动启动流量接管")

type CertificateState struct {
	Trusted     bool   `json:"trusted"`
	Installed   bool   `json:"installed"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	ExpiresAt   string `json:"expiresAt"`
}

func CertificateStatus(ctx context.Context, d capture.Descriptor) (CertificateState, error) {
	var state CertificateState
	if err := ctx.Err(); err != nil {
		return state, err
	}
	if err := d.Validate(); err != nil {
		return state, err
	}
	block, _ := pem.Decode([]byte(d.Certificate))
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return state, err
	}
	trusted, err := system.CertificateTrustedContext(ctx, d.Certificate)
	if err != nil {
		return state, err
	}
	installed := trusted
	if !trusted {
		if err = withCertificateFile(d.Certificate, func(path string) error {
			var e error
			installed, e = system.CertificateInstalled(ctx, path)
			return e
		}); err != nil {
			return state, err
		}
	}
	return CertificateState{Trusted: trusted, Installed: installed, Name: cert.Subject.CommonName, Fingerprint: d.Fingerprint, ExpiresAt: cert.NotAfter.Format(time.RFC3339)}, nil
}

func prepareDesktopCertificate(ctx context.Context, d capture.Descriptor, _ *capture.Logbook) error {
	state, err := CertificateStatus(ctx, d)
	if err != nil {
		return err
	}
	if !state.Trusted {
		return ErrCertificateRequired
	}
	return nil
}

func InstallCertificate(ctx context.Context, d capture.Descriptor, progress system.CertificateProgress) (CertificateState, error) {
	if err := d.Validate(); err != nil {
		return CertificateState{}, err
	}
	progress.Report("证书校验通过", "证书格式、有效期及后端指纹一致")
	err := withCertificateFile(d.Certificate, func(path string) error {
		return system.InstallCertificate(ctx, path, progress)
	})
	if err != nil {
		return CertificateState{}, err
	}
	state, err := CertificateStatus(ctx, d)
	if err == nil && !state.Installed {
		err = fmt.Errorf("证书未能写入系统")
	}
	return state, err
}

func TrustCertificate(ctx context.Context, d capture.Descriptor, progress system.CertificateProgress) (CertificateState, error) {
	if err := d.Validate(); err != nil {
		return CertificateState{}, err
	}
	state, err := CertificateStatus(ctx, d)
	if err != nil {
		return CertificateState{}, err
	}
	if state.Trusted {
		progress.Report("证书已被系统信任", "无需再次授权")
		return state, nil
	}
	if !state.Installed {
		return state, fmt.Errorf("请先将证书安装到系统，再信任证书")
	}
	progress.Report("证书校验通过", "当前证书已在系统中，开始设置信任")
	err = withCertificateFile(d.Certificate, func(path string) error {
		return system.TrustCertificate(ctx, path, progress)
	})
	if err != nil {
		return state, err
	}
	state, err = CertificateStatus(ctx, d)
	if err == nil && !state.Trusted {
		err = fmt.Errorf("系统尚未信任该证书，请完成授权后再试")
	}
	return state, err
}

func withCertificateFile(certificate string, use func(string) error) error {
	temp, err := os.MkdirTemp("", "sz-capture-cert-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	path := filepath.Join(temp, "ca.crt")
	if err = os.WriteFile(path, []byte(certificate), 0600); err != nil {
		return err
	}
	return use(path)
}
