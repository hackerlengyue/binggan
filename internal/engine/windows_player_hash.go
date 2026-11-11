package engine

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

// Installations have already passed the vendor/path checks in discovery.
// Read each actual executable instead of looking up a particular build digest.
func detectWindowsPlayer(installations []Installation) (Player, error) {
	if len(installations) == 0 {
		return Player{}, errors.New("未找到 Windows 播放器，请先打开深造播放器，再重新检测")
	}
	var firstError error
	for _, installation := range installations {
		player, err := windowsPlayerParameters(installation)
		if err == nil {
			return player, nil
		}
		if firstError == nil {
			firstError = err
		}
	}
	return Player{}, firstError
}

func windowsPlayerParameters(installation Installation) (Player, error) {
	f, err := os.Open(installation.Path)
	if err != nil {
		return Player{}, fmt.Errorf("无法读取播放器文件：%w", err)
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return Player{}, err
	}
	if !before.Mode().IsRegular() {
		return Player{}, errors.New("播放器路径不是可读取的程序文件")
	}
	var signature [2]byte
	if _, err := io.ReadFull(f, signature[:]); err != nil || signature != [2]byte{'M', 'Z'} {
		return Player{}, errors.New("播放器程序文件无效，请重新检测")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Player{}, err
	}
	// One streaming pass reads all bytes, independently of the player process
	// and without applying the macOS MD5(hex(SHA256(file))) transformation.
	md5Hash := md5.New()
	read, err := io.Copy(md5Hash, f)
	if err != nil {
		return Player{}, fmt.Errorf("无法读取播放器文件：%w", err)
	}
	after, err := os.Stat(installation.Path)
	if err != nil {
		return Player{}, err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || read != before.Size() || !before.ModTime().Equal(after.ModTime()) {
		return Player{}, errors.New("播放器正在更新，请重新检测")
	}
	checksum := hex.EncodeToString(md5Hash.Sum(nil))
	return Player{SoftwareName: checksum, AppMD5: checksum}, nil
}
