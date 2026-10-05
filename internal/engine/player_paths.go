package engine

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Installation describes a located player, separately from verified decryption
// parameters. Windows PE file versions and executable digests are not themselves
// proof of the softwareName/appMd5 sent by the player.
type Installation struct {
	Path    string `json:"path"`
	Version string `json:"version,omitempty"`
}

type PlayerParametersUnavailable struct{ Installation Installation }

func (e *PlayerParametersUnavailable) Error() string {
	return "已找到 Windows 播放器，但尚无已验证的解密校验参数，请从相同版本的提取记录填入或手动配置"
}

// WindowsPlayerPathPattern also covers a player opened after the tunnel starts.
// MainPlayer is a generic name: never capture every MainPlayer.exe on the system.
const WindowsPlayerPathPattern = `(?i)[\\/]SZPlayer(?:[ _-][^\\/]*)?[\\/]MainPlayer\.exe$`

var windowsPlayerDirectory = regexp.MustCompile(`(?i)^SZPlayer(?:[ _-].*)?$`)

func windowsPlayerName(name string) bool {
	return strings.EqualFold(name, "SzPlayer.exe") || strings.EqualFold(name, "MainPlayer.exe")
}

func validWindowsPlayerPath(path string, trustedReference bool) bool {
	if !filepath.IsAbs(path) || !windowsPlayerName(filepath.Base(path)) {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	var signature [2]byte
	if _, err = f.Read(signature[:]); err != nil || signature != [2]byte{'M', 'Z'} {
		return false
	}
	if strings.EqualFold(filepath.Base(path), "SzPlayer.exe") {
		return true
	}
	for _, name := range []string{"Qt5Core.dll", "Qt5Network.dll"} {
		info, err := os.Stat(filepath.Join(filepath.Dir(path), name))
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	if !trustedReference && !windowsPlayerDirectory.MatchString(filepath.Base(filepath.Dir(path))) {
		// The Windows vendor bundle also contains these two specific companions.
		// This permits a renamed portable directory without treating any Qt-based
		// executable called MainPlayer as the capture target.
		for _, name := range []string{"pyxt.dll", "xldl.dll"} {
			info, err := os.Stat(filepath.Join(filepath.Dir(path), name))
			if err != nil || !info.Mode().IsRegular() {
				return false
			}
		}
	}
	return true
}

func playerPathsAt(root string) []string {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	if windowsPlayerName(filepath.Base(root)) {
		return []string{filepath.Clean(root)}
	}
	return []string{filepath.Join(root, "MainPlayer.exe"), filepath.Join(root, "SzPlayer.exe")}
}

// Parse registry App Paths, DisplayIcon and file association values as data.
// No shell, PowerShell, or target executable is ever launched for discovery.
func executableFromRegistry(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, `"`) {
		if end := strings.Index(value[1:], `"`); end >= 0 {
			return value[1 : end+1]
		}
		return ""
	}
	if end := strings.Index(strings.ToLower(value), ".exe"); end >= 0 {
		return strings.TrimSpace(value[:end+4])
	}
	return ""
}

func playerDirectoryCandidates(roots []string) []string {
	var paths []string
	for _, root := range roots {
		if root == "" || !filepath.IsAbs(root) {
			continue
		}
		paths = append(paths, playerPathsAt(root)...)
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || !windowsPlayerDirectory.MatchString(entry.Name()) {
				continue
			}
			paths = append(paths, playerPathsAt(filepath.Join(root, entry.Name()))...)
		}
	}
	return paths
}

func appendInstallation(items []Installation, path, version string, trusted bool) []Installation {
	path = filepath.Clean(path)
	if !validWindowsPlayerPath(path, trusted) {
		return items
	}
	for _, item := range items {
		if strings.EqualFold(item.Path, path) {
			return items
		}
	}
	return append(items, Installation{Path: path, Version: version})
}

func PlayerWasLocated(err error) bool {
	var unavailable *PlayerParametersUnavailable
	return errors.As(err, &unavailable)
}
