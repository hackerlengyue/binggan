package captureproxy

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type CA struct {
	Certificate tls.Certificate
	Path        string
	Fingerprint string
}

func LoadCA(dir string) (CA, error) {
	c := CA{Path: filepath.Join(dir, "sz-capture-ca.crt")}
	keyPath := filepath.Join(dir, "sz-capture-ca.key")
	certPEM, certErr := os.ReadFile(c.Path)
	keyPEM, keyErr := os.ReadFile(keyPath)
	if os.IsNotExist(certErr) && os.IsNotExist(keyErr) {
		newCert, newKey, e := newCAMaterial()
		if e != nil {
			return c, e
		}
		// The private PEM bundle is the atomic source of truth. The public .crt
		// is an export that can be repaired after a crash between the two writes.
		keyPEM = append(newKey, newCert...)
		if e = replaceFile(keyPath, keyPEM, 0600); e != nil {
			return c, e
		}
		keyErr = nil
	}
	if keyErr != nil {
		return c, fmt.Errorf("证书或私钥缺失；请恢复数据目录中的证书文件，不要单独删除其中一个")
	}
	exported := certPEM
	bundled := false
	for rest := keyPEM; len(rest) > 0; {
		block, tail := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = tail
		if block.Type == "CERTIFICATE" {
			certPEM = pem.EncodeToMemory(block)
			bundled = true
			break
		}
	}
	// Existing installations with a key-only PEM keep using their matching .crt.
	if !bundled && certErr != nil {
		return c, fmt.Errorf("证书或私钥缺失；请恢复数据目录中的证书文件，不要单独删除其中一个")
	}
	pair, e := tls.X509KeyPair(certPEM, keyPEM)
	if e != nil {
		return c, e
	}
	pair.Leaf, e = x509.ParseCertificate(pair.Certificate[0])
	if e != nil {
		return c, e
	}
	if !pair.Leaf.IsCA || time.Now().Before(pair.Leaf.NotBefore) || time.Now().After(pair.Leaf.NotAfter) {
		return c, fmt.Errorf("本机抓包证书已失效，请先移除系统信任后重新生成")
	}
	if bundled && (certErr != nil || !bytes.Equal(exported, certPEM)) {
		if e = replaceFile(c.Path, certPEM, 0600); e != nil {
			return c, fmt.Errorf("证书已保存，但导出失败，修复目录权限后重新打开应用：%w", e)
		}
	}
	c.Certificate = pair
	sum := sha256.Sum256(pair.Certificate[0])
	c.Fingerprint = hex.EncodeToString(sum[:])
	return c, nil
}
func ReplaceCA(dir string) (CA, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return CA{}, e
	}
	certPEM, keyPEM, e := newCAMaterial()
	if e != nil {
		return CA{}, e
	}
	if e = replaceFile(filepath.Join(dir, "sz-capture-ca.key"), append(keyPEM, certPEM...), 0600); e != nil {
		return CA{}, e
	}
	return LoadCA(dir)
}
func newCAMaterial() (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "饼干大小姐 Local CA " + fmt.Sprintf("%032x", serial)[:8], Organization: []string{"饼干大小姐"}}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(5, 0, 0), KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true, IsCA: true, MaxPathLen: 0, MaxPathLenZero: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	raw, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}), nil
}
func replaceFile(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".ca-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	// Windows does not support syncing directory handles through os.File.Sync.
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (c CA) Trusted() bool {
	roots, e := x509.SystemCertPool()
	if e != nil {
		return false
	}
	_, e = c.Certificate.Leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}})
	return e == nil
}
