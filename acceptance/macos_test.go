//go:build acceptance

package acceptance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/runner"
)

// TestAcceptanceMacOSFirefox checks firefox still runs once the install step
// has ended. The step's own check runs before its temp dir is removed, so a
// link into that dir passes there and dangles in every step after it.
func TestAcceptanceMacOSFirefox(t *testing.T) {
	requireDarwin(t)
	s, _ := sandbox(t)

	out, err := runWithJobTools(t, s, "install-firefox")
	assert.NilError(t, err, out)
	// A Firefox already on PATH at the latest version is left alone, which
	// would leave nothing here under test.
	assert.Assert(t, strings.Contains(out, "installed Firefox "), "install-firefox installed nothing: %s", out)

	assertReportsVersion(t, s, "firefox", "")
	assertResolvesToFile(t, "/usr/local/bin/firefox")
}

// TestAcceptanceMacOSChromeForTesting checks the chrome link resolves on
// macOS, where the build is an app bundle rather than a tree with a chrome at
// its top.
func TestAcceptanceMacOSChromeForTesting(t *testing.T) {
	requireDarwin(t)
	s, run := sandbox(t)

	out, err := run(t, "install-chrome-for-testing")
	assert.NilError(t, err, out)

	for _, name := range []string{"chrome", "chromedriver"} {
		assertReportsVersion(t, s, name, "")
		assertResolvesToFile(t, filepath.Join("/usr/local/bin", name))
	}
}

// runWithJobTools is the sandbox's run for an install that uses brew and gpg,
// neither of which works under the sandbox's $HOME as it would under a job's:
//
//   - With an empty $HOME, brew has no cached formula index, so it fetches the
//     current one, in which the image's Homebrew finds no bottle for gnupg or
//     coreutils. Its cache is left at the job's, as a real job's would be.
//   - gpg keeps its agent's sockets in its home directory, and one under the
//     sandbox's long temp path is past macOS's limit on a socket's path, so
//     gpg --import fails. It gets a short home of its own instead.
func runWithJobTools(t *testing.T, s *sdktest.Sandbox, args ...string) (string, error) {
	t.Helper()

	cache := os.Getenv("HOMEBREW_CACHE")
	if cache == "" {
		home, err := os.UserHomeDir()
		assert.NilError(t, err)
		cache = filepath.Join(home, "Library", "Caches", "Homebrew")
	}
	gnupgHome, err := os.MkdirTemp("/tmp", "gnupg")
	assert.NilError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(gnupgHome) })

	env := s.Env()
	env["HOMEBREW_CACHE"] = cache
	env["GNUPGHOME"] = gnupgHome

	r := runner.New(t, binaryPath, runner.WithSandboxEnv(env))
	_, err = r.Run(args...)
	out := r.StdoutString() + r.StderrString()
	t.Logf("setup-browser-tools %s:\n%s", strings.Join(args, " "), out)
	return out, err
}

func requireDarwin(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skipf("covers the macOS install, and this is %s", runtime.GOOS)
	}
}

// assertResolvesToFile checks path, following any symlinks, is a regular file.
func assertResolvesToFile(t *testing.T, path string) {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(path)
	assert.NilError(t, err, "%s does not resolve", path)
	info, err := os.Stat(resolved)
	assert.NilError(t, err)
	assert.Assert(t, info.Mode().IsRegular(), "%s resolves to %s, which is not a file", path, resolved)
	t.Logf("%s resolves to %s", path, resolved)
}
