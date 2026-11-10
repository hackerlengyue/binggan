package engine

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Explicit AES inputs remain supported independently of any derivation theory.
// Filename/fill derivation is opt-in: the follow-up report has synthetic tests,
// but no real-player sample confirming its interpretation of the fill argument.
func (k Key) IsSZPD() bool { return str(k.Data["pattern"]) == "szpd" }

func (k Key) szpdParameters() ([]byte, []byte, error) {
	key, e1 := hex.DecodeString(str(k.Data["aesKey"]))
	iv, e2 := hex.DecodeString(str(k.Data["aesIV"]))
	if e1 != nil || e2 != nil || len(key) != 32 || len(iv) != aes.BlockSize {
		return nil, nil, errors.New("SZPD 需要 aesKey（64 位十六进制）和 aesIV（32 位十六进制），请提供与源文件对应的 AES 参数")
	}
	return key, iv, nil
}

// ValidateInputKey checks the actual file signature before any app-hash lookup
// or MP4 sample parsing. The same preflight is used by queued and direct runs.
func ValidateInputKey(path string, k Key) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var head [8]byte
	n, err := io.ReadFull(f, head[:])
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return err
	}
	if n >= 4 && string(head[:4]) == "SZPD" {
		if n < 8 {
			return errors.New("SZPD 文件头不完整")
		}
		version := binary.LittleEndian.Uint32(head[4:])
		if version != 582 {
			return fmt.Errorf("尚不支持 SZPD %d 视频容器；326 多流格式仍需实测确认", version)
		}
		if !k.IsSZPD() {
			return errors.New("检测到 Windows SZPD 582 容器，请导入 pattern 为 szpd 且包含 aesKey/aesIV 的密钥 JSON；接口播放密码不能代替容器 AES 参数")
		}
	} else if k.IsSZPD() {
		return errors.New("SZPD 密钥只能用于 SZPD 582 容器，不能用于 MP4 采样加密视频")
	}
	return nil
}

// decryptSZPD582 bounds memory independently of file size and keeps the final
// block for strict PKCS#7 and declared plaintext-length checks.
func decryptSZPD582(ctx context.Context, src io.ReaderAt, size int64, dst io.Writer, key, iv []byte, progress func(float64, float64)) error {
	header, err := readSZPD582Header(src, size)
	if err != nil {
		return err
	}
	cipherLen, plainLen := header.cipherLen, header.plainLen
	block, err := aes.NewCipher(key)
	if err != nil || len(key) != 32 || len(iv) != aes.BlockSize {
		return errors.New("SZPD AES-256-CBC 参数无效")
	}
	mode := cipher.NewCBCDecrypter(block, iv)
	buf := make([]byte, 1<<20)
	for offset := int64(0); offset < cipherLen; {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := min(int64(len(buf)), cipherLen-offset)
		data := buf[:int(n)]
		if _, err := src.ReadAt(data, 0x24+offset); err != nil {
			return err
		}
		mode.CryptBlocks(data, data)
		offset += n
		if offset == cipherLen {
			padding := int(data[len(data)-1])
			if padding < 1 || padding > aes.BlockSize || !bytes.Equal(data[len(data)-padding:], bytes.Repeat([]byte{byte(padding)}, padding)) || cipherLen-int64(padding) != plainLen {
				return errors.New("SZPD AES 参数不匹配或密文损坏（填充或明文长度校验失败）")
			}
			data = data[:len(data)-padding]
		}
		if n, err := dst.Write(data); err != nil {
			return err
		} else if n != len(data) {
			return io.ErrShortWrite
		}
		if progress != nil {
			progress(float64(offset), float64(cipherLen))
		}
	}
	return ctx.Err()
}

func runSZPD(ctx context.Context, input, outputPath string, k Key, log LogFunc, progress ProgressFunc) (result map[string]any, err error) {
	started := time.Now()
	stageStart := started
	stage := "decrypt"
	defer func() {
		if err != nil {
			err = fmt.Errorf("%s失败：%w", StageName(stage), err)
		}
	}()
	last := time.Time{}
	emit := func(done, total float64) {
		if time.Since(last) < 500*time.Millisecond && done < total {
			return
		}
		last = time.Now()
		progress(Progress{Stage: stage, Done: done, Total: total, Elapsed: time.Since(started).Seconds(), StageElapsed: time.Since(stageStart).Seconds()})
	}
	src, err := os.Open(input)
	if err != nil {
		return nil, err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return nil, err
	}
	var key, iv []byte
	keySource := "provided"
	if k.UsesSZPDDerivation() {
		stage = "derive"
		emit(0, 256)
		log("INFO", "正在尝试 SZPD 文件名派生方案 · 该方案尚待真实播放器样本验证")
		key, iv, err = recoverSZPD582Parameters(ctx, src, info.Size(), k.WithInputName(filepath.Base(input)), emit)
		if err != nil {
			return nil, err
		}
		keySource = szpdFilenameFillDerivation
		stage, stageStart, last = "decrypt", time.Now(), time.Time{}
		log("INFO", "SZPD 派生候选通过媒体头和尾部填充筛选 · 开始完整解密与解码校验")
	} else {
		key, iv, err = k.szpdParameters()
		if err != nil {
			return nil, err
		}
	}
	// A private directory in the destination filesystem allows atomic publication
	// and closes every file before FFmpeg or Rename opens it on Windows.
	dir, err := os.MkdirTemp(filepath.Dir(outputPath), ".szpd-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	plain := filepath.Join(dir, "plain.media")
	f, err := os.OpenFile(plain, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	log("INFO", "检测到 SZPD 582 · 使用文件对应的 AES-256-CBC 参数解密")
	err = decryptSZPD582(ctx, src, info.Size(), f, key, iv, emit)
	closeErr := f.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	stage, stageStart, last = "remux", time.Now(), time.Time{}
	emit(0, 1)
	candidate := filepath.Join(dir, "output.mp4")
	log("INFO", "容器解密完成 · 正在封装 MP4，保留视频和音频编码")
	if err = runMedia(ctx, log, 1, emit, "-nostdin", "-v", "error", "-xerror", "-protocol_whitelist", "file,pipe", "-i", plain, "-map", "0:v:0", "-map", "0:a?", "-c", "copy", "-movflags", "+faststart", "-n", candidate); err != nil {
		return nil, err
	}
	stage, stageStart, last = "validate", time.Now(), time.Time{}
	emit(0, 1)
	if err = runMedia(ctx, log, 1, emit, "-nostdin", "-v", "error", "-xerror", "-protocol_whitelist", "file,pipe", "-i", candidate, "-map", "0:v", "-map", "0:a?", "-f", "null", "-"); err != nil {
		return nil, fmt.Errorf("SZPD 完整媒体解码校验失败：%w", err)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	info, err = os.Stat(candidate)
	if err != nil {
		return nil, err
	}
	if err = os.Rename(candidate, outputPath); err != nil {
		return nil, err
	}
	stage, stageStart = "complete", time.Now()
	emit(1, 1)
	log("INFO", "SZPD 解密、MP4 封装和完整媒体解码校验通过")
	return map[string]any{"engine": "go", "format": "SZPD", "version": 582, "key_source": keySource, "size": info.Size(), "elapsed_sec": time.Since(started).Seconds(), "check": map[string]any{"valid": true, "full_decode": true}}, nil
}
