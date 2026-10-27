//go:build acceptance

package acceptance_test

import (
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/toolkit/sdk"
	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/runner"
)

// disposableEnvVar opts a machine other than a CircleCI job in to the
// real-install tests. `task acceptance-docker` sets it in the container it
// starts.
const disposableEnvVar = "SETUP_BROWSER_TOOLS_ACCEPTANCE_DISPOSABLE"

// requireDisposable skips t unless the machine can be thrown away afterwards.
// A real install adds system packages, writes under /usr/local and /opt, and
// may edit /etc/hosts, none of which a sandbox can redirect, so it must never
// run on a developer's machine by accident.
func requireDisposable(t *testing.T) {
	t.Helper()
	if sdk.InCircleCIJob() || os.Getenv(disposableEnvVar) == "1" {
		return
	}
	t.Skipf("installs system packages, so it runs only in a CircleCI job or with %s=1", disposableEnvVar)
}

// runFunc runs the binary in a sandbox's environment and returns its combined
// output.
type runFunc func(t *testing.T, args ...string) (string, error)

// sandbox returns a sandbox, after requireDisposable, and a run function bound
// to it. $HOME, $BASH_ENV and the user's cache directory point into temp
// directories; the system locations an install writes to are the real ones.
//
// The cache is CircleCI's own when TOOLKIT_CACHE_MODE=live, which the CI jobs
// set, and in-memory otherwise, so the same test runs in a local container.
// Put s.CacheKeyNonce in any --cache-key a test passes: keys are write-once,
// and a fixed one would restore whatever an earlier run left behind.
func sandbox(t *testing.T) (*sdktest.Sandbox, runFunc) {
	t.Helper()
	requireDisposable(t)

	s := sdktest.NewSandbox(t, sdktest.WithLiveCache())
	return s, func(t *testing.T, args ...string) (string, error) {
		t.Helper()
		r := runner.New(t, binaryPath, runner.WithSandboxEnv(s.Env()))
		_, err := r.Run(args...)
		out := r.StdoutString() + r.StderrString()
		t.Logf("setup-browser-tools %s:\n%s", strings.Join(args, " "), out)
		return out, err
	}
}

// laterStep runs name with args the way a later step of the job would: in a
// fresh bash, which sources $BASH_ENV itself as a CircleCI step's shell does, so
// anything the function exported there applies. The sandbox's $HOME is used,
// since it stands in for the job's.
//
// $GOROOT is dropped, as sdktest.LaterStep does: go test can set it for the
// suite, and a later step would not have it.
func laterStep(t *testing.T, s *sdktest.Sandbox, name string, args ...string) (string, error) {
	t.Helper()

	env := s.CmdEnv(withoutVars(os.Environ(), "GOROOT"))
	if _, err := os.Stat(s.BashEnv); err != nil {
		// Nothing was exported. A job always has the file, and bash would
		// complain on stderr about a $BASH_ENV that is not there.
		env = withoutVars(env, "BASH_ENV")
	}

	cmd := exec.CommandContext(t.Context(), "bash", append([]string{"-c", `exec "$@"`, "bash", name}, args...)...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// withoutVars drops the named variables rather than emptying them, so a child
// cannot tell them from unset.
func withoutVars(env []string, names ...string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(names, name) {
			out = append(out, kv)
		}
	}
	return out
}

var dottedVersion = regexp.MustCompile(`[0-9]+\.[0-9]+`)

// assertReportsVersion checks a later step can run name --version and that it
// reports a version, containing want when want is not empty. An exit code
// alone is not evidence that anything was installed.
func assertReportsVersion(t *testing.T, s *sdktest.Sandbox, name, want string) {
	t.Helper()

	out, err := laterStep(t, s, name, "--version")
	assert.NilError(t, err, "%s --version in a later step: %s", name, out)
	assert.Assert(t, dottedVersion.MatchString(out), "%s --version reported no version: %s", name, out)
	if want != "" {
		assert.Assert(t, strings.Contains(out, want), "%s --version reported %q, want %s", name, out, want)
	}
}
