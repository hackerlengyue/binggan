package system

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeCA(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "SZ Capture Local CA TESTTEST"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "ca.crt")
	if err = os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha1.Sum(der)
	return path, hex.EncodeToString(sum[:])
}

func TestImportAddsCertificateWithoutTrust(t *testing.T) {
	path, fingerprint := writeCA(t)
	keychain := "/Users/test/Library/Keychains/login.keychain-db"
	var calls []string
	stored := false
	err := importCertificate(context.Background(), path, 501, func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args[0])
		switch args[0] {
		case "default-keychain":
			return []byte(`"` + keychain + `"`), nil
		case "find-certificate":
			if stored {
				return []byte("SHA-1 hash: " + strings.ToUpper(fingerprint)), nil
			}
			return []byte("not found"), errors.New("missing")
		case "add-certificates":
			if args[1] != "-k" || args[2] != keychain || args[3] != path {
				t.Fatalf("import arguments: %v", args)
			}
			stored = true
			return nil, nil
		default:
			t.Fatalf("trust command during install: %v", args)
		}
		return nil, nil
	}, nil)
	if err != nil || strings.Join(calls, ",") != "default-keychain,find-certificate,add-certificates,find-certificate" {
		t.Fatalf("install changed trust: calls=%v err=%v", calls, err)
	}
}

func TestImportTreatsExistingCertificateAsInstalled(t *testing.T) {
	path, fingerprint := writeCA(t)
	var calls []string
	err := importCertificate(context.Background(), path, 501, func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args[0])
		if args[0] == "default-keychain" {
			return []byte(`"/Users/test/login.keychain-db"`), nil
		}
		return []byte("SHA-1 hash: " + fingerprint), nil
	}, nil)
	if err != nil || strings.Join(calls, ",") != "default-keychain,find-certificate" {
		t.Fatalf("existing certificate was imported again: calls=%v err=%v", calls, err)
	}
}

func TestTrustRefusesUntilCertificateIsInstalled(t *testing.T) {
	path, _ := writeCA(t)
	var calls []string
	err := trustCertificate(context.Background(), path, 501, func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args[0])
		if args[0] == "default-keychain" {
			return []byte(`"/Users/test/login.keychain-db"`), nil
		}
		return []byte("not found"), errors.New("missing")
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "请先将证书安装到系统") || strings.Contains(strings.Join(calls, ","), "add-trusted-cert") {
		t.Fatalf("trust ran before install: calls=%v err=%v", calls, err)
	}
}

func TestTrustUsesInteractiveUserKeychainAfterInstall(t *testing.T) {
	path, fingerprint := writeCA(t)
	keychain := "/Users/test/Library/Keychains/login.keychain-db"
	var calls []string
	err := trustCertificate(context.Background(), path, 501, func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args[0])
		switch args[0] {
		case "verify-cert":
			if len(calls) == 1 {
				return []byte("CSSMERR_TP_NOT_TRUSTED"), errors.New("untrusted")
			}
			return nil, nil
		case "default-keychain":
			return []byte(`"` + keychain + `"`), nil
		case "find-certificate":
			return []byte("SHA-1 hash: " + strings.ToUpper(fingerprint)), nil
		case "add-trusted-cert":
			if args[1] != "-r" || args[2] != "trustRoot" || args[3] != "-k" || args[4] != keychain || args[5] != path {
				t.Fatalf("trust arguments: %v", args)
			}
			return nil, nil
		default:
			t.Fatalf("unexpected %v", args)
		}
		return nil, nil
	}, nil)
	if err != nil || strings.Join(calls, ",") != "verify-cert,default-keychain,find-certificate,add-trusted-cert,verify-cert" {
		t.Fatalf("unexpected trust flow: calls=%v err=%v", calls, err)
	}
}

func TestTrustSkipsAlreadyTrustedCA(t *testing.T) {
	path, _ := writeCA(t)
	calls := 0
	err := trustCertificate(context.Background(), path, 501, func(context.Context, ...string) ([]byte, error) {
		calls++
		return nil, nil
	}, nil)
	if err != nil || calls != 1 {
		t.Fatalf("trusted certificate prompted again: calls=%d, err=%v", calls, err)
	}
}

func TestTrustRejectsBackgroundRootSession(t *testing.T) {
	err := trustCertificate(context.Background(), "ca.crt", 0, func(context.Context, ...string) ([]byte, error) {
		t.Fatal("must not change trust from the root helper")
		return nil, nil
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "登录用户会话") {
		t.Fatalf("missing actionable session error: %v", err)
	}
}

func TestTrustAuthorizationDenialDoesNotReportSuccess(t *testing.T) {
	path, fingerprint := writeCA(t)
	var stages []string
	err := trustCertificate(context.Background(), path, 501, func(_ context.Context, args ...string) ([]byte, error) {
		switch args[0] {
		case "verify-cert":
			return []byte("untrusted"), errors.New("untrusted")
		case "default-keychain":
			return []byte(`"/Users/test/login.keychain-db"`), nil
		case "find-certificate":
			return []byte("SHA-1 hash: " + fingerprint), nil
		case "add-trusted-cert":
			if len(stages) == 0 || stages[len(stages)-1] != "等待系统授权" {
				t.Fatal("authorization command ran before progress was reported")
			}
			return []byte("The authorization was denied since no user interaction was possible."), errors.New("denied")
		default:
			t.Fatalf("unexpected %v", args)
		}
		return nil, nil
	}, func(message, _ string) { stages = append(stages, message) })
	if err == nil || !strings.Contains(err.Error(), "授权窗口") || stages[len(stages)-1] != "等待系统授权" {
		t.Fatalf("denied trust reported success: stages=%v err=%v", stages, err)
	}
}

func TestKeychainReadFailureIsNotReportedAsMissingCertificate(t *testing.T) {
	fingerprint := strings.Repeat("a", 40)
	installed, err := certificateInKeychain(context.Background(), func(context.Context, ...string) ([]byte, error) {
		return []byte("User interaction is not allowed"), errors.New("security failed")
	}, "/Users/test/login.keychain-db", "Test CA", fingerprint)
	if installed || err == nil || !strings.Contains(err.Error(), "无法检查登录钥匙串") {
		t.Fatalf("keychain failure = installed:%v err:%v", installed, err)
	}
	installed, err = certificateInKeychain(context.Background(), func(context.Context, ...string) ([]byte, error) {
		return []byte("The specified item could not be found in the keychain."), errors.New("not found")
	}, "/Users/test/login.keychain-db", "Test CA", fingerprint)
	if installed || err != nil {
		t.Fatalf("missing certificate = installed:%v err:%v", installed, err)
	}
}

func TestCaptureHelperUsesSeparateV2SystemIdentity(t *testing.T) {
	uid := 501
	if got := captureServiceLabel(uid); got != "local.szjm.capture.v2.501" {
		t.Fatalf("helper label = %q", got)
	}
	if got := CaptureServiceSocket(uid); got != "/var/run/local.szjm.capture.v2.501.sock" {
		t.Fatalf("helper socket = %q", got)
	}
	if strings.Contains(captureServiceBinary(uid), "local.szjm.capture.501") || strings.Contains(captureServicePlist(uid), "local.szjm.capture.501") {
		t.Fatal("v2 helper overlaps the legacy launchd files")
	}
}
