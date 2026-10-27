//go:build !linux && !darwin

package browser

import "errors"

func startDetached(string, []string, string) (func() error, error) {
	return nil, errors.New("starting Xvfb is only supported on Linux")
}
