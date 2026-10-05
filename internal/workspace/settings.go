package workspace

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time.haomen/binggan/v2/internal/engine"
	"time.haomen/binggan/v2/internal/procutil"
	"time.haomen/binggan/v2/internal/store"
	"time"
	"unicode/utf8"
)

type PlayerSettings struct {
	Mode         string `json:"mode" binding:"required,oneof=auto manual"`
	SoftwareName string `json:"softwareName" binding:"max=100"`
	AppMD5       string `json:"appMd5" binding:"max=32"`
}

func (a *App) Settings() (PlayerSettings, error) {
	p := PlayerSettings{Mode: "auto"}
	err := a.Store.GetSetting("player", &p)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return p, err
}

var errInvalidSettings = errors.New("请填写有效的 App 版本和 32 位校验值")

func (a *App) SaveSettings(p PlayerSettings) (PlayerSettings, error) {
	p.SoftwareName = strings.TrimSpace(p.SoftwareName)
	p.AppMD5 = strings.ToLower(strings.TrimSpace(p.AppMD5))
	if (p.Mode != "auto" && p.Mode != "manual") || utf8.RuneCountInString(p.SoftwareName) > 100 || len(p.AppMD5) > 32 ||
		p.Mode == "manual" && (p.SoftwareName == "" || !engine.HashValid(p.AppMD5)) {
		return PlayerSettings{}, errInvalidSettings
	}
	if p.Mode == "auto" {
		p.SoftwareName = ""
		p.AppMD5 = ""
	}
	if err := a.Store.SetSetting("player", p); err != nil {
		return PlayerSettings{}, err
	}
	return p, nil
}

type ConnectionStatus struct {
	Active              Connection `json:"active"`
	Saved               Connection `json:"saved"`
	TokenConfigured     bool       `json:"tokenConfigured"`
	RestartRequired     bool       `json:"restartRequired"`
	CaptureProxyAddress string     `json:"captureProxyAddress"`
	CaptureProxyReady   bool       `json:"captureProxyReady"`
}

func (a *App) Connection() (ConnectionStatus, error) {
	active := Connection{"sing-box", a.Config.Host, a.Config.Port}
	saved := active
	err := a.Store.GetSetting("connection", &saved)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ConnectionStatus{}, err
	}
	// Migrate any retired connection method without modifying historical captures.
	if saved.Provider != active.Provider || saved.Host != active.Host || saved.Port != active.Port {
		saved = active
		if err := a.Store.SetSetting("connection", saved); err != nil {
			return ConnectionStatus{}, err
		}
	}
	proxyAddress := ""
	proxyReady := false
	if a.collector != nil {
		a.collector.mu.Lock()
		if a.collector.relay != nil {
			proxyAddress = a.collector.relay.Address
		}
		a.collector.mu.Unlock()
		proxyReady = a.collector.ready.Load()
	}
	return ConnectionStatus{active, saved, a.Config.Token != "", false, proxyAddress, proxyReady}, nil
}

// DiagnosticCheck is one row of the environment page. Rows are produced grouped
// by Group, in display order.
type DiagnosticCheck struct {
	Group   string `json:"group"`
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type Diagnosis struct {
	CheckedAt                   string            `json:"checkedAt"`
	Go                          string            `json:"go"`
	Player                      *engine.Player    `json:"player"`
	KnownPlayers                map[string]string `json:"knownPlayers"`
	Checks                      []DiagnosticCheck `json:"checks"`
	EnvironmentAppMD5Configured bool              `json:"environmentAppMd5Configured"`
}

func (a *App) Diagnose(parent context.Context) (Diagnosis, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	var checks []DiagnosticCheck
	add := func(group, name string, err error, ok string) {
		msg := ok
		if err != nil {
			msg = err.Error()
		}
		checks = append(checks, DiagnosticCheck{Group: group, Name: name, OK: err == nil, Message: msg})
	}

	type checkResult struct {
		name    string
		err     error
		message string
	}
	results := make([]checkResult, 4)
	var wg sync.WaitGroup
	for i, name := range []string{"ffmpeg", "ffprobe"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.CommandContext(ctx, name, "-version")
			procutil.HideWindow(cmd)
			out, err := cmd.Output()
			fields := strings.Fields(strings.SplitN(string(out), "\n", 2)[0])
			version := ""
			if len(fields) > 2 {
				version = fields[2]
			}
			label := "音视频处理 FFmpeg"
			if name == "ffprobe" {
				label = "媒体检测 FFprobe"
			}
			results[i] = checkResult{label, err, version}
		}()
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		var version string
		err := a.Store.DB.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version)
		results[2] = checkResult{"SQLite 数据库", err, version}
	}()
	go func() {
		defer wg.Done()
		f, err := os.CreateTemp(a.Config.DataDir, ".write-test-*")
		if err == nil {
			name := f.Name()
			closeErr := f.Close()
			err = os.Remove(name)
			if err == nil {
				err = closeErr
			}
		}
		results[3] = checkResult{"任务与日志存储", err, "可写入"}
	}()
	var p engine.Player
	var playerErr error
	wg.Add(1)
	go func() { defer wg.Done(); p, playerErr = engine.DetectPlayer() }()
	wg.Wait()
	add("运行环境", "Go 运行环境", nil, runtime.Version())
	add("运行环境", "处理器架构", nil, architectureName(ctx))
	add("音视频", results[0].name, results[0].err, results[0].message)
	add("音视频", results[1].name, results[1].err, results[1].message)
	add("网络采集", "sing-box", nil, singBoxVersion())
	add("数据存储", results[2].name, results[2].err, results[2].message)
	add("数据存储", results[3].name, results[3].err, results[3].message)
	add("解密", "Go 解密引擎", nil, "AES / RC4 / MP4 / 音频频谱")
	var player *engine.Player
	err := playerErr
	var unavailable *engine.PlayerParametersUnavailable
	if errors.As(err, &unavailable) {
		location := unavailable.Installation.Path
		if unavailable.Installation.Version != "" {
			location += " · " + unavailable.Installation.Version
		}
		add("解密", "播放器安装位置", nil, location)
	}
	if err == nil {
		player = &p
	}
	add("解密", "播放器自动识别", err, p.SoftwareName)
	return Diagnosis{store.Now(), runtime.Version(), player, engine.KnownPlayers, checks, engine.HashValid(a.Config.AppMD5)}, nil
}
func (a *App) keyHash(k engine.Key) (string, error) {
	s, err := a.Settings()
	if err != nil {
		return "", err
	}
	var p *engine.Player
	if s.Mode == "manual" {
		p = &engine.Player{SoftwareName: s.SoftwareName, AppMD5: s.AppMD5}
	}
	h, err := engine.ResolveHash(k.Data, a.Config.AppMD5, p)
	if err != nil {
		// Plain mima keys do not require the application's AES derivation hash.
		if _, plainErr := engine.Mima(k.Data, ""); plainErr == nil {
			return "", nil
		}
	}
	return h, err
}
func EnsureDirectories(root string) error {
	for _, dir := range []string{"input", "keys"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			return err
		}
	}
	return nil
}
