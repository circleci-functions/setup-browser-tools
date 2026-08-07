package browser

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const versionsFileName = ".browser-versions"

// RecordVersion appends "<name>: <version>" to $HOME/.browser-versions, so later
// steps in the job can read what was installed.
//
// The orb writes this file from one branch of one command; every subcommand here
// records into it. It is informational, so a failure to write warns rather than
// failing an install that has already succeeded.
func RecordVersion(out io.Writer, name, version string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(out, "%s: cannot locate home directory, not recording %s %s: %v\n",
			cacheKeyPrefix, name, version, err)
		return
	}

	path := filepath.Join(home, versionsFileName)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // a fixed filename under the user's own home directory
	if err != nil {
		fmt.Fprintf(out, "%s: cannot write %s: %v\n", cacheKeyPrefix, path, err)
		return
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "%s: %s\n", name, version); err != nil {
		fmt.Fprintf(out, "%s: cannot write %s: %v\n", cacheKeyPrefix, path, err)
	}
}
