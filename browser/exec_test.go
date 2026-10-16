package browser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

type invocation struct {
	name string
	args []string
	env  []string
}

type recorder struct {
	calls    []invocation
	failures int
	stdout   string
}

func (rec *recorder) exec(_ context.Context, name string, args, env []string, out io.Writer) (string, error) {
	rec.calls = append(rec.calls, invocation{name: name, args: args, env: env})

	if rec.failures > 0 {
		rec.failures--
		return "", errors.New("exit status 1")
	}

	fmt.Fprint(out, rec.stdout)
	return rec.stdout, nil
}

func (rec *recorder) names() []string {
	out := make([]string, 0, len(rec.calls))
	for _, c := range rec.calls {
		out = append(out, c.name)
	}
	return out
}

func sudoPrivilege() Privilege {
	return ResolvePrivilege(Host{
		Geteuid:  func() int { return 1000 },
		LookPath: fakePath("sudo"),
	})
}

func rootPrivilege() Privilege {
	return ResolvePrivilege(Host{Geteuid: func() int { return 0 }})
}

func TestRunAppliesPrivilege(t *testing.T) {
	tests := []struct {
		name     string
		priv     Privilege
		cmd      Command
		wantName string
		wantArgs []string
	}{
		{
			name:     "privileged command is prefixed when not root",
			priv:     sudoPrivilege(),
			cmd:      Command{Name: "apt-get", Args: []string{"install", "-y", "firefox"}},
			wantName: "sudo",
			wantArgs: []string{"apt-get", "install", "-y", "firefox"},
		},
		{
			name:     "privileged command is not prefixed as root",
			priv:     rootPrivilege(),
			cmd:      Command{Name: "apt-get", Args: []string{"update"}},
			wantName: "apt-get",
			wantArgs: []string{"update"},
		},
		{
			name:     "unprivileged command is never prefixed",
			priv:     sudoPrivilege(),
			cmd:      Command{Name: "google-chrome", Args: []string{"--version"}, Unprivileged: true},
			wantName: "google-chrome",
			wantArgs: []string{"--version"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			r := &Runner{Priv: tc.priv, Out: io.Discard, Executor: rec.exec}

			_, err := r.Run(context.Background(), tc.cmd)
			assert.NilError(t, err)

			assert.Equal(t, len(rec.calls), 1)
			assert.Equal(t, rec.calls[0].name, tc.wantName)
			assert.DeepEqual(t, rec.calls[0].args, tc.wantArgs)
		})
	}
}

func TestRunStreamsAndReturnsStdout(t *testing.T) {
	var log bytes.Buffer
	rec := &recorder{stdout: "Google Chrome 147.0.7727.55\n"}
	r := &Runner{Priv: rootPrivilege(), Out: &log, Executor: rec.exec}

	got, err := r.Run(context.Background(), Command{Name: "google-chrome", Args: []string{"--version"}})
	assert.NilError(t, err)
	assert.Equal(t, got, "Google Chrome 147.0.7727.55\n")
	assert.Equal(t, log.String(), "Google Chrome 147.0.7727.55\n")
}

func TestOutputKeepsCommandOutOfTheLog(t *testing.T) {
	var log bytes.Buffer
	rec := &recorder{stdout: "Google Chrome 147.0.7727.55\n"}
	r := &Runner{Priv: sudoPrivilege(), Out: &log, Executor: rec.exec}

	got, err := r.Output(context.Background(), "google-chrome", "--version")
	assert.NilError(t, err)
	assert.Equal(t, got, "Google Chrome 147.0.7727.55\n")
	assert.Equal(t, log.String(), "", "Output must not write to the log")
	assert.Equal(t, rec.calls[0].name, "google-chrome", "Output must not use sudo")
}

func TestRunErrorNamesTheCommand(t *testing.T) {
	rec := &recorder{failures: 1}
	r := &Runner{Priv: rootPrivilege(), Out: io.Discard, Executor: rec.exec}

	_, err := r.Run(context.Background(), Command{Name: "apt-get", Args: []string{"install", "-y", "firefox"}})
	assert.Assert(t, err != nil)
	assert.Assert(t, strings.Contains(err.Error(), "apt-get install -y firefox"),
		"error %q should name the command", err.Error())
}

func TestRunPassesEnv(t *testing.T) {
	rec := &recorder{}
	r := &Runner{Priv: rootPrivilege(), Out: io.Discard, Executor: rec.exec}

	err := r.Exec(context.Background(), "apt-get", "install", "-y", "microsoft-edge-stable")
	assert.NilError(t, err)
	assert.Assert(t, rec.calls[0].env == nil)

	_, err = r.Run(context.Background(), Command{
		Name: "apt-get",
		Args: []string{"install", "-y", "google-chrome-stable"},
		Env:  []string{"DEBIAN_FRONTEND=noninteractive"},
	})
	assert.NilError(t, err)
	assert.DeepEqual(t, rec.calls[1].env, []string{"DEBIAN_FRONTEND=noninteractive"})
}

func TestRunWithRetry(t *testing.T) {
	tests := []struct {
		name          string
		failures      int
		wantErr       bool
		wantAttempts  int
		wantSleeps    int
		wantErrSubstr string
	}{
		{
			name:         "succeeds first time, no sleep",
			failures:     0,
			wantAttempts: 1,
			wantSleeps:   0,
		},
		{
			name:         "succeeds on the second attempt",
			failures:     1,
			wantAttempts: 2,
			wantSleeps:   1,
		},
		{
			name:         "succeeds on the last allowed attempt",
			failures:     4,
			wantAttempts: 5,
			wantSleeps:   4,
		},
		{
			name:          "gives up after five attempts",
			failures:      5,
			wantErr:       true,
			wantAttempts:  5,
			wantSleeps:    4,
			wantErrSubstr: "failed after 5 attempts",
		},
		{
			name:          "a permanently failing command still stops at five",
			failures:      99,
			wantErr:       true,
			wantAttempts:  5,
			wantSleeps:    4,
			wantErrSubstr: "apt-get update",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{failures: tc.failures}
			var sleeps []time.Duration
			r := &Runner{
				Priv:     rootPrivilege(),
				Out:      io.Discard,
				Sleep:    func(d time.Duration) { sleeps = append(sleeps, d) },
				Executor: rec.exec,
			}

			err := r.RunWithRetry(context.Background(), Command{Name: "apt-get", Args: []string{"update"}})

			if tc.wantErr {
				assert.Assert(t, err != nil)
				assert.Assert(t, strings.Contains(err.Error(), tc.wantErrSubstr),
					"error %q should contain %q", err.Error(), tc.wantErrSubstr)
			} else {
				assert.NilError(t, err)
			}

			// Only apt-get calls count as attempts; clearAptLists may add rm calls.
			attempts := 0
			for _, name := range rec.names() {
				if name == "apt-get" {
					attempts++
				}
			}
			assert.Equal(t, attempts, tc.wantAttempts)
			assert.Equal(t, len(sleeps), tc.wantSleeps)
			for _, d := range sleeps {
				assert.Equal(t, d, aptRetryDelay)
			}
		})
	}
}

func TestRunWithRetryDoesNotSleepAfterTheFinalAttempt(t *testing.T) {
	// Sleeping after the last attempt would add five dead seconds to every
	// failing job.
	rec := &recorder{failures: 99}
	var sleeps int
	r := &Runner{
		Priv:     rootPrivilege(),
		Out:      io.Discard,
		Sleep:    func(time.Duration) { sleeps++ },
		Executor: rec.exec,
	}

	err := r.RunWithRetry(context.Background(), Command{Name: "apt-get", Args: []string{"update"}})
	assert.Assert(t, err != nil)
	assert.Equal(t, sleeps, aptMaxAttempts-1)
}

func TestAptUpdateRetries(t *testing.T) {
	rec := &recorder{failures: 2}
	r := &Runner{
		Priv:     sudoPrivilege(),
		Out:      io.Discard,
		Sleep:    func(time.Duration) {},
		Executor: rec.exec,
	}

	assert.NilError(t, r.AptUpdate(context.Background()))

	// Every apt-get attempt must carry the sudo prefix, not just the first.
	aptCalls := 0
	for _, c := range rec.calls {
		if c.name == "sudo" && len(c.args) > 0 && c.args[0] == "apt-get" {
			aptCalls++
			assert.DeepEqual(t, c.args, []string{"apt-get", "update"})
		}
	}
	assert.Equal(t, aptCalls, 3)
}

func TestRunWithRetryReportsProgressToTheLog(t *testing.T) {
	var log bytes.Buffer
	rec := &recorder{failures: 2}
	r := &Runner{
		Priv:     rootPrivilege(),
		Out:      &log,
		Sleep:    func(time.Duration) {},
		Executor: rec.exec,
	}

	assert.NilError(t, r.RunWithRetry(context.Background(), Command{Name: "apt-get", Args: []string{"update"}}))
	assert.Assert(t, strings.Contains(log.String(), "attempt 1 of 5 failed"))
	assert.Assert(t, strings.Contains(log.String(), "attempt 2 of 5 failed"))
}

func TestCommandString(t *testing.T) {
	tests := []struct {
		name string
		cmd  Command
		want string
	}{
		{
			name: "name only",
			cmd:  Command{Name: "apt-get"},
			want: "apt-get",
		},
		{
			name: "name and args",
			cmd:  Command{Name: "apt-get", Args: []string{"install", "-y", "firefox"}},
			want: "apt-get install -y firefox",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.cmd.String(), tc.want)
		})
	}
}

func TestRealExecRunsAndCaptures(t *testing.T) {
	var log bytes.Buffer
	r := &Runner{Priv: rootPrivilege(), Out: &log}

	got, err := r.Output(context.Background(), "echo", "hello")
	assert.NilError(t, err)
	assert.Equal(t, strings.TrimSpace(got), "hello")
}

func TestRealExecReportsFailure(t *testing.T) {
	r := &Runner{Priv: rootPrivilege(), Out: io.Discard}

	_, err := r.Run(context.Background(), Command{
		Name:         "false",
		Unprivileged: true,
	})
	assert.Assert(t, err != nil)
	assert.Assert(t, strings.Contains(err.Error(), "false"))
}

func TestBrewNeverRunsUnderSudo(t *testing.T) {
	tests := []struct {
		name    string
		install func(*Installer) error
	}{
		{name: "Firefox dependencies", install: func(i *Installer) error { return i.installFirefoxDependencies(context.Background()) }},
		{name: "Edge", install: func(i *Installer) error { return i.installEdgeMacOS(context.Background()) }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, calls := testInstaller(t, installerOpts{distro: DistroMacOS, arch: ArchARM64, onPath: []string{"brew", "sudo"}})
			i.Runner.Priv = sudoPrivilege()

			assert.NilError(t, tc.install(i))

			assert.Assert(t, len(*calls) > 0)
			for _, c := range *calls {
				assert.Equal(t, c.name, "brew", "brew was not run as the job user: %+v", *calls)
			}
		})
	}
}
