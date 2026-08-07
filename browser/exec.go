package browser

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Retry policy for apt-get, matching the orb: five attempts, five seconds
// apart, clearing apt's package lists between them.
const (
	aptMaxAttempts = 5
	aptRetryDelay  = 5 * time.Second
	aptListsDir    = "/var/lib/apt/lists"
)

// Command describes an external command to run.
type Command struct {
	Name string
	Args []string
	// Env holds additional KEY=VALUE entries, appended to the process
	// environment.
	Env []string
	// Unprivileged runs the command without the sudo prefix. Commands that
	// only read, such as asking a browser for its version, set this.
	Unprivileged bool
	// Quiet keeps the command's output out of the log, returning it to the
	// caller only.
	Quiet bool
}

// String renders the command as it would be typed, for error messages.
func (c Command) String() string {
	if len(c.Args) == 0 {
		return c.Name
	}
	return c.Name + " " + strings.Join(c.Args, " ")
}

// Executor runs a fully prepared command, writing its output to out, and
// returns everything the command wrote to stdout.
type Executor func(ctx context.Context, name string, args, env []string, out io.Writer) (string, error)

// Runner executes external commands, applying the sudo prefix to those that
// need root. A zero Runner runs commands for real, writes their output to
// stderr, and sleeps between retries; tests replace the fields to observe
// invocations without executing anything.
type Runner struct {
	// Priv decides whether commands are prefixed with sudo.
	Priv Privilege
	// Out receives command output. Defaults to os.Stderr, which is where
	// install chatter belongs in a job log.
	Out io.Writer
	// Sleep defaults to time.Sleep.
	Sleep func(time.Duration)
	// Executor defaults to running the command for real.
	Executor Executor
}

func (r *Runner) out() io.Writer {
	if r.Out != nil {
		return r.Out
	}
	return os.Stderr
}

func (r *Runner) sleep(d time.Duration) {
	if r.Sleep != nil {
		r.Sleep(d)
		return
	}
	time.Sleep(d)
}

func (r *Runner) executor() Executor {
	if r.Executor != nil {
		return r.Executor
	}
	return realExec
}

// Run executes cmd and returns its stdout. Unless cmd is Unprivileged, the
// sudo prefix is applied.
func (r *Runner) Run(ctx context.Context, cmd Command) (string, error) {
	name, args := cmd.Name, cmd.Args
	if !cmd.Unprivileged {
		name, args = r.Priv.Command(cmd.Name, cmd.Args...)
	}

	out := r.out()
	if cmd.Quiet {
		out = io.Discard
	}

	stdout, err := r.executor()(ctx, name, args, cmd.Env, out)
	if err != nil {
		return stdout, fmt.Errorf("%s: %w", cmd, err)
	}
	return stdout, nil
}

// Exec runs name with args as root, discarding its stdout. It is the common
// case: an install command whose output only needs to reach the log.
func (r *Runner) Exec(ctx context.Context, name string, args ...string) error {
	_, err := r.Run(ctx, Command{Name: name, Args: args})
	return err
}

// Output runs name with args without the sudo prefix and returns its stdout,
// keeping it out of the log. It is for interrogating the system, such as
// reading an installed browser's version.
func (r *Runner) Output(ctx context.Context, name string, args ...string) (string, error) {
	return r.Run(ctx, Command{Name: name, Args: args, Unprivileged: true, Quiet: true})
}

// RunWithRetry runs cmd, retrying on failure up to five times, five seconds
// apart. Between attempts it clears apt's downloaded package lists: a
// truncated or corrupt list from a flaky mirror fails every later apt-get run
// until it is removed, so retrying without clearing them would fail
// identically each time.
//
// The returned error reports how many attempts were made.
func (r *Runner) RunWithRetry(ctx context.Context, cmd Command) error {
	var err error
	for attempt := 1; attempt <= aptMaxAttempts; attempt++ {
		if _, err = r.Run(ctx, cmd); err == nil {
			return nil
		}

		if attempt == aptMaxAttempts {
			return fmt.Errorf("%s failed after %d attempts: %w", cmd, aptMaxAttempts, err)
		}

		fmt.Fprintf(r.out(), "setup-browser-tools: attempt %d of %d failed, retrying: %v\n",
			attempt, aptMaxAttempts, err)
		r.clearAptLists(ctx)
		r.sleep(aptRetryDelay)
	}

	return err
}

// AptUpdate refreshes apt's package lists, with retries.
func (r *Runner) AptUpdate(ctx context.Context) error {
	return r.RunWithRetry(ctx, Command{Name: "apt-get", Args: []string{"update"}})
}

// clearAptLists removes apt's downloaded package lists. The directory is
// globbed here rather than passed to a shell, so no shell is involved and a
// host without apt is a no-op. Failures are reported and otherwise ignored,
// since this only exists to make the following retry more likely to succeed.
func (r *Runner) clearAptLists(ctx context.Context) {
	entries, err := filepath.Glob(filepath.Join(aptListsDir, "*"))
	if err != nil || len(entries) == 0 {
		return
	}

	args := append([]string{"-rf"}, entries...)
	if err := r.Exec(ctx, "rm", args...); err != nil {
		fmt.Fprintf(r.out(), "setup-browser-tools: could not clear %s: %v\n", aptListsDir, err)
	}
}

// realExec runs the command, teeing its stdout to out so that it both appears
// in the log and is returned. Stderr goes only to out.
func realExec(ctx context.Context, name string, args, env []string, out io.Writer) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	var stdout bytes.Buffer
	cmd.Stdout = io.MultiWriter(&stdout, out)
	cmd.Stderr = out

	err := cmd.Run()
	return stdout.String(), err //nolint:wrapcheck // wrapped by Runner.Run with the command line
}
