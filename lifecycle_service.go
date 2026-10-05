package main

import (
	"sync"
	"time"

	"github.com/egoist/mygo"
)

var QuitRequested = mygo.NewEvent[uint64]("desktop:quit-requested")

// LifecycleService keeps the renderer alive until its capture journal is saved.
type LifecycleService struct {
	mu                         sync.Mutex
	ready, approved, resolving bool
	sequence, pending          uint64
	timer                      *time.Timer
	request                    func(uint64)
	quit                       func()
	report                     func(string, func())
	reveal                     func()
}

func newLifecycleService() *LifecycleService {
	s := &LifecycleService{quit: mygo.App.Quit}
	s.request = func(id uint64) { _ = QuitRequested.Broadcast(id) }
	s.report = func(detail string, force func()) {
		result, err := mygo.Dialog.Message(mygo.MessageOptions{
			Type: mygo.MessageWarning, Title: "退出前未能完成收尾",
			Message: "暂停播放或保存记录尚未确认完成", Detail: detail + "\n仍然退出可能丢失未保存内容，SzPlayer 也可能继续播放。",
			Buttons: []string{"留下处理", "仍然退出"}, DefaultButton: 0, CancelButton: 0,
		})
		if err == nil && result.Button == 1 {
			force()
		}
	}
	return s
}

func (s *LifecycleService) RendererReady() { s.mu.Lock(); s.ready = true; s.mu.Unlock() }

// allowQuit handles explicit Quit requests, never an ordinary window close.
func (s *LifecycleService) allowQuit() bool {
	s.mu.Lock()
	if s.approved || !s.ready {
		s.approved = true
		s.mu.Unlock()
		return true
	}
	if s.pending != 0 {
		s.mu.Unlock()
		return false
	}
	s.sequence++
	id := s.sequence
	s.pending = id
	s.resolving = false
	s.timer = time.AfterFunc(30*time.Second, func() { s.FinishQuit(id, "收尾超过 30 秒，请检查当前任务和保存状态。") })
	s.mu.Unlock()
	s.request(id)
	return false
}

// Only an explicit Quit that passed cleanup may destroy the main window.
func (s *LifecycleService) windowMayClose() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.approved
}

// FinishQuit ignores stale acknowledgements, including a late timeout result.
func (s *LifecycleService) FinishQuit(id uint64, failure string) {
	s.mu.Lock()
	if id == 0 || s.pending != id || s.resolving {
		s.mu.Unlock()
		return
	}
	s.resolving = true
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	// Hold pending while the native warning is open, so repeated Quit cannot race it.
	if failure != "" {
		s.mu.Unlock()
		go func() {
			if s.reveal != nil {
				s.reveal()
			}
			s.report(failure, func() { s.approve(id) })
			s.mu.Lock()
			if s.pending == id {
				s.pending = 0
			}
			s.mu.Unlock()
		}()
		return
	}
	s.approved = true
	s.pending = 0
	s.mu.Unlock()
	s.quit()
}

func (s *LifecycleService) approve(id uint64) {
	s.mu.Lock()
	if s.pending != id {
		s.mu.Unlock()
		return
	}
	s.approved = true
	s.pending = 0
	s.mu.Unlock()
	s.quit()
}

func (*LifecycleService) Relaunch() { mygo.App.Relaunch() }
func (*LifecycleService) Quit()     { mygo.App.Quit() }
