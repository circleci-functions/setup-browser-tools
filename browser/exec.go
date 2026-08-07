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

const (
	aptMaxAttempts = 5
	aptRetryDelay  = 5 * time.Second
	aptListsDir    = "/var/lib/apt/lists"
)

type Command struct {
	Name string
	Args []string
	// Env holds additional KEY=VALUE entries, appended to the process environment.
	Env []string
	// Unprivileged suppresses the sudo prefix, for commands that only read.
	Unprivileged bool
	// Quiet keeps output out of the log, returning it to the caller only.
	Quiet bool
}

func (c Command) String() string {
	if len(c.Args) == 0 {
		return c.Name
	}
	return c.Name + " " + strings.Join(c.Args, " ")
}

type Executor func(ctx context.Context, name string, args, env []string, out io.Writer) (string, error)

// Runner executes external commands, applying sudo where needed. The zero value
// runs commands for real against stderr; tests replace the fields to observe
// invocations without executing anything.
type Runner struct {
	Priv     Privilege
	Out      io.Writer
	Sleep    func(time.Duration)
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

func (r *Runner) Exec(ctx context.Context, name string, args ...string) error {
	_, err := r.Run(ctx, Command{Name: name, Args: args})
	return err
}

// Output runs name unprivileged and returns its stdout without logging it, for
// interrogating the system.
func (r *Runner) Output(ctx context.Context, name string, args ...string) (string, error) {
	return r.Run(ctx, Command{Name: name, Args: args, Unprivileged: true, Quiet: true})
}

// RunWithRetry runs cmd up to aptMaxAttempts times, clearing apt's package
// lists between attempts. The clearing is the point: a truncated list from a
// flaky mirror fails every later apt-get identically, so retrying without it
// would fail the same way five times.
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

func (r *Runner) AptUpdate(ctx context.Context) error {
	return r.RunWithRetry(ctx, Command{Name: "apt-get", Args: []string{"update"}})
}

// clearAptLists expands the orb's `rm -rf /var/lib/apt/lists/*` glob in Go
// rather than handing it to a shell, so no shell is involved and a host without
// apt is a no-op instead of an rm on a literal asterisk. Failures only reduce
// the odds of the next attempt succeeding, so they are reported and ignored.
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
