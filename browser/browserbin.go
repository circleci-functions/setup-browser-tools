package browser

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/circleci-functions/toolkit/bashenv"
)

// The variables later steps can read to find an installed browser. CHROME_BIN
// is what Karma's Chrome launchers read; FIREFOX_BIN is its Firefox
// equivalent.
const (
	chromeBinEnv  = "CHROME_BIN"
	firefoxBinEnv = "FIREFOX_BIN"
)

// firefoxMacBinary is the binary inside the app bundle installFirefoxMacOS
// copies into /Applications.
const firefoxMacBinary = "/Applications/Firefox.app/Contents/MacOS/firefox"

// exportBrowserBin logs where browser is and exports path to later steps as
// name, so a test runner need not go looking for the browser itself.
//
// A value the user has already set is left alone, as is one an earlier install
// in this run exported. Like RecordVersion this is a convenience, so a failure
// to export warns rather than failing an install that has already succeeded.
func (i *Installer) exportBrowserBin(browser, name, path string) {
	i.logf("%s is at %s", browser, path)

	if current := os.Getenv(name); current != "" {
		i.logf("%s is already set to %s; leaving it as it is", name, current)
		return
	}
	if owner, ok := i.exported[name]; ok {
		i.logf("%s already names %s; leaving it as it is", name, owner)
		return
	}
	if i.exported == nil {
		i.exported = map[string]string{}
	}
	i.exported[name] = browser

	err := bashenv.ExportEnvVars(map[string]string{name: path})
	switch {
	case errors.Is(err, bashenv.ErrNoBashEnv):
		i.logf("BASH_ENV is not set, so %s is not exported to later steps", name)
	case err != nil:
		i.logf("cannot export %s to later steps: %v", name, err)
	default:
		i.logf("exported %s for later steps", name)
	}
}

// exportChromeBin exports whichever binary the channel's Chrome was found
// under. On macOS that is the binary inside the app bundle, which is where
// Karma itself looks there, rather than the google-chrome wrapper script.
func (i *Installer) exportChromeBin(ctx context.Context, channel string) {
	name := "Chrome"
	if i.Platform.Distro == DistroAlpine {
		name = "Chromium"
	}

	if i.Platform.IsMacOS() {
		i.exportBrowserBin(name, chromeBinEnv, macAppBinary(chromeMacApp(channel)))
		return
	}

	bin, _, found := i.installedChrome(ctx, channel)
	if !found {
		i.logf("cannot find %s on PATH, so %s is not exported", name, chromeBinEnv)
		return
	}
	path, err := i.Host.lookPath(bin)
	if err != nil {
		i.logf("cannot find %s on PATH, so %s is not exported", name, chromeBinEnv)
		return
	}
	i.exportBrowserBin(name, chromeBinEnv, absPath(path))
}

// exportFirefoxBin exports the Firefox this run installed, or with fresh unset
// the one already found on PATH. A fresh install on macOS is named by the
// binary in the app bundle: the wrapper it links to lives in the install's
// temporary directory, which does not outlive the step.
func (i *Installer) exportFirefoxBin(fresh bool, installDir string) {
	switch {
	case fresh && i.Platform.IsMacOS():
		i.exportBrowserBin("Firefox", firefoxBinEnv, firefoxMacBinary)
	case fresh:
		i.exportBrowserBin("Firefox", firefoxBinEnv, absPath(filepath.Join(installDir, "firefox")))
	default:
		path, err := i.Host.lookPath("firefox")
		if err != nil {
			i.logf("cannot find Firefox on PATH, so %s is not exported", firefoxBinEnv)
			return
		}
		i.exportBrowserBin("Firefox", firefoxBinEnv, absPath(path))
	}
}

// absPath makes path absolute, since a later step may run from another
// directory. A path that cannot be made absolute is returned as it is.
func absPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
