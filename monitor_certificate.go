package main

import (
	"context"
	"fmt"
	"net/http"

	"time.haomen/binggan/v2/internal/capture"
	"time.haomen/binggan/v2/internal/system"
	"time.haomen/binggan/v2/internal/tunnel"
)

// Fetch independently so opening the dialog does not wait for a TUN/UAC prompt.
func (a *Monitor) certificateDescriptor() (capture.Descriptor, error) {
	if a.ctx == nil {
		return capture.Descriptor{}, fmt.Errorf("程序正在启动，请稍后刷新")
	}
	config, err := a.receiverConfig()
	if err != nil {
		return capture.Descriptor{}, err
	}
	client, release := a.localClient()
	defer release()
	return capture.Fetch(a.ctx, client, config)
}

func (a *Monitor) receiverConfig() (capture.Config, error) {
	return a.config, a.config.Validate()
}

func (a *Monitor) localClient() (*http.Client, func()) {
	a.mu.RLock()
	client := a.client
	a.mu.RUnlock()
	if client != nil {
		return client, func() {}
	}
	client = capture.LocalClient()
	return client, client.CloseIdleConnections
}

func (a *Monitor) GetCertificateState() (tunnel.CertificateState, error) {
	a.certificateOp.Lock()
	defer a.certificateOp.Unlock()
	d, err := a.certificateDescriptor()
	if err != nil {
		return tunnel.CertificateState{}, err
	}
	return tunnel.CertificateStatus(a.ctx, d)
}

func (a *Monitor) InstallCertificate() (tunnel.CertificateState, error) {
	return a.changeCertificate("开始安装 CA 证书", "证书安装未完成", "证书已安装到系统", "请继续信任证书", tunnel.InstallCertificate)
}

func (a *Monitor) TrustCertificate() (tunnel.CertificateState, error) {
	return a.changeCertificate("开始信任 CA 证书", "证书信任未完成", "证书已被系统信任", "系统信任校验通过，即将继续启动", tunnel.TrustCertificate)
}

func (a *Monitor) changeCertificate(start, failed, done, detail string, act func(context.Context, capture.Descriptor, system.CertificateProgress) (tunnel.CertificateState, error)) (tunnel.CertificateState, error) {
	a.op.Lock()
	defer a.op.Unlock()
	a.certificateOp.Lock()
	defer a.certificateOp.Unlock()
	if a.closed || a.logs == nil {
		return tunnel.CertificateState{}, fmt.Errorf("程序尚未就绪，请稍后重试")
	}
	a.logs.AddCertificate("info", start, "正在读取本机后端证书")
	d, err := a.certificateDescriptor()
	if err != nil {
		a.logs.AddCertificate("error", failed, err.Error())
		return tunnel.CertificateState{}, err
	}
	state, err := act(a.ctx, d, func(message, detail string) { a.logs.AddCertificate("info", message, detail) })
	if err != nil {
		a.logs.AddCertificate("error", failed, err.Error())
		return state, err
	}
	a.logs.AddCertificate("info", done, detail)
	if a.lease == nil {
		a.blocked = false
	}
	a.signal()
	return state, nil
}

func (a *Monitor) RegenerateCertificate() (tunnel.CertificateState, error) {
	a.op.Lock()
	defer a.op.Unlock()
	a.certificateOp.Lock()
	defer a.certificateOp.Unlock()
	if a.closed || a.ctx == nil || a.logs == nil {
		return tunnel.CertificateState{}, fmt.Errorf("程序尚未就绪，请稍后重试")
	}
	a.logs.AddCertificate("info", "开始生成新证书", "正在请求本机后端更换 CA")
	config, err := a.receiverConfig()
	if err != nil {
		a.logs.AddCertificate("error", "新证书未生成", err.Error())
		return tunnel.CertificateState{}, err
	}
	client, release := a.localClient()
	defer release()
	d, err := capture.Regenerate(a.ctx, client, config)
	if err != nil {
		a.logs.AddCertificate("error", "新证书未生成", err.Error())
		return tunnel.CertificateState{}, err
	}
	state, err := tunnel.CertificateStatus(a.ctx, d)
	if err != nil {
		a.logs.AddCertificate("error", "新证书未生成", err.Error())
		return tunnel.CertificateState{}, err
	}
	a.logs.AddCertificate("info", "新证书已生成", "请先安装到系统，再信任证书")
	if a.lease == nil {
		a.blocked = false
	}
	a.signal()
	return state, nil
}
