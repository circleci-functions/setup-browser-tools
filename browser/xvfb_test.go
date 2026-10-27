package browser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/toolkit/bashenv"
)

// fakeX stands in for Xvfb and its socket, so no X server is started.
type fakeX struct {
	// up is whether the display answers before anything is started.
	up bool
	// upAfter is how many polls after starting the display first answers; -1
	// means never.
	upAfter int
	// exitErr is what the started process reports from its exit check.
	exitErr error
	// log is written to the log file when the process starts.
	log string

	started [][]string
	polls   int
	sleeps  int
}

func (f *fakeX) server(t *testing.T) XServer {
	t.Helper()
	return XServer{
		LogDir: t.TempDir(),
		Start: func(name string, args []string, logPath string) (func() error, error) {
			f.started = append(f.started, append([]string{name}, args...))
			if err := os.WriteFile(logPath, []byte(f.log), 0o600); err != nil {
				return nil, err
			}
			return func() error { return f.exitErr }, nil
		},
		Ready: func(string) bool {
			if len(f.started) == 0 {
				return f.up
			}
			f.polls++
			return f.upAfter >= 0 && f.polls > f.upAfter
		},
		Sleep: func(time.Duration) { f.sleeps++ },
	}
}

// xvfbInstaller is testInstaller with a fake X server and a log to read.
func xvfbInstaller(t *testing.T, x *fakeX, opts installerOpts) (*Installer, *[]invocation, *bytes.Buffer) {
	t.Helper()
	i, calls := testInstaller(t, opts)
	i.XServer = x.server(t)
	var log bytes.Buffer
	i.Out = &log
	return i, calls, &log
}

// withBashEnv points $BASH_ENV at a fresh file and returns its path.
func withBashEnv(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bash_env")
	t.Setenv("BASH_ENV", path)
	return path
}

func readBashEnv(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	assert.NilError(t, err)
	return string(data)
}

func TestStartXvfbTimesOutWithTheLogTail(t *testing.T) {
	bashEnv := withBashEnv(t)
	var lines []string
	for n := 1; n <= 30; n++ {
		lines = append(lines, fmt.Sprintf("xvfb line %02d", n))
	}
	x := &fakeX{upAfter: -1, log: strings.Join(lines, "\n") + "\n"}
	i, _, _ := xvfbInstaller(t, x, installerOpts{distro: DistroDebian, onPath: []string{"Xvfb"}})

	err := i.StartXvfb(context.Background(), XvfbOptions{})

	assert.ErrorContains(t, err, "display :99 did not come up within 10s")
	assert.ErrorContains(t, err, "xvfb line 30")
	assert.ErrorContains(t, err, "xvfb line 11")
	assert.Assert(t, !strings.Contains(err.Error(), "xvfb line 10"), "only the tail belongs in the error: %v", err)
	assert.Equal(t, x.sleeps, int(xvfbTimeout/xvfbPoll), "the wait is bounded")
	assert.Equal(t, readBashEnv(t, bashEnv), "", "DISPLAY must not be exported for a display that is not up")
}

func TestStartXvfbReportsAnEarlyExit(t *testing.T) {
	withBashEnv(t)
	x := &fakeX{
		upAfter: -1,
		exitErr: errors.New("Xvfb exited with status 1"),
		log:     "(EE) Server is already active for display 99\n",
	}
	i, _, _ := xvfbInstaller(t, x, installerOpts{distro: DistroDebian, onPath: []string{"Xvfb"}})

	err := i.StartXvfb(context.Background(), XvfbOptions{})

	assert.ErrorContains(t, err, "the X server exited before display :99 came up")
	assert.ErrorContains(t, err, "exited with status 1")
	assert.ErrorContains(t, err, "Server is already active")
	assert.Equal(t, x.sleeps, 0, "an exited server is not waited on")
}

func TestStartXvfbIsANoOpOnMacOS(t *testing.T) {
	// Not even $BASH_ENV is needed: there is nothing to export.
	t.Setenv("BASH_ENV", "")
	x := &fakeX{}
	i, calls, log := xvfbInstaller(t, x, installerOpts{distro: DistroMacOS, arch: ArchARM64})

	assert.NilError(t, i.StartXvfb(context.Background(), XvfbOptions{}))

	assert.Equal(t, len(*calls), 0)
	assert.Equal(t, len(x.started), 0)
	assert.Assert(t, strings.Contains(log.String(), "macOS has a display"), log.String())
}

func TestStartXvfbFailsUpFront(t *testing.T) {
	tests := []struct {
		name    string
		bashEnv bool
		opts    XvfbOptions
		priv    Privilege
		wantErr string
		wantIs  error
	}{
		{
			name:    "no BASH_ENV to export DISPLAY through",
			opts:    XvfbOptions{},
			priv:    rootPrivilege(),
			wantErr: "cannot pass DISPLAY on",
			wantIs:  bashenv.ErrNoBashEnv,
		},
		{
			name:    "no root or sudo to install the package",
			bashEnv: true,
			priv:    Privilege{},
			wantErr: "Xvfb needs root or sudo to run apt-get",
		},
		{
			name:    "a display that is not a number",
			bashEnv: true,
			opts:    XvfbOptions{Display: "localhost:1"},
			priv:    rootPrivilege(),
			wantErr: `--display "localhost:1"`,
		},
		{
			name:    "a malformed screen",
			bashEnv: true,
			opts:    XvfbOptions{Screen: "1280x1024"},
			priv:    rootPrivilege(),
			wantErr: `--screen "1280x1024"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			x := &fakeX{up: true}
			i, calls, _ := xvfbInstaller(t, x, installerOpts{distro: DistroDebian})
			// After the installer, which points $BASH_ENV at a temp file.
			if tc.bashEnv {
				withBashEnv(t)
			} else {
				t.Setenv("BASH_ENV", "")
			}
			i.Runner.Priv = tc.priv

			err := i.StartXvfb(context.Background(), tc.opts)

			assert.ErrorContains(t, err, tc.wantErr)
			if tc.wantIs != nil {
				assert.ErrorIs(t, err, tc.wantIs)
			}
			assert.Equal(t, len(*calls), 0)
			assert.Equal(t, len(x.started), 0)
		})
	}
}

func TestBrowserToolsChecksXvfbBeforeInstalling(t *testing.T) {
	x := &fakeX{upAfter: 0}
	i, _, _ := xvfbInstaller(t, x, installerOpts{distro: DistroDebian})
	t.Setenv("BASH_ENV", "")
	var ran []string
	stubComponents(i, map[string]error{}, &ran)

	err := i.InstallBrowserTools(context.Background(), BrowserToolsOptions{Chrome: true, Xvfb: true})

	assert.ErrorIs(t, err, bashenv.ErrNoBashEnv)
	assert.Equal(t, len(ran), 0, "no browser should be installed for a run that cannot finish: %v", ran)
}

func TestDisplayAnswers(t *testing.T) {
	// A short directory: a unix socket path is limited to about 100 bytes.
	dir, err := os.MkdirTemp("", "x11")
	assert.NilError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	assert.Assert(t, !displayAnswers(dir, ":5"), "no socket at all")

	// A socket file left by a server that has died does not count.
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "X5"), nil, 0o600))
	assert.Assert(t, !displayAnswers(dir, ":5"), "a stale socket file")

	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "unix", filepath.Join(dir, "X6"))
	assert.NilError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	assert.Assert(t, displayAnswers(dir, ":6"), "a listening socket")
}
