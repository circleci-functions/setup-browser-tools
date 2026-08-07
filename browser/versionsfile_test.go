package browser

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestRecordVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	RecordVersion(io.Discard, "Chrome", "147.0.7727.55")

	got, err := os.ReadFile(filepath.Join(home, versionsFileName))
	assert.NilError(t, err)
	assert.Equal(t, string(got), "Chrome: 147.0.7727.55\n")
}

func TestRecordVersionAppends(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Several subcommands in one job must accumulate rather than overwrite.
	RecordVersion(io.Discard, "Firefox", "149.0.2")
	RecordVersion(io.Discard, "geckodriver", "v0.36.0")
	RecordVersion(io.Discard, "Chrome", "147.0.7727.55")

	got, err := os.ReadFile(filepath.Join(home, versionsFileName))
	assert.NilError(t, err)
	assert.Equal(t, string(got), "Firefox: 149.0.2\ngeckodriver: v0.36.0\nChrome: 147.0.7727.55\n")
}

func TestRecordVersionWithoutHomeWarnsOnly(t *testing.T) {
	t.Setenv("HOME", "")

	var log bytes.Buffer
	RecordVersion(&log, "Chrome", "147.0.7727.55")

	assert.Assert(t, strings.Contains(log.String(), "Chrome"), "the warning should name what went unrecorded")
	assert.Assert(t, strings.Contains(log.String(), "147.0.7727.55"))
}

func TestRecordVersionUnwritableWarnsOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// A directory where the file should be makes the open fail.
	assert.NilError(t, os.MkdirAll(filepath.Join(home, versionsFileName), 0o750))

	var log bytes.Buffer
	RecordVersion(&log, "Chrome", "147.0.7727.55")
	assert.Assert(t, strings.Contains(log.String(), versionsFileName))
}
