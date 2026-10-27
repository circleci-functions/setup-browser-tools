//go:build acceptance

package acceptance_test

import (
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// requireLinuxARM64 skips t unless it runs on 64-bit ARM Linux. When debian is
// set it must also be Debian/Ubuntu, and otherwise any distro but Alpine, whose
// Chromium packages change what is installed.
func requireLinuxARM64(t *testing.T, debian bool) {
	t.Helper()
	if runtime.GOOS != "linux" || runtime.GOARCH != "arm64" {
		t.Skipf("covers Linux ARM64, not %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if issue, err := os.ReadFile("/etc/issue"); err == nil && strings.Contains(string(issue), "Alpine") {
		t.Skip("covers Linux ARM64 other than Alpine")
	}
	if !debian {
		return
	}
	// The binary classifies a host with both as RHEL, so this does too.
	_, yumErr := exec.LookPath("yum")
	_, aptErr := exec.LookPath("apt-get")
	if yumErr == nil || aptErr != nil {
		t.Skip("covers Debian/Ubuntu on ARM64")
	}
}

// TestAcceptanceLinuxARM64Firefox installs Mozilla's linux-aarch64 build, then
// asks for a release older than the first one Mozilla built for ARM64. That
// must be refused by name, and must leave the Firefox already there working.
func TestAcceptanceLinuxARM64Firefox(t *testing.T) {
	requireLinuxARM64(t, false)
	s, run := sandbox(t)

	out, err := run(t, "install-firefox")
	assert.NilError(t, err, out)
	banner, err := laterStep(t, s, "firefox", "--version")
	assert.NilError(t, err, banner)
	installed := dottedVersion.FindString(banner)
	assert.Assert(t, installed != "", "firefox --version reported no version: %s", banner)

	out, err = run(t, "install-firefox", "--version", "128.0esr")
	assert.Assert(t, err != nil, "Firefox 128.0esr has no ARM64 build, so it must not install: %s", out)
	assert.Assert(t, strings.Contains(out, "Mozilla publishes Linux ARM64 builds from Firefox 136 on"), out)
	assertReportsVersion(t, s, "firefox", installed)
}

var chromeVersion = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+`)

// TestAcceptanceLinuxARM64ChromeForTesting installs the linux-arm64 build and
// its driver, which have to be the same version for a test to drive the one
// with the other.
func TestAcceptanceLinuxARM64ChromeForTesting(t *testing.T) {
	requireLinuxARM64(t, true)
	s, run := sandbox(t)

	out, err := run(t, "install-chrome-for-testing")
	assert.NilError(t, err, out)

	chrome, err := laterStep(t, s, "chrome", "--version")
	assert.NilError(t, err, chrome)
	version := chromeVersion.FindString(chrome)
	assert.Assert(t, version != "", "chrome --version reported no version: %s", chrome)
	assertReportsVersion(t, s, "chromedriver", version)
}

// TestAcceptanceLinuxARM64ChromeDriverIsRefused checks the refusal names the
// command that does install a driver here, since Google Chrome, which
// install-chromedriver matches, has no ARM64 build.
func TestAcceptanceLinuxARM64ChromeDriverIsRefused(t *testing.T) {
	requireLinuxARM64(t, false)
	_, run := sandbox(t)

	out, err := run(t, "install-chromedriver")
	assert.Assert(t, err != nil, "install-chromedriver must not succeed on Linux ARM64: %s", out)
	assert.Assert(t, strings.Contains(out, "use install-chrome-for-testing"), out)
}

// TestAcceptanceLinuxARM64BrowserTools runs the default set, where Chrome and
// ChromeDriver are on only by default, so they are skipped with a reason
// rather than failing the install of the two that do work.
func TestAcceptanceLinuxARM64BrowserTools(t *testing.T) {
	requireLinuxARM64(t, true)
	s, run := sandbox(t)

	out, err := run(t, "install-browser-tools")
	assert.NilError(t, err, out)
	assert.Assert(t, strings.Contains(out, "skipping Chrome: "), out)
	assert.Assert(t, strings.Contains(out, "skipping ChromeDriver: "), out)
	assertReportsVersion(t, s, "firefox", "")
	assertReportsVersion(t, s, "geckodriver", "")
}
