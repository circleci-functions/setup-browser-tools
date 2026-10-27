//go:build linux || darwin

package browser

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

func TestStartDetachedLogsAndReportsExit(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "out.log")

	exited, err := startDetached("sh", []string{"-c", "echo to-stdout; echo to-stderr >&2; exit 3"}, logPath)
	assert.NilError(t, err)

	var exitErr error
	for range 100 {
		if exitErr = exited(); exitErr != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	assert.ErrorContains(t, exitErr, "sh exited with status 3")

	log, err := os.ReadFile(logPath)
	assert.NilError(t, err)
	assert.Equal(t, string(log), "to-stdout\nto-stderr\n")
}

func TestStartDetachedRunsInItsOwnSession(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "out.log")
	pidPath := filepath.Join(t.TempDir(), "pid")

	// The child records its pid, then waits; its session id must be its own.
	exited, err := startDetached("sh", []string{"-c", "echo $$ > " + pidPath + "; exec sleep 30"}, logPath)
	assert.NilError(t, err)
	assert.NilError(t, exited(), "the process should still be running")

	var pid int
	for range 100 {
		if data, err := os.ReadFile(pidPath); err == nil && len(data) > 0 {
			_, _ = fmt.Sscan(string(data), &pid)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	assert.Assert(t, pid > 0)
	t.Cleanup(func() {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		for range 100 {
			if exited() != nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	})

	// syscall has no portable Getsid wrapper.
	sid, _, errno := syscall.RawSyscall(syscall.SYS_GETSID, uintptr(pid), 0, 0)
	assert.Equal(t, errno, syscall.Errno(0))
	assert.Equal(t, int(sid), pid, "the process should lead a session of its own")
}
