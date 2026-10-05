package main

import (
	"context"
	"fmt"

	"time.haomen/binggan/v2/internal/tunnel"
)

// CaptureService exposes the native capture and certificate operations.
type CaptureService struct{ app *Workbench }

func (s *CaptureService) monitor(ctx context.Context) (*Monitor, error) {
	if err := s.app.wait(ctx); err != nil {
		return nil, err
	}
	if s.app.monitor == nil {
		return nil, fmt.Errorf("流量采集未启动：%v", s.app.startErr)
	}
	return s.app.monitor, nil
}

func (s *CaptureService) State(ctx context.Context) (State, error) {
	m, err := s.monitor(ctx)
	if err != nil {
		return State{}, err
	}
	return m.GetState(), nil
}

func (s *CaptureService) Retry(ctx context.Context) error {
	m, err := s.monitor(ctx)
	if err != nil {
		return err
	}
	if err := s.app.app.StartCollector(); err != nil {
		return err
	}
	m.Retry()
	return nil
}

func (s *CaptureService) ClearLogs(ctx context.Context) error {
	m, err := s.monitor(ctx)
	if err != nil {
		return err
	}
	return m.ClearLogs()
}

func (s *CaptureService) Certificate(ctx context.Context) (tunnel.CertificateState, error) {
	m, err := s.monitor(ctx)
	if err != nil {
		return tunnel.CertificateState{}, err
	}
	return m.GetCertificateState()
}

func (s *CaptureService) Install(ctx context.Context) (tunnel.CertificateState, error) {
	m, err := s.monitor(ctx)
	if err != nil {
		return tunnel.CertificateState{}, err
	}
	return m.InstallCertificate()
}

func (s *CaptureService) Trust(ctx context.Context) (tunnel.CertificateState, error) {
	m, err := s.monitor(ctx)
	if err != nil {
		return tunnel.CertificateState{}, err
	}
	return m.TrustCertificate()
}

func (s *CaptureService) Regenerate(ctx context.Context) (tunnel.CertificateState, error) {
	m, err := s.monitor(ctx)
	if err != nil {
		return tunnel.CertificateState{}, err
	}
	return m.RegenerateCertificate()
}
