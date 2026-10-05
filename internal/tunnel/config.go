package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	boxservice "github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/dns/transport/local"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/direct"
	"github.com/sagernet/sing-box/protocol/socks"
	"github.com/sagernet/sing-box/protocol/tun"
	boxjson "github.com/sagernet/sing/common/json"
	"net"
	"path/filepath"
	"strconv"
	"time.haomen/binggan/v2/internal/capture"
	"time.haomen/binggan/v2/internal/engine"
)

// The elevated helper may start in / or another read-only working directory.
// sing-box enables its cache whenever PlatformLogWriter is present, even when
// cache_file.enabled is omitted, so its path must always be explicit here.
func helperOptions(d capture.Descriptor, runtimeDir string) (context.Context, option.Options, error) {
	ctx, opts, err := Options(d)
	if err != nil {
		return ctx, opts, err
	}
	if !filepath.IsAbs(runtimeDir) {
		return ctx, opts, fmt.Errorf("流量接管临时目录必须使用绝对路径")
	}
	opts.Experimental = &option.ExperimentalOptions{CacheFile: &option.CacheFileOptions{
		Enabled: true,
		Path:    filepath.Join(runtimeDir, "cache.db"),
	}}
	return ctx, opts, nil
}

func Options(d capture.Descriptor) (context.Context, option.Options, error) {
	ctx := Context(context.Background())
	raw, e := ConfigJSON(d)
	if e != nil {
		return ctx, option.Options{}, e
	}
	opts, e := boxjson.UnmarshalExtendedContext[option.Options](ctx, raw)
	return ctx, opts, e
}
func Context(ctx context.Context) context.Context {
	ins := inbound.NewRegistry()
	tun.RegisterInbound(ins)
	outs := outbound.NewRegistry()
	direct.RegisterOutbound(outs)
	socks.RegisterOutbound(outs)
	ds := dns.NewTransportRegistry()
	local.RegisterTransport(ds)
	return box.Context(ctx, ins, outs, endpoint.NewRegistry(), ds, boxservice.NewRegistry(), certificate.NewRegistry())
}

// captureProcesses lists the only executables whose traffic is sent to the
// capture backend. Everything else stays direct so unrelated system traffic
// never reaches the receiver or floods the log window.
var captureProcesses = []string{"SzPlayer", "szplayer", "SzPlayer.exe", "szplayer.exe"}

func ConfigJSON(d capture.Descriptor) ([]byte, error) {
	if e := d.Validate(); e != nil {
		return nil, e
	}
	host, port, e := net.SplitHostPort(d.ProxyAddress)
	if e != nil {
		return nil, e
	}
	n, e := strconv.Atoi(port)
	if e != nil || n < 1 || n > 65535 {
		return nil, fmt.Errorf("抓包端口无效")
	}
	rules := []any{
		map[string]any{"ip_cidr": []string{"127.0.0.0/8", "::1/128"}, "action": "route", "outbound": "direct"},
		map[string]any{"process_path": []string{d.BackendExecutable}, "action": "route", "outbound": "direct"},
		map[string]any{"process_name": captureProcesses, "action": "route", "outbound": "capture"},
		map[string]any{"process_path_regex": []string{engine.WindowsPlayerPathPattern}, "action": "route", "outbound": "capture"},
	}
	// Native discovery covers registered/custom portable directories. The bounded
	// vendor-directory rule above also works if the player opens after this helper.
	var paths []string
	for _, installation := range engine.WindowsPlayerInstallations() {
		paths = append(paths, installation.Path)
	}
	if len(paths) > 0 {
		rules = append(rules, map[string]any{"process_path": paths, "action": "route", "outbound": "capture"})
	}
	m := map[string]any{
		"log":       map[string]any{"level": "warn", "timestamp": true, "disabled": false},
		"dns":       map[string]any{"servers": []any{map[string]any{"type": "local", "tag": "local"}}, "final": "local"},
		"inbounds":  []any{map[string]any{"type": "tun", "tag": "app-traffic", "address": []string{"172.29.255.1/30", "fd6a:73:7a::1/126"}, "auto_route": true, "strict_route": false, "dns_mode": "disabled", "stack": "system", "mtu": 1500}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}, map[string]any{"type": "socks", "version": "5", "tag": "capture", "server": host, "server_port": n, "username": d.ProxyUsername, "password": d.ProxyPassword}},
		"route":     map[string]any{"auto_detect_interface": true, "default_domain_resolver": "local", "final": "direct", "rules": rules},
	}
	return json.MarshalIndent(m, "", "  ")
}
