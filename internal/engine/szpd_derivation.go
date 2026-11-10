package engine

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

const szpdFilenameFillDerivation = "filename-fill-v1"

func (k Key) UsesSZPDDerivation() bool {
	return k.IsSZPD() && k.Data["derivation"] == szpdFilenameFillDerivation
}

func (k Key) validateSZPDParameters() error {
	if derivation, exists := k.Data["derivation"]; exists {
		if derivation != szpdFilenameFillDerivation {
			return errors.New("未知的 SZPD 派生方案，仅支持 filename-fill-v1")
		}
		if _, exists := k.Data["aesKey"]; exists {
			return errors.New("SZPD 派生模式不能同时填写 aesKey/aesIV")
		}
		if _, exists := k.Data["aesIV"]; exists {
			return errors.New("SZPD 派生模式不能同时填写 aesKey/aesIV")
		}
		_, err := k.szpdFileNameBytes()
		return err
	}
	_, _, err := k.szpdParameters()
	return err
}

// WithInputName preserves the original name before uploads are retained as
// input.sz. Clone Data so a shared batch key never inherits another file's name.
// Explicit fileName/fileNameHex describe exact hash input and take precedence.
func (k Key) WithInputName(name string) Key {
	if !k.UsesSZPDDerivation() {
		return k
	}
	if _, ok := k.Data["fileName"]; ok {
		return k
	}
	if _, ok := k.Data["fileNameHex"]; ok {
		return k
	}
	data := make(map[string]any, len(k.Data)+1)
	for field, value := range k.Data {
		data[field] = value
	}
	data["fileName"] = name
	k.Data = data
	return k
}

// Preserve whitespace and case: the exact bytes are cryptographic input.
func (k Key) szpdFileNameBytes() ([]byte, error) {
	name, hasName := k.Data["fileName"]
	hexName, hasHex := k.Data["fileNameHex"]
	if hasName && hasHex {
		return nil, errors.New("fileName 和 fileNameHex 只能填写一种")
	}
	if !hasName && !hasHex {
		return nil, nil
	} // Bind the selected video's original name later.
	var data []byte
	if hasHex {
		raw, ok := hexName.(string)
		if !ok || len(raw) > 65536 {
			return nil, errors.New("fileNameHex 必须是有效的文件名字节十六进制")
		}
		var err error
		data, err = hex.DecodeString(raw)
		if err != nil {
			return nil, errors.New("fileNameHex 必须是有效的文件名字节十六进制")
		}
	} else {
		raw, ok := name.(string)
		if !ok || !utf8.ValidString(raw) {
			return nil, errors.New("fileName 必须是有效的 UTF-8 字符串")
		}
		data = []byte(raw)
	}
	if len(data) == 0 || len(data) > 32768 || bytes.IndexByte(data, 0) >= 0 {
		return nil, errors.New("派生文件名必须为 1–32768 字节，且不能包含空字节")
	}
	return data, nil
}

type szpd582Header struct {
	plainLen, cipherLen int64
	salt                [16]byte
}

func readSZPD582Header(src io.ReaderAt, size int64) (szpd582Header, error) {
	var result szpd582Header
	var head [0x24]byte
	if _, err := src.ReadAt(head[:], 0); err != nil {
		return result, fmt.Errorf("SZPD 文件头不完整：%w", err)
	}
	if string(head[:4]) != "SZPD" || binary.LittleEndian.Uint32(head[4:8]) != 582 {
		return result, errors.New("不是 SZPD 582 容器")
	}
	result.plainLen = int64(binary.LittleEndian.Uint32(head[12:16]))
	result.cipherLen = int64(binary.LittleEndian.Uint32(head[16:20]))
	if result.cipherLen == 0 || result.cipherLen%aes.BlockSize != 0 || size < 0x24 || result.cipherLen > size-0x24 || result.plainLen >= result.cipherLen || result.cipherLen-result.plainLen > aes.BlockSize {
		return result, errors.New("SZPD 长度字段无效或文件尚未下载完整")
	}
	copy(result.salt[:], head[20:36])
	return result, nil
}

// This implements the follow-up report's hypothesis, not a claim about every
// player build: SHA256(name || fill*16), then SHA256(digest || LE32(1)).
func deriveSZPD582(name, salt []byte, fill byte) ([]byte, []byte) {
	h := sha256.New()
	h.Write(name)
	h.Write(bytes.Repeat([]byte{fill}, 16))
	material := h.Sum(nil)
	constant := []byte{1, 0, 0, 0}
	key := sha256.Sum256(append(material, constant...))
	h.Reset()
	h.Write(salt)
	h.Write(constant)
	return key[:], h.Sum(nil)[:16]
}

// Screen only a bounded prefix and the last CBC block for each of 256 fills.
// Reading/decrypting an entire multi-GB file 256 times is unnecessary. Full
// decryption and FFmpeg validation remain mandatory after a unique match.
func recoverSZPD582Parameters(ctx context.Context, src io.ReaderAt, size int64, k Key, progress func(float64, float64)) ([]byte, []byte, error) {
	header, err := readSZPD582Header(src, size)
	if err != nil {
		return nil, nil, err
	}
	name, err := k.szpdFileNameBytes()
	if err != nil {
		return nil, nil, err
	}
	if len(name) == 0 {
		return nil, nil, errors.New("SZPD 派生缺少原始文件名")
	}
	prefix := make([]byte, min(header.cipherLen, 512))
	if _, err := src.ReadAt(prefix, 0x24); err != nil {
		return nil, nil, err
	}
	tail := make([]byte, min(header.cipherLen, 32))
	if _, err := src.ReadAt(tail, 0x24+header.cipherLen-int64(len(tail))); err != nil {
		return nil, nil, err
	}
	var foundKey, foundIV []byte
	matches := 0
	for fill := 0; fill < 256; fill++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		key, iv := deriveSZPD582(name, header.salt[:], byte(fill))
		block, _ := aes.NewCipher(key)
		decoded := make([]byte, len(prefix))
		cipher.NewCBCDecrypter(block, iv).CryptBlocks(decoded, prefix)
		if szpdMediaHeader(decoded[:min(int64(len(decoded)), header.plainLen)], header.plainLen) {
			lastIV := iv
			if len(tail) == 32 {
				lastIV = tail[:16]
			}
			last := make([]byte, 16)
			cipher.NewCBCDecrypter(block, lastIV).CryptBlocks(last, tail[len(tail)-16:])
			padding := int(header.cipherLen - header.plainLen)
			if bytes.Equal(last[len(last)-padding:], bytes.Repeat([]byte{byte(padding)}, padding)) {
				matches++
				foundKey, foundIV = key, iv
			}
		}
		if progress != nil {
			progress(float64(fill+1), 256)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if matches == 0 {
		return nil, nil, errors.New("256 个填充值均未通过筛选；请核对原始文件名及其编码，或使用真实 aesKey/aesIV；该派生方案尚未经过真实播放器验证")
	}
	if matches != 1 {
		return nil, nil, errors.New("SZPD 派生出现多个候选，无法可靠确定密钥，请提供真实 aesKey/aesIV")
	}
	return foundKey, foundIV, nil
}

func szpdMediaHeader(data []byte, size int64) bool {
	if len(data) >= 12 && string(data[4:8]) == "ftyp" {
		boxSize := int64(binary.BigEndian.Uint32(data[:4]))
		return boxSize >= 16 && boxSize%4 == 0 && boxSize <= size
	}
	if len(data) >= 4 && bytes.Equal(data[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}) {
		return true
	}
	if len(data) >= 9 && string(data[:4]) == "FLV\x01" {
		offset := int64(binary.BigEndian.Uint32(data[5:9]))
		return data[4]&^byte(5) == 0 && offset >= 9 && offset <= size
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "AVI " {
		return true
	}
	return len(data) > 376 && data[0] == 0x47 && data[188] == 0x47 && data[376] == 0x47
}
