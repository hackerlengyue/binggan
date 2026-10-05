//go:build !darwin || !cgo

package playerautomation

import "runtime"

type unsupportedDriver struct{}

func newDriver() Driver { return &unsupportedDriver{} }
func (d *unsupportedDriver) Status() Status {
	result := platformStatus()
	result.Message = "播放编排的原生控件驱动尚未在 " + runtime.GOOS + " 实机验证"
	return result
}
func (d *unsupportedDriver) Snapshot() (Snapshot, error) { return snapshot(false, nil), ErrUnsupported }
func (d *unsupportedDriver) PlaybackSnapshot() (Snapshot, error) {
	return snapshot(false, nil), ErrUnsupported
}
func (d *unsupportedDriver) CourseSnapshot() (Snapshot, error) {
	return snapshot(false, nil), ErrUnsupported
}
func (d *unsupportedDriver) ExpandFolder(string) error             { return ErrUnsupported }
func (d *unsupportedDriver) OpenCourse(string) error               { return ErrUnsupported }
func (d *unsupportedDriver) SetVolume(int) error                   { return ErrUnsupported }
func (d *unsupportedDriver) SetFullscreen(bool) error              { return ErrUnsupported }
func (d *unsupportedDriver) SetPlaybackRate(float64) error         { return ErrUnsupported }
func (d *unsupportedDriver) TogglePlayback() error                 { return ErrUnsupported }
func (d *unsupportedDriver) Click(ClickRequest) error              { return ErrUnsupported }
func (d *unsupportedDriver) RequestAccessibilityPermission() error { return ErrUnsupported }

func (d *unsupportedDriver) RevealCourse(CourseTargetRequest) (Snapshot, error) {
	return snapshot(false, nil), ErrUnsupported
}
