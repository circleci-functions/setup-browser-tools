//go:build acceptance

package acceptance_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"gotest.tools/v3/assert"
)

// offPathDir returns a --install-dir that does not exist yet and is not on
// PATH, which is the case a driver install has to create and export.
//
// An install with sudo leaves it owned by root, which t.TempDir could not
// clean up as the job user, so it is removed with the same privilege first.
func offPathDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "drivers")
	t.Cleanup(func() {
		rm := exec.Command("rm", "-rf", dir)
		if os.Geteuid() != 0 {
			rm = exec.Command("sudo", "rm", "-rf", dir)
		}
		if out, err := rm.CombinedOutput(); err != nil {
			t.Errorf("remove %s: %v: %s", dir, err, out)
		}
	})

	_, err := os.Stat(dir)
	assert.Assert(t, os.IsNotExist(err), "%s is meant to start missing", dir)
	assert.Assert(t, !strings.Contains(":"+os.Getenv("PATH")+":", ":"+dir+":"), "%s is meant to start off PATH", dir)
	return dir
}

// assertResolvesTo checks a later step finds name at want, which is what a
// test calling it by name would run.
func assertResolvesTo(t *testing.T, s *sdktest.Sandbox, name, want string) {
	t.Helper()
	out, err := laterStep(t, s, "bash", "-c", `command -v "$1"`, "bash", name)
	assert.NilError(t, err, "command -v %s in a later step: %s", name, out)
	assert.Equal(t, strings.TrimSpace(out), want)
}

func TestAcceptanceDriverInstallDirMissingAndOffPath(t *testing.T) {
	s, run := sandbox(t)
	dir := offPathDir(t)

	out, err := run(t, "install-geckodriver", "--install-dir", dir)
	assert.NilError(t, err, out)

	info, err := os.Stat(dir)
	assert.NilError(t, err, "the install did not create %s", dir)
	assert.Assert(t, info.IsDir())
	assertResolvesTo(t, s, "geckodriver", filepath.Join(dir, "geckodriver"))
	assertReportsVersion(t, s, "geckodriver", "")
}

// An older copy on PATH used to be what the install checked, and so failed it
// for reporting the wrong version.
func TestAcceptanceDriverInstallDirAheadOfAnOlderCopy(t *testing.T) {
	s, run := sandbox(t)
	dir := offPathDir(t)

	out, err := run(t, "install-geckodriver", "--version", "v0.35.0")
	assert.NilError(t, err, out)
	out, err = run(t, "install-geckodriver", "--version", "v0.36.0", "--install-dir", dir)
	assert.NilError(t, err, out)

	assertResolvesTo(t, s, "geckodriver", filepath.Join(dir, "geckodriver"))
	assertReportsVersion(t, s, "geckodriver", "0.36.0")
}

// The same version elsewhere on PATH used to be taken for one already in
// --install-dir, so nothing was written there.
func TestAcceptanceDriverInstallDirWithTheSameVersionOnPath(t *testing.T) {
	s, run := sandbox(t)
	dir := offPathDir(t)

	out, err := run(t, "install-geckodriver", "--version", "v0.36.0")
	assert.NilError(t, err, out)
	out, err = run(t, "install-geckodriver", "--version", "v0.36.0", "--install-dir", dir)
	assert.NilError(t, err, out)

	_, err = os.Stat(filepath.Join(dir, "geckodriver"))
	assert.NilError(t, err, "nothing was written into %s", dir)
	assertResolvesTo(t, s, "geckodriver", filepath.Join(dir, "geckodriver"))
	assertReportsVersion(t, s, "geckodriver", "0.36.0")
}

func TestAcceptanceDriverInstallDirChromeDriver(t *testing.T) {
	if runtime.GOOS == "linux" && runtime.GOARCH == "arm64" {
		t.Skip("Google publishes no Chrome or ChromeDriver for Linux arm64")
	}
	s, run := sandbox(t)
	dir := offPathDir(t)

	out, err := run(t, "install-chrome")
	assert.NilError(t, err, out)
	out, err = run(t, "install-chromedriver", "--install-dir", dir)
	assert.NilError(t, err, out)

	assertResolvesTo(t, s, "chromedriver", filepath.Join(dir, "chromedriver"))
	assertReportsVersion(t, s, "chromedriver", "")
}
