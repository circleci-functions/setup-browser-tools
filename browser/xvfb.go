package browser

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/circleci-functions/toolkit/bashenv"
)

const (
	DefaultXvfbDisplay = ":99"
	DefaultXvfbScreen  = "1280x1024x24"

	xvfbBinary    = "Xvfb"
	xSocketDir    = "/tmp/.X11-unix"
	xvfbTimeout   = 10 * time.Second
	xvfbPoll      = 100 * time.Millisecond
	xvfbTailLines = 20
)

var (
	xvfbDisplayPattern = regexp.MustCompile(`^:([0-9]+)$`)
	xvfbScreenPattern  = regexp.MustCompile(`^[0-9]+x[0-9]+x[0-9]+$`)
)

type XvfbOptions struct {
	// Display is the X display to serve, e.g. ":99". Empty means
	// DefaultXvfbDisplay.
	Display string
	// Screen is the geometry and depth of screen 0, e.g. "1280x1024x24". Empty
	// means DefaultXvfbScreen.
	Screen string
}

func (o XvfbOptions) withDefaults() XvfbOptions {
	if o.Display == "" {
		o.Display = DefaultXvfbDisplay
	}
	if o.Screen == "" {
		o.Screen = DefaultXvfbScreen
	}
	return o
}

// XServer starts Xvfb and watches for its display. The zero value does both for
// real; tests replace the fields so no X server is started.
type XServer struct {
	// Start launches name detached from this process, with its output going to
	// logPath. The returned function reports nil while the process is running,
	// and why it stopped once it has.
	Start func(name string, args []string, logPath string) (exited func() error, err error)
	// Ready reports whether display is accepting connections.
	Ready func(display string) bool
	Sleep func(time.Duration)
	// LogDir is where the Xvfb log is written. Empty means os.TempDir().
	LogDir string
}

func (x XServer) start(name string, args []string, logPath string) (func() error, error) {
	if x.Start != nil {
		return x.Start(name, args, logPath)
	}
	return startDetached(name, args, logPath)
}

func (x XServer) ready(display string) bool {
	if x.Ready != nil {
		return x.Ready(display)
	}
	return displayAnswers(xSocketDir, display)
}

func (x XServer) sleep(d time.Duration) {
	if x.Sleep != nil {
		x.Sleep(d)
		return
	}
	time.Sleep(d)
}

func (x XServer) logPath(display string) string {
	dir := x.LogDir
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "xvfb-"+displayNumber(display)+".log")
}

// StartXvfb gives later steps an X display for headed browsers: it installs Xvfb
// if needed, starts it unless the display is already up, and exports DISPLAY
// through $BASH_ENV. macOS has a display of its own, so there it does nothing.
//
// Xvfb is left running when this returns, since the steps that need it come
// later. Nothing stops it; the job's machine is discarded at the end.
func (i *Installer) StartXvfb(ctx context.Context, opts XvfbOptions) error {
	opts = opts.withDefaults()
	if i.Platform.IsMacOS() {
		i.logf("macOS has a display of its own; not starting Xvfb")
		return nil
	}
	if err := i.checkXvfb(opts); err != nil {
		return err
	}

	if i.XServer.ready(opts.Display) {
		return i.exportDisplay(opts.Display, "display %s is already up; reusing it", opts.Display)
	}

	if err := i.installXvfb(ctx); err != nil {
		return err
	}
	if err := i.runXvfb(opts); err != nil {
		return err
	}
	return i.exportDisplay(opts.Display, "started Xvfb on %s at %s (log: %s)",
		opts.Display, opts.Screen, i.XServer.logPath(opts.Display))
}

// checkXvfb fails before anything is installed or started when the run cannot
// succeed. DISPLAY is what the caller asked for, so not being able to export it
// is an error rather than a warning.
func (i *Installer) checkXvfb(opts XvfbOptions) error {
	if i.Platform.IsMacOS() {
		return nil
	}
	if !xvfbDisplayPattern.MatchString(opts.Display) {
		return fmt.Errorf("--display %q is not an X display number such as :99", opts.Display)
	}
	if !xvfbScreenPattern.MatchString(opts.Screen) {
		return fmt.Errorf("--screen %q is not WIDTHxHEIGHTxDEPTH, such as 1280x1024x24", opts.Screen)
	}
	if os.Getenv("BASH_ENV") == "" {
		return fmt.Errorf("cannot pass DISPLAY on: %w", bashenv.ErrNoBashEnv)
	}
	if !i.Host.has(xvfbBinary) {
		return i.requirePrivilege("Xvfb", "run "+i.packageManager())
	}
	return nil
}

func (i *Installer) installXvfb(ctx context.Context) error {
	if i.Host.has(xvfbBinary) {
		i.logf("Xvfb is already installed")
		return nil
	}

	var err error
	switch i.Platform.Distro {
	case DistroDebian:
		if err = i.Runner.AptUpdate(ctx); err == nil {
			err = i.aptInstall(ctx, "xvfb")
		}
	case DistroRHEL:
		err = i.Runner.Exec(ctx, "yum", "install", "-y", "xorg-x11-server-Xvfb")
	case DistroAlpine:
		if err = i.Runner.RunWithRetry(ctx, Command{Name: "apk", Args: []string{"update"}}); err == nil {
			err = i.Runner.Exec(ctx, "apk", "add", "--no-cache", "xvfb")
		}
	default:
		return Unsupported("Xvfb", i.Platform, "")
	}
	if err != nil {
		return err
	}
	i.logf("installed Xvfb with %s", i.packageManager())
	return nil
}

// runXvfb starts Xvfb as the job user and waits for its socket to answer. It
// fails with the end of Xvfb's log if the server exits or never comes up.
func (i *Installer) runXvfb(opts XvfbOptions) error {
	logPath := i.XServer.logPath(opts.Display)
	args := []string{opts.Display, "-screen", "0", opts.Screen, "-nolisten", "tcp"}

	exited, err := i.XServer.start(xvfbBinary, args, logPath)
	if err != nil {
		return fmt.Errorf("starting Xvfb: %w", err)
	}

	for range int(xvfbTimeout / xvfbPoll) {
		if i.XServer.ready(opts.Display) {
			return nil
		}
		if err := exited(); err != nil {
			return fmt.Errorf("the X server exited before display %s came up: %w%s",
				opts.Display, err, logTail(logPath))
		}
		i.XServer.sleep(xvfbPoll)
	}
	if i.XServer.ready(opts.Display) {
		return nil
	}
	return fmt.Errorf("display %s did not come up within %s%s", opts.Display, xvfbTimeout, logTail(logPath))
}

func (i *Installer) exportDisplay(display, format string, args ...any) error {
	if err := bashenv.ExportEnvVars(map[string]string{"DISPLAY": display}); err != nil {
		return fmt.Errorf("exporting DISPLAY: %w", err)
	}
	i.logf(format+"; exported DISPLAY=%s to later steps", append(args, display)...)
	return nil
}

// displayNumber returns the 99 of ":99".
func displayNumber(display string) string {
	return strings.TrimPrefix(display, ":")
}

// displayAnswers reports whether the display's socket exists and accepts a
// connection. A socket file alone is not enough: one left behind by a server
// that has since died is still there.
func displayAnswers(socketDir, display string) bool {
	socket := filepath.Join(socketDir, "X"+displayNumber(display))
	if _, err := os.Stat(socket); err != nil {
		return false
	}
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// logTail returns the last lines of the Xvfb log, formatted to follow an error
// message, or "" when there is nothing to show.
func logTail(path string) string {
	data, err := os.ReadFile(path) //nolint:gosec // our own log file
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return ""
	}
	if len(lines) > xvfbTailLines {
		lines = lines[len(lines)-xvfbTailLines:]
	}
	return fmt.Sprintf("\nlast lines of %s:\n%s", path, strings.Join(lines, "\n"))
}
