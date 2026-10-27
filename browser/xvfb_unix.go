//go:build linux || darwin

package browser

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// startDetached starts name so that it outlives this process and the step that
// ran it: in a session of its own, so the end of the step does not hang it up,
// and with no stdio shared with the step, which may otherwise wait for the
// process to close it before finishing.
func startDetached(name string, args []string, logPath string) (func() error, error) {
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		return nil, err
	}
	defer stdin.Close()

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // a log path we chose
	if err != nil {
		return nil, err
	}
	defer logFile.Close()

	cmd := exec.Command(name, args...)
	cmd.Stdin = stdin
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		return nil, err
	}

	// The process is still this one's child after Release, so a non-blocking
	// wait both detects an early exit and reaps it.
	return func() error {
		var status syscall.WaitStatus
		wpid, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
		switch {
		case errors.Is(err, syscall.EINTR):
			return nil
		case err != nil:
			return fmt.Errorf("waiting for %s: %w", name, err)
		case wpid == 0:
			return nil
		case status.Signaled():
			return fmt.Errorf("%s was killed by %s", name, status.Signal())
		default:
			return fmt.Errorf("%s exited with status %d", name, status.ExitStatus())
		}
	}, nil
}
