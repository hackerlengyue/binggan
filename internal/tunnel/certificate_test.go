package tunnel

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"
)

func TestCertificateStatusDoesNotTrustBackendPEMAlone(t *testing.T) {
	d := descriptor(t)
	state, err := CertificateStatus(context.Background(), d)
	if err != nil || state.Trusted || state.Fingerprint != d.Fingerprint || state.ExpiresAt == "" {
		t.Fatalf("incorrect OS trust status: %+v, %v", state, err)
	}
	if err = prepareDesktopCertificate(context.Background(), d, nil); !errors.Is(err, ErrCertificateRequired) {
		t.Fatalf("untrusted CA did not require installation: %v", err)
	}
}

func TestInvalidCertificateCannotReachOSInstallation(t *testing.T) {
	d := descriptor(t)
	d.Fingerprint = "mismatch"
	if _, err := InstallCertificate(context.Background(), d, nil); err == nil {
		t.Fatal("invalid fingerprint accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CertificateStatus(ctx, descriptor(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled status check: %v", err)
	}
}

func TestCertificateTemporaryFileIsPrivateAndAlwaysRemoved(t *testing.T) {
	for _, failure := range []bool{false, true} {
		var path string
		expected := errors.New("authorization canceled")
		err := withCertificateFile("public-cert", func(p string) error {
			path = p
			st, err := os.Stat(path)
			// Windows permissions are ACL-based; POSIX mode bits do not apply.
			if err != nil || (runtime.GOOS != "windows" && st.Mode().Perm() != 0600) {
				t.Fatalf("certificate file permissions: %v %v", st, err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "public-cert" {
				t.Fatalf("certificate file contents: %s %v", data, err)
			}
			if failure {
				return expected
			}
			return nil
		})
		if failure && !errors.Is(err, expected) {
			t.Fatal(err)
		}
		if !failure && err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("certificate temporary file was not removed")
		}
	}
}
