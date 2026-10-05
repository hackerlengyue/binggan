package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func WindowsPlayerInstallations() []Installation {
	var items []Installation
	add := func(path string, trusted bool) {
		version, product := windowsPlayerMetadata(path)
		identity := strings.ToLower(product)
		trusted = trusted || strings.Contains(identity, "szplayer") || strings.Contains(product, "深造")
		items = appendInstallation(items, path, version, trusted)
	}
	// An explicit path also supports renamed portable directories and is read only.
	for _, path := range playerPathsAt(os.Getenv("SZJM_PLAYER_PATH")) {
		add(path, true)
	}
	// Running processes locate portable players on any drive without a disk scan.
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err == nil {
		defer windows.CloseHandle(snapshot)
		entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
		for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
			if !windowsPlayerName(windows.UTF16ToString(entry.ExeFile[:])) {
				continue
			}
			handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, entry.ProcessID)
			if err != nil {
				continue
			}
			buf := make([]uint16, 32768)
			size := uint32(len(buf))
			err = windows.QueryFullProcessImageName(handle, 0, &buf[0], &size)
			windows.CloseHandle(handle)
			if err == nil && size > 0 && size < uint32(len(buf)) {
				add(windows.UTF16ToString(buf[:size]), false)
			}
		}
	}
	for _, hive := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		for _, view := range []uint32{registry.WOW64_64KEY, registry.WOW64_32KEY} {
			for _, name := range []string{"SzPlayer.exe", "MainPlayer.exe"} {
				key, err := registry.OpenKey(hive, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\`+name, registry.QUERY_VALUE|view)
				if err != nil {
					continue
				}
				value, _, err := key.GetStringValue("")
				key.Close()
				if err == nil {
					add(executableFromRegistry(expandWindowsEnvironment(value)), false)
				}
			}
			key, err := registry.OpenKey(hive, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`, registry.READ|view)
			if err != nil {
				continue
			}
			names, _ := key.ReadSubKeyNames(-1)
			for _, name := range names {
				sub, err := registry.OpenKey(key, name, registry.QUERY_VALUE)
				if err != nil {
					continue
				}
				display, _, _ := sub.GetStringValue("DisplayName")
				if !strings.Contains(strings.ToLower(display), "szplayer") && !strings.Contains(display, "深造播放器") {
					sub.Close()
					continue
				}
				location, _, _ := sub.GetStringValue("InstallLocation")
				icon, _, _ := sub.GetStringValue("DisplayIcon")
				sub.Close()
				for _, p := range playerPathsAt(expandWindowsEnvironment(location)) {
					add(p, true)
				}
				add(executableFromRegistry(expandWindowsEnvironment(icon)), true)
			}
			key.Close()
		}
	}
	home, _ := os.UserHomeDir()
	roots := []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramW6432")}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		roots = append(roots, local, filepath.Join(local, "Programs"))
	}
	if home != "" {
		roots = append(roots, filepath.Join(home, "Desktop"), filepath.Join(home, "Downloads"))
	}
	if oneDrive := os.Getenv("OneDrive"); oneDrive != "" {
		roots = append(roots, filepath.Join(oneDrive, "Desktop"))
	}
	for _, path := range playerDirectoryCandidates(roots) {
		add(path, false)
	}
	return items
}

func expandWindowsEnvironment(value string) string {
	result, err := registry.ExpandString(value)
	if err != nil {
		return value
	}
	return result
}

func windowsPlayerMetadata(path string) (string, string) {
	var handle windows.Handle
	size, err := windows.GetFileVersionInfoSize(path, &handle)
	if err != nil || size == 0 || size > 4<<20 {
		return "", ""
	}
	data := make([]byte, size)
	if windows.GetFileVersionInfo(path, 0, size, unsafe.Pointer(&data[0])) != nil {
		return "", ""
	}
	defer runtime.KeepAlive(data)
	var translations *uint16
	var length uint32
	if windows.VerQueryValue(unsafe.Pointer(&data[0]), `\VarFileInfo\Translation`, unsafe.Pointer(&translations), &length) != nil || translations == nil || length < 4 {
		return "", ""
	}
	start := uintptr(unsafe.Pointer(&data[0]))
	end := start + uintptr(len(data))
	ptr := uintptr(unsafe.Pointer(translations))
	if ptr < start || ptr+uintptr(length) > end {
		return "", ""
	}
	pairs := unsafe.Slice(translations, length/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		query := func(name string) string { return windowsVersionString(data, pairs[i], pairs[i+1], name) }
		version, product := query("ProductVersion"), query("ProductName")
		if version == "" {
			version = query("FileVersion")
		}
		if version != "" || product != "" {
			return version, product
		}
	}
	return "", ""
}

func windowsVersionString(data []byte, language, codepage uint16, name string) string {
	var value *uint16
	var length uint32
	key := fmt.Sprintf(`\StringFileInfo\%04x%04x\%s`, language, codepage, name)
	if windows.VerQueryValue(unsafe.Pointer(&data[0]), key, unsafe.Pointer(&value), &length) != nil || value == nil || length == 0 {
		return ""
	}
	start := uintptr(unsafe.Pointer(&data[0]))
	ptr := uintptr(unsafe.Pointer(value))
	if ptr < start || ptr+uintptr(length)*2 > start+uintptr(len(data)) {
		return ""
	}
	return strings.TrimSpace(windows.UTF16ToString(unsafe.Slice(value, length)))
}
