package system

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/x509"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Query CryptoAPI directly: certutil output is localized and may use UTF-16 or
// a legacy console code page. A text search is not a reliable store lookup.
func certificateInUserStore(ctx context.Context, name string, cert *x509.Certificate) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	storeName, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return false, err
	}
	store, err := windows.CertOpenStore(windows.CERT_STORE_PROV_SYSTEM_W, 0, 0,
		windows.CERT_SYSTEM_STORE_CURRENT_USER|windows.CERT_STORE_OPEN_EXISTING_FLAG|windows.CERT_STORE_READONLY_FLAG,
		uintptr(unsafe.Pointer(storeName)))
	runtime.KeepAlive(storeName)
	if err != nil {
		return false, fmt.Errorf("无法读取 Windows 当前用户证书存储：%w", err)
	}
	defer windows.CertCloseStore(store, 0)
	sum := sha1.Sum(cert.Raw)
	blob := windows.DataBlob{Size: uint32(len(sum)), Data: &sum[0]}
	found, err := windows.CertFindCertificateInStore(store, windows.X509_ASN_ENCODING, 0, windows.CERT_FIND_SHA1_HASH, unsafe.Pointer(&blob), nil)
	runtime.KeepAlive(sum)
	if found != nil {
		defer windows.CertFreeCertificateContext(found)
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if errors.Is(err, syscall.Errno(windows.CRYPT_E_NOT_FOUND)) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("无法查找 Windows 证书：%w", err)
	}
	return found != nil && bytes.Equal(cert.Raw, unsafe.Slice(found.EncodedCert, found.Length)), nil
}
