//go:build acceptance

package acceptance_test

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/runner"
)

// TestAcceptanceBrowserBinDebian checks a later step can find both browsers
// through the variables the installs export, rather than by searching PATH.
func TestAcceptanceBrowserBinDebian(t *testing.T) {
	requireGoogleChromeHost(t)
	s, run := sandbox(t)

	out, err := run(t, "install-chrome")
	assert.NilError(t, err, out)
	out, err = run(t, "install-firefox")
	assert.NilError(t, err, out)

	assertBinReportsVersion(t, s, "CHROME_BIN", "Google Chrome")
	assertBinReportsVersion(t, s, "FIREFOX_BIN", "Mozilla Firefox")
}

// TestAcceptanceBrowserBinAlreadyInstalled checks an install that finds the
// browser already there still exports it, and that doing so again adds no
// second copy of the export to $BASH_ENV.
func TestAcceptanceBrowserBinAlreadyInstalled(t *testing.T) {
	requireGoogleChromeHost(t)
	s, run := sandbox(t)

	for _, cmd := range []string{"install-chrome", "install-firefox"} {
		out, err := run(t, cmd)
		assert.NilError(t, err, out)
		out, err = run(t, cmd)
		assert.NilError(t, err, out)
		assert.Assert(t, strings.Contains(out, "is already installed"), "the second %s reinstalled: %s", cmd, out)
	}

	assert.Equal(t, exportsOf(t, s, "CHROME_BIN"), 1, "CHROME_BIN exported more than once:\n%s", readBashEnv(t, s))
	assert.Equal(t, exportsOf(t, s, "FIREFOX_BIN"), 1, "FIREFOX_BIN exported more than once:\n%s", readBashEnv(t, s))
	assertBinReportsVersion(t, s, "CHROME_BIN", "Google Chrome")
	assertBinReportsVersion(t, s, "FIREFOX_BIN", "Mozilla Firefox")
}

// TestAcceptanceBrowserBinLeavesAPresetValueAlone checks a CHROME_BIN the job
// already sets wins over the installed Chrome.
func TestAcceptanceBrowserBinLeavesAPresetValueAlone(t *testing.T) {
	requireChromeHost(t)
	s, _ := sandbox(t)

	env := maps.Clone(s.Env())
	env["CHROME_BIN"] = "/opt/my-chrome/chrome"
	r := runner.New(t, binaryPath, runner.WithSandboxEnv(env))
	_, err := r.Run("install-chrome")
	out := r.StdoutString() + r.StderrString()
	t.Logf("setup-browser-tools install-chrome:\n%s", out)
	assert.NilError(t, err, out)

	assert.Assert(t, strings.Contains(out, "CHROME_BIN is already set to /opt/my-chrome/chrome; leaving it as it is"), out)
	assert.Equal(t, exportsOf(t, s, "CHROME_BIN"), 0, "CHROME_BIN was exported:\n%s", readBashEnv(t, s))
}

// TestAcceptanceBrowserBinAlpine checks install-chrome on Alpine names the
// Chromium it installs.
func TestAcceptanceBrowserBinAlpine(t *testing.T) {
	if !isAlpine() {
		t.Skip("Chromium stands in for Chrome only on Alpine")
	}
	s, run := sandbox(t)

	out, err := run(t, "install-chrome")
	assert.NilError(t, err, out)

	bin := assertBinReportsVersion(t, s, "CHROME_BIN", "Chromium")
	assert.Assert(t, strings.Contains(filepath.Base(bin), "chromium"), "CHROME_BIN is %s", bin)
}

// TestAcceptanceBrowserBinChromeForTesting checks Chrome for Testing keeps
// CHROME_BIN when one run installs it alongside Chrome.
func TestAcceptanceBrowserBinChromeForTesting(t *testing.T) {
	requireGoogleChromeHost(t)
	if _, err := exec.LookPath("apt-get"); err != nil {
		t.Skip("Chrome for Testing is published for Debian/Ubuntu only on Linux")
	}
	s, run := sandbox(t)

	out, err := run(t, "install-browser-tools", "--chrome-for-testing",
		"--chrome-for-testing-install-dir", s.BinDir,
		"--no-chromedriver", "--no-firefox", "--no-geckodriver")
	assert.NilError(t, err, out)
	assert.Assert(t, strings.Contains(out, "CHROME_BIN already names Chrome for Testing"), out)

	bin := assertBinReportsVersion(t, s, "CHROME_BIN", "Google Chrome for Testing")
	assert.Equal(t, bin, filepath.Join(s.BinDir, "chrome"))
}

// requireGoogleChromeHost skips t where Google publishes no Chrome: Google
// Chrome is amd64-only on Linux, and Alpine gets Chromium instead.
func requireGoogleChromeHost(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" || isAlpine() {
		t.Skipf("needs Google Chrome and Firefox, which this suite installs only on Linux amd64 outside Alpine, not %s/%s",
			runtime.GOOS, runtime.GOARCH)
	}
}

// requireChromeHost skips t where install-chrome has nothing to install.
func requireChromeHost(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && !isAlpine()) {
		t.Skipf("install-chrome installs nothing on %s/%s here", runtime.GOOS, runtime.GOARCH)
	}
}

// assertBinReportsVersion checks a later step sees name as an absolute path,
// and that running it reports a version containing want. It returns the path.
func assertBinReportsVersion(t *testing.T, s *sdktest.Sandbox, name, want string) string {
	t.Helper()

	out, err := laterStep(t, s, "printenv", name)
	assert.NilError(t, err, "%s is not set in a later step: %s", name, out)
	bin := strings.TrimSpace(out)
	assert.Assert(t, filepath.IsAbs(bin), "%s is not an absolute path: %q", name, bin)

	assertReportsVersion(t, s, bin, want)
	return bin
}

func readBashEnv(t *testing.T, s *sdktest.Sandbox) string {
	t.Helper()
	data, err := os.ReadFile(s.BashEnv)
	if os.IsNotExist(err) {
		return ""
	}
	assert.NilError(t, err)
	return string(data)
}

// exportsOf counts the lines in $BASH_ENV that export name.
func exportsOf(t *testing.T, s *sdktest.Sandbox, name string) int {
	t.Helper()
	var n int
	for line := range strings.Lines(readBashEnv(t, s)) {
		if strings.HasPrefix(line, "export "+name+"=") {
			n++
		}
	}
	return n
}
