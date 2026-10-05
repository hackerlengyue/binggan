// Package engine implements SZ decryption in Go. FFmpeg is used only for media
// decoding/encoding; password derivation, packet crypto and audio repair are Go.
package engine

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"howett.net/plist"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

type Key struct {
	Passwords map[string]string `json:"passwords"`
	Data      map[string]any    `json:"getPwdData"`
}
type Player struct {
	SoftwareName string `json:"softwareName"`
	AppMD5       string `json:"appMd5"`
}

var KnownPlayers = map[string]string{"MAC_sz_25.12.52": "de31c80fff74af282de314ec43b456d1"}
var hexPattern = regexp.MustCompile(`^[0-9a-fA-F]{32,256}$`)

func HashValid(s string) bool { return len(s) == 32 && hexPattern.MatchString(s) }
func str(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}
func pick(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := str(m[k]); s != "" {
			return s
		}
	}
	return ""
}
func number(v any) (int64, error) {
	s := str(v)
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("参数必须为整数")
	}
	return n, nil
}
func ReadKey(raw []byte) (Key, error) {
	var k Key
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	if err := d.Decode(&k); err != nil {
		return k, errors.New("密钥 JSON 格式无效")
	}
	return k, k.Validate()
}
func (k Key) Validate() error {
	if len(k.Passwords) == 0 || len(k.Passwords) > 10000 || k.Data == nil {
		return errors.New("缺少完整的 passwords/getPwdData")
	}
	for i, p := range k.Passwords {
		n, err := strconv.Atoi(i)
		if err != nil || n < 1 || len(p) < 1 || len(p) > 4096 {
			return errors.New("分段密钥格式无效")
		}
	}
	for _, field := range []string{"den", "pmn"} {
		v, err := number(k.Data[field])
		if err != nil || v <= 0 {
			return fmt.Errorf("%s 必须为正整数", field)
		}
	}
	if p := str(k.Data["pattern"]); p != "1" && p != "3" {
		return errors.New("仅支持 pattern 1 或 3")
	}
	if pick(k.Data, "mima", "mimaPlain", "mima_plain", "mimaEnc", "mima_enc", "password", "pwd", "密码", "firstPassword", "first_password") == "" {
		return errors.New("缺少 mima 参数")
	}
	if v := pick(k.Data, "appMd5", "app_md5"); v != "" && !HashValid(v) {
		return errors.New("App 校验值必须为 32 位十六进制")
	}
	return nil
}
func DetectPlayer() (Player, error) {
	if runtime.GOOS == "windows" {
		if installations := WindowsPlayerInstallations(); len(installations) > 0 {
			return Player{}, &PlayerParametersUnavailable{Installation: installations[0]}
		}
		return Player{}, errors.New("未找到 Windows 播放器，请先打开深造播放器，再重新检测")
	}
	home, _ := os.UserHomeDir()
	for _, app := range []string{"/Applications/SzPlayer.app", filepath.Join(home, "Applications/SzPlayer.app")} {
		p, err := PlayerAt(app)
		if err == nil {
			return p, nil
		}
	}
	return Player{}, errors.New("未找到可读取的播放器")
}
func PlayerAt(app string) (Player, error) {
	b, err := os.ReadFile(filepath.Join(app, "Contents/Info.plist"))
	if err != nil {
		return Player{}, err
	}
	var info struct {
		Executable string `plist:"CFBundleExecutable"`
		Version    string `plist:"CFBundleShortVersionString"`
	}
	if _, err = plist.Unmarshal(b, &info); err != nil {
		return Player{}, err
	}
	if info.Executable == "" || filepath.Base(info.Executable) != info.Executable || info.Version == "" {
		return Player{}, errors.New("播放器信息无效")
	}
	f, err := os.Open(filepath.Join(app, "Contents/MacOS", info.Executable))
	if err != nil {
		return Player{}, err
	}
	defer f.Close()
	digest, err := playerFileHash(f)
	if err != nil {
		return Player{}, err
	}
	return Player{"MAC_sz_" + info.Version, digest}, nil
}

var playerHashCache = struct {
	sync.Mutex
	entries map[string]struct {
		info   os.FileInfo
		digest string
	}
}{entries: make(map[string]struct {
	info   os.FileInfo
	digest string
})}

// Only the executable digest is cached. Plist/version and file identity are
// checked on every call, so an upgrade or replacement invalidates the digest.
func playerFileHash(f *os.File) (string, error) {
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	playerHashCache.Lock()
	defer playerHashCache.Unlock()
	if cached, ok := playerHashCache.entries[f.Name()]; ok && os.SameFile(cached.info, info) && cached.info.Size() == info.Size() && cached.info.ModTime() == info.ModTime() {
		return cached.digest, nil
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	after, err := os.Stat(f.Name())
	if err != nil {
		return "", err
	}
	if !os.SameFile(info, after) || info.Size() != after.Size() || info.ModTime() != after.ModTime() {
		return "", errors.New("播放器正在更新，请重新检测")
	}
	sum := md5.Sum([]byte(hex.EncodeToString(h.Sum(nil))))
	digest := hex.EncodeToString(sum[:])
	if len(playerHashCache.entries) >= 32 {
		clear(playerHashCache.entries)
	}
	playerHashCache.entries[f.Name()] = struct {
		info   os.FileInfo
		digest string
	}{info, digest}
	return digest, nil
}
func ResolveHash(data map[string]any, env string, fallback *Player) (string, error) {
	if s := pick(data, "appMd5", "app_md5"); s != "" {
		if !HashValid(s) {
			return "", errors.New("App 校验值无效")
		}
		return strings.ToLower(s), nil
	}
	version := pick(data, "softwareName", "softwarename", "software_name")
	if fallback != nil {
		if version != "" && version != fallback.SoftwareName {
			return "", errors.New("密钥中的 App 版本与解密配置不一致")
		}
		return fallback.AppMD5, nil
	}
	if env != "" {
		if !HashValid(env) {
			return "", errors.New("播放器校验值无效")
		}
		return strings.ToLower(env), nil
	}
	if h, ok := KnownPlayers[version]; ok {
		return h, nil
	}
	p, err := DetectPlayer()
	if err != nil {
		return "", fmt.Errorf("无法获取 App 校验值：%w；请在解密设置中配置与提取记录对应的版本和校验值", err)
	}
	if version != "" && version != p.SoftwareName {
		return "", errors.New("密钥中的 App 版本与本机版本不一致")
	}
	return p.AppMD5, nil
}
func triplet(uit string) ([3]string, error) {
	var out [3]string
	if len(uit) < 108 || uit[5] != 's' || uit[9] != 'z' || uit[12] != 'k' || uit[15] != 'j' {
		return out, errors.New("uit 长度或标记无效")
	}
	for _, i := range []int{15, 12, 9, 5} {
		uit = uit[:i] + uit[i+1:]
	}
	out[0] = uit[4:8] + uit[35:47] + uit[72:88]
	out[1] = uit[47:56] + uit[8:19] + uit[88:100]
	out[2] = uit[19:35] + uit[56:72]
	return out, nil
}
func Mima(data map[string]any, appHash string) (string, error) {
	raw := pick(data, "mima", "mimaPlain", "mima_plain")
	if hexPattern.MatchString(raw) {
		return strings.ToLower(raw), nil
	}
	enc := pick(data, "mimaEnc", "mima_enc", "password", "pwd", "密码", "firstPassword", "first_password")
	if enc == "" {
		enc = raw
	}
	md := [3]string{pick(data, "md51"), pick(data, "md52"), pick(data, "md53")}
	if !HashValid(md[0]) || !HashValid(md[1]) || !HashValid(md[2]) {
		var err error
		md, err = triplet(pick(data, "uit", "int", "uitStr", "uuitString"))
		if err != nil {
			return "", err
		}
	}
	if !HashValid(appHash) {
		return "", errors.New("缺少有效的 App 校验值")
	}
	sum := md5.Sum([]byte(appHash + md[0] + md[1] + md[2]))
	block, _ := aes.NewCipher([]byte(hex.EncodeToString(sum[:])))
	ct, err := base64.StdEncoding.DecodeString(enc)
	if err != nil || len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return "", errors.New("密文 mima 不是有效的 AES/Base64 数据")
	}
	for _, cbc := range []bool{false, true} {
		plain := make([]byte, len(ct))
		if cbc {
			cipher.NewCBCDecrypter(block, make([]byte, 16)).CryptBlocks(plain, ct)
		} else {
			for i := 0; i < len(ct); i += 16 {
				block.Decrypt(plain[i:i+16], ct[i:i+16])
			}
		}
		n := int(plain[len(plain)-1])
		if n > 0 && n <= 16 {
			valid := true
			for _, v := range plain[len(plain)-n:] {
				if int(v) != n {
					valid = false
				}
			}
			if valid {
				plain = plain[:len(plain)-n]
			}
		}
		s := strings.TrimSpace(string(plain))
		if hexPattern.MatchString(s) {
			return strings.ToLower(s), nil
		}
	}
	return "", errors.New("mima AES 解密失败，请检查密钥、uit 和 App 版本是否对应")
}
func Transform(s string) string {
	if len(s) == 0 {
		return s
	}
	slice := func(s string, a, b int) string {
		if a > len(s) {
			return ""
		}
		if b > len(s) {
			b = len(s)
		}
		return s[a:b]
	}
	switch s[0] {
	case '1':
		s = slice(s, 8, 12) + slice(s, 16, 21) + slice(s, 25, 30) + slice(s, 34, 40) + slice(s, 44, 52) + slice(s, 56, 60)
		s = slice(s, 24, 28) + slice(s, 16, 20) + slice(s, 4, 8) + slice(s, 12, 16) + slice(s, 20, 24) + slice(s, 8, 12) + slice(s, 0, 4) + slice(s, 28, 32)
		return slice(s, 8, 16) + slice(s, 16, 24) + slice(s, 0, 8) + slice(s, 24, 32)
	case '2':
		var b strings.Builder
		for _, i := range []int{57, 61, 25, 23, 1, 21, 19, 3, 17, 15, 53, 5, 29, 27, 7, 13, 9, 11, 31, 45, 63, 33, 59, 35, 55, 49, 37, 51, 47, 39, 43, 41} {
			if i < len(s) {
				b.WriteByte(s[i])
			}
		}
		return b.String()
	}
	return s
}
func segment(pos, den, pmn int64) int64 {
	v := pos / den
	if pos%den >= den/2+den%2 {
		v++
	}
	v = v/pmn + 1
	if v > 180 {
		v = 180
	}
	return v
}
func packetKey(mima, pwd string, size int) []byte {
	if len(mima) > 10 {
		mima = mima[:10]
	}
	if len(pwd) > 10 {
		pwd = pwd[:10]
	}
	return []byte(mima + strconv.Itoa(size) + pwd)
}
