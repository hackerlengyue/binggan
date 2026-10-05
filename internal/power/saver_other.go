//go:build (!darwin && !windows) || (darwin && !cgo)

package power

import "errors"

func startScreenSaverGuard() (func() error, func(), error) {
	return nil, nil, errors.New("当前系统不支持阻止屏保")
}
