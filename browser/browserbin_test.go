package browser

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// These cover what a real install cannot cheaply reach; the acceptance suite
// checks the exports a real install makes on Debian and Alpine.

// bashEnv returns what the install wrote to the $BASH_ENV testInstaller points
// at, or "" when it wrote nothing.
func bashEnv(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(os.Getenv("BASH_ENV")) //nolint:gosec // testInstaller points it at a temp file
	if errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	assert.NilError(t, err)
	return string(data)
}

func TestInstallChromeExportsTheAppBinaryOnMacOS(t *testing.T) {
	tests := []struct {
		name    string
		channel string
		onPath  []string
		want    string
	}{
		{
			name:    "stable names the binary in the app bundle, not the wrapper",
			channel: ChannelStable,
			onPath:  []string{"google-chrome-stable"},
			want:    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		},
		{
			name:    "beta",
			channel: ChannelBeta,
			onPath:  []string{"google-chrome-beta"},
			want:    "/Applications/Google Chrome Beta.app/Contents/MacOS/Google Chrome Beta",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, _ := testInstaller(t, installerOpts{
				distro:  DistroMacOS,
				onPath:  tc.onPath,
				stdouts: map[string]string{"--version": "Google Chrome 147.0.7727.55 \n"},
			})
			var log strings.Builder
			i.Out = &log

			assert.NilError(t, i.InstallChrome(context.Background(), ChromeOptions{Version: "147.0.7727.55", Channel: tc.channel}))

			assert.Equal(t, bashEnv(t), "export CHROME_BIN='"+tc.want+"'\n")
			assert.Assert(t, strings.Contains(log.String(), "Chrome is at "+tc.want+"\n"), "log was %q", log.String())
		})
	}
}

func TestExportFirefoxBinOnMacOSNamesTheAppBinary(t *testing.T) {
	// The link in --install-dir points at a wrapper in the install's temporary
	// directory, which is gone by the time a later step runs.
	i, _ := testInstaller(t, installerOpts{distro: DistroMacOS})

	i.exportFirefoxBin(true, "/usr/local/bin")

	assert.Equal(t, bashEnv(t), "export FIREFOX_BIN='/Applications/Firefox.app/Contents/MacOS/firefox'\n")
}

func TestExportBrowserBinWithoutBashEnvOnlyLogs(t *testing.T) {
	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})
	t.Setenv("BASH_ENV", "")
	var log strings.Builder
	i.Out = &log

	i.exportBrowserBin("Firefox", firefoxBinEnv, "/usr/local/bin/firefox")

	assert.Assert(t, strings.Contains(log.String(), "Firefox is at /usr/local/bin/firefox\n"), "log was %q", log.String())
	assert.Assert(t, strings.Contains(log.String(), "BASH_ENV is not set, so FIREFOX_BIN is not exported to later steps"),
		"log was %q", log.String())
}

func TestExportBrowserBinWarnsWhenBashEnvCannotBeWritten(t *testing.T) {
	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})
	// A directory cannot be read as a file, which is as broken as $BASH_ENV gets.
	t.Setenv("BASH_ENV", t.TempDir())
	var log strings.Builder
	i.Out = &log

	i.exportBrowserBin("Firefox", firefoxBinEnv, "/usr/local/bin/firefox")

	assert.Assert(t, strings.Contains(log.String(), "cannot export FIREFOX_BIN to later steps"), "log was %q", log.String())
}

func TestExportBrowserBinFirstClaimInARunWins(t *testing.T) {
	// install-browser-tools installs Chrome for Testing before Chrome, so this
	// is what gives Chrome for Testing CHROME_BIN when both are installed.
	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})
	var log strings.Builder
	i.Out = &log

	i.exportBrowserBin("Chrome for Testing", chromeBinEnv, "/usr/local/bin/chrome")
	i.exportBrowserBin("Chrome", chromeBinEnv, "/usr/bin/google-chrome-stable")

	assert.Equal(t, bashEnv(t), "export CHROME_BIN='/usr/local/bin/chrome'\n")
	assert.Assert(t, strings.Contains(log.String(), "Chrome is at /usr/bin/google-chrome-stable\n"), "log was %q", log.String())
	assert.Assert(t, strings.Contains(log.String(), "CHROME_BIN already names Chrome for Testing; leaving it as it is"),
		"log was %q", log.String())
}
