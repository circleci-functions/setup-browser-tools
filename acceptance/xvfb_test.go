//go:build acceptance

package acceptance_test

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
)

// Each test starts Xvfb on a display of its own. A server outlives the test
// that started it, as it is meant to outlive a step, so sharing a display would
// make a later test reuse an earlier one's server instead of starting its own.
const (
	xvfbStartDisplay = ":90"
	xvfbBadDisplay   = ":91"
	// install-browser-tools --xvfb always uses the default display.
	xvfbHeadedDisplay = ":99"
)

// requireLinux skips t where start-xvfb has nothing to do.
func requireLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("start-xvfb only starts a display on Linux")
	}
}

// withoutDisplay unsets DISPLAY for the test, so a later step can only have it
// from $BASH_ENV.
func withoutDisplay(t *testing.T) {
	t.Helper()
	t.Setenv("DISPLAY", "")
	assert.NilError(t, os.Unsetenv("DISPLAY"))
}

func isAlpine() bool {
	_, err := os.Stat("/etc/alpine-release")
	return err == nil
}

// installPackages installs packages a test needs with the system package
// manager, under sudo unless this is root.
func installPackages(t *testing.T, apt, apk []string) {
	t.Helper()

	var cmds [][]string
	switch {
	case isAlpine():
		cmds = [][]string{append([]string{"apk", "add", "--no-cache"}, apk...)}
	case hasCommand("apt-get"):
		cmds = [][]string{
			{"apt-get", "update", "-qq"},
			append([]string{"apt-get", "install", "-y", "-qq"}, apt...),
		}
	default:
		t.Skip("no apk or apt-get to install the X client with")
	}

	for _, args := range cmds {
		if os.Geteuid() != 0 {
			args = append([]string{"sudo"}, args...)
		}
		cmd := exec.CommandContext(t.Context(), args[0], args[1:]...)
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
		out, err := cmd.CombinedOutput()
		assert.NilError(t, err, "%s: %s", strings.Join(args, " "), out)
	}
}

func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// assertExportedOnce checks $BASH_ENV sets DISPLAY to display, once.
func assertExportedOnce(t *testing.T, s *sdktest.Sandbox, display string) {
	t.Helper()
	data, err := os.ReadFile(s.BashEnv)
	assert.NilError(t, err, "nothing was exported")
	assert.Equal(t, strings.Count(string(data), "export DISPLAY="), 1, "$BASH_ENV:\n%s", data)
	assert.Assert(t, strings.Contains(string(data), "export DISPLAY='"+display+"'"), "$BASH_ENV:\n%s", data)
}

// TestAcceptanceXvfbStart starts a display, uses it from a later step, and
// starts it again, which must reuse it.
func TestAcceptanceXvfbStart(t *testing.T) {
	requireLinux(t)
	withoutDisplay(t)
	s, run := sandbox(t)

	out, err := run(t, "start-xvfb", "--display", xvfbStartDisplay)
	assert.NilError(t, err, out)
	assert.Assert(t, strings.Contains(out, "started Xvfb on "+xvfbStartDisplay), out)
	assertExportedOnce(t, s, xvfbStartDisplay)

	// The function's process has exited; the server must still be up for this
	// later one.
	got, err := laterStep(t, s, "printenv", "DISPLAY")
	assert.NilError(t, err, "DISPLAY is not set in a later step: %s", got)
	assert.Equal(t, strings.TrimSpace(got), xvfbStartDisplay)

	installPackages(t, []string{"x11-utils"}, []string{"xdpyinfo"})
	got, err = laterStep(t, s, "xdpyinfo")
	assert.NilError(t, err, "xdpyinfo in a later step: %s", got)
	assert.Assert(t, strings.Contains(got, "name of display:    "+xvfbStartDisplay), got)
	assert.Assert(t, strings.Contains(got, "1280x1024 pixels"), got)

	out, err = run(t, "start-xvfb", "--display", xvfbStartDisplay)
	assert.NilError(t, err, out)
	assert.Assert(t, strings.Contains(out, "display "+xvfbStartDisplay+" is already up; reusing it"), out)
	assertExportedOnce(t, s, xvfbStartDisplay)
}

// TestAcceptanceXvfbHeadedBrowser is what the display is for: a browser run
// without --headless, from a later step. Without a display Chrome exits at once
// with "Missing X server or $DISPLAY".
func TestAcceptanceXvfbHeadedBrowser(t *testing.T) {
	requireLinux(t)
	browser := "google-chrome"
	if isAlpine() {
		// install-chrome installs Chromium on Alpine, on either architecture.
		browser = "chromium-browser"
	} else if runtime.GOARCH == "arm64" {
		t.Skip("Google publishes no Linux ARM64 Chrome")
	}
	withoutDisplay(t)
	s, run := sandbox(t)

	out, err := run(t, "install-browser-tools", "--xvfb", "--no-firefox", "--no-geckodriver", "--no-chromedriver")
	assert.NilError(t, err, out)
	assertExportedOnce(t, s, xvfbHeadedDisplay)
	assertReportsVersion(t, s, browser, "")

	// --no-sandbox because a container may run this as root.
	script := browser + ` --no-first-run --no-sandbox --user-data-dir="$(mktemp -d)" about:blank >/tmp/headed-browser.log 2>&1 &
pid=$!
sleep 10
if kill -0 "$pid" 2>/dev/null; then
  echo "headed browser still running on $DISPLAY after 10s"
  kill "$pid"
  exit 0
fi
echo "headed browser exited:"
cat /tmp/headed-browser.log
exit 1`
	got, err := laterStep(t, s, "bash", "-c", script)
	assert.NilError(t, err, got)
	t.Log(strings.TrimSpace(got))
}

// TestAcceptanceXvfbBadScreen checks a server that refuses to start is reported
// with its own reason, rather than as a display that never came up.
func TestAcceptanceXvfbBadScreen(t *testing.T) {
	requireLinux(t)
	s, run := sandbox(t)

	// A depth of 7 bits is well formed but no visual supports it.
	out, err := run(t, "start-xvfb", "--display", xvfbBadDisplay, "--screen", "1024x768x7")
	assert.Assert(t, err != nil, "an unusable screen should fail:\n%s", out)
	assert.Assert(t, strings.Contains(out, "exited before display "+xvfbBadDisplay+" came up"), out)
	assert.Assert(t, strings.Contains(out, "Couldn't add screen"), "Xvfb's own error should be shown:\n%s", out)

	_, err = os.Stat(s.BashEnv)
	assert.Assert(t, os.IsNotExist(err), "DISPLAY must not be exported for a display that failed")
}
