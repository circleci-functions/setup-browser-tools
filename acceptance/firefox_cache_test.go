//go:build acceptance

package acceptance_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"
)

const firefoxCacheVersion = "149.0.2"

// TestAcceptanceFirefoxCache installs a pinned Firefox with --use-cache, then
// installs it again on the same machine with the install and the local cache
// directory gone and archive.mozilla.org unreachable, so only the job cache can
// supply it. The second install is still verified, from the signing key,
// manifest and signature cached with the archive.
//
// In CI the second install restores, from CircleCI's own cache, an entry saved
// earlier in the same job. The key carries the sandbox's nonce, which the
// toolkit derives from the run so that a later step of it can see what an
// earlier one saved and no other run can.
func TestAcceptanceFirefoxCache(t *testing.T) {
	skip.If(t, runtime.GOOS != "linux" || runtime.GOARCH != "amd64",
		"Firefox is installed from Mozilla's tarball only on Linux amd64")
	s, run := sandbox(t)
	args := []string{"install-firefox", "--use-cache", "--version", firefoxCacheVersion, "--cache-key", s.CacheKeyNonce}

	out, err := run(t, args...)
	assert.NilError(t, err, out)
	assert.Assert(t, !strings.Contains(out, "from the job cache"), "the first install has nothing to restore: %s", out)

	asRoot(t, "", "rm", "-rf", "/usr/local/bin/firefox", "/usr/local/bin/firefox-"+firefoxCacheVersion)
	assert.NilError(t, os.RemoveAll(filepath.Join(s.Home, ".cache", "setup-browser-tools")))
	out, err = laterStep(t, s, "firefox", "--version")
	assert.Assert(t, err != nil, "firefox is still installed: %s", out)

	blockHost(t, "archive.mozilla.org")

	out, err = run(t, args...)
	assert.NilError(t, err, out)
	assert.Assert(t, strings.Contains(out, "from the job cache"), "the second install should come from the cache: %s", out)
	assertReportsVersion(t, s, "firefox", firefoxCacheVersion)
}

// blockHost points host at 127.0.0.1 in /etc/hosts until t ends, so a
// connection to it is refused at once. The binary is built without cgo, and Go's
// own resolver reads /etc/hosts before asking DNS.
func blockHost(t *testing.T, host string) {
	t.Helper()

	original, err := os.ReadFile("/etc/hosts")
	assert.NilError(t, err)
	// Both writes go through tee rather than a rename: in a container
	// /etc/hosts is a bind mount, which can be written but not replaced.
	t.Cleanup(func() { asRoot(t, string(original), "tee", "/etc/hosts") })
	asRoot(t, "\n127.0.0.1 "+host+"\n", "tee", "-a", "/etc/hosts")
}

// asRoot runs name as root, through sudo unless the suite already is root as on
// an image without sudo, with stdin as its input, and fails t if it fails.
func asRoot(t *testing.T, stdin, name string, args ...string) {
	t.Helper()

	if os.Geteuid() != 0 {
		name, args = "sudo", append([]string{name}, args...)
	}
	// Not t.Context: this also runs from a cleanup, by when that is cancelled.
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	assert.NilError(t, err, "%s %s: %s", name, strings.Join(args, " "), out)
}
