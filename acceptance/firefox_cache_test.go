//go:build acceptance

package acceptance_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
)

const firefoxCacheVersion = "149.0.2"

// mozillaReleaseKey is the primary key fingerprint Mozilla's signature on the
// checksum manifest is pinned to.
const mozillaReleaseKey = "14F26682D0916CDD81E37B6D61B7B526D98F0353"

// TestAcceptanceFirefoxCache installs a pinned Firefox with --use-cache, then
// installs it again on the same machine with the install and the local cache
// directory gone and archive.mozilla.org unreachable, so only the job cache can
// supply it. The second install is still verified, from the signing key,
// manifest and signature cached with the archive, and both installs check that
// signature against Mozilla's pinned release key in a keyring of their own.
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
	assertPinnedSignature(t, s, out)

	asRoot(t, "", "rm", "-rf", "/usr/local/bin/firefox", "/usr/local/bin/firefox-"+firefoxCacheVersion)
	assert.NilError(t, os.RemoveAll(filepath.Join(s.Home, ".cache", "setup-browser-tools")))
	out, err = laterStep(t, s, "firefox", "--version")
	assert.Assert(t, err != nil, "firefox is still installed: %s", out)

	blockHost(t, "archive.mozilla.org")

	out, err = run(t, args...)
	assert.NilError(t, err, out)
	assert.Assert(t, strings.Contains(out, "from the job cache"), "the second install should come from the cache: %s", out)
	assertPinnedSignature(t, s, out)
	assertReportsVersion(t, s, "firefox", firefoxCacheVersion)
}

// assertPinnedSignature checks an install's output reports the manifest signed
// by Mozilla's pinned release key, and that verifying it left the job user's
// own keyring alone.
func assertPinnedSignature(t *testing.T, s *sdktest.Sandbox, out string) {
	t.Helper()

	assert.Assert(t, strings.Contains(out, "signed by Mozilla's release key "+mozillaReleaseKey),
		"the manifest's signature was not checked against the pinned key: %s", out)
	_, err := os.Stat(filepath.Join(s.Home, ".gnupg"))
	assert.Assert(t, errors.Is(err, os.ErrNotExist), "the install used the job user's keyring: %v", err)
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
