//go:build !windows && !darwin

package system

import (
	"context"
	"fmt"
)

func LaunchHelper(context.Context, string, string, string) error {
	return fmt.Errorf("只支持 macOS 和 Windows")
}
func InstallCertificate(context.Context, string, CertificateProgress) error {
	return fmt.Errorf("只支持 macOS 和 Windows")
}
func TrustCertificate(context.Context, string, CertificateProgress) error {
	return fmt.Errorf("只支持 macOS 和 Windows")
}
func CertificateInstalled(context.Context, string) (bool, error) {
	return false, fmt.Errorf("只支持 macOS 和 Windows")
}
