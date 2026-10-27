package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/circleci-functions/toolkit/bashenv"
	"github.com/circleci-functions/toolkit/pathlink"
)

// Chrome majors below this have no ChromeDriver at all; below
// chromeForTestingFrom, drivers come from the old storage bucket.
const (
	chromeDriverMinMajor = 43
	chromeLegacyLatestTo = 114
	chromeForTestingFrom = 115
)

// legacyChromeDriverVersions maps Chrome majors 43-69 to their ChromeDriver
// release. Those predate the LATEST_RELEASE_<major> endpoint, so the mapping has
// to be carried explicitly.
var legacyChromeDriverVersions = map[int]string{
	69: "2.44", 68: "2.42", 67: "2.41", 66: "2.40", 65: "2.38",
	64: "2.37", 63: "2.36", 62: "2.35", 61: "2.34", 60: "2.33",
	59: "2.32", 58: "2.31", 57: "2.29", 56: "2.29", 55: "2.28",
	54: "2.27", 53: "2.26", 52: "2.24", 51: "2.23", 50: "2.22",
	49: "2.22", 48: "2.21", 47: "2.21", 46: "2.21", 45: "2.20",
	44: "2.20", 43: "2.20",
}

type ChromeDriverOptions struct {
	InstallDir string
}

func (i *Installer) InstallChromeDriver(ctx context.Context, opts ChromeDriverOptions) error {
	if i.Platform.Distro == DistroAlpine {
		return i.installChromeDriverAlpine(ctx)
	}
	if i.Platform.IsLinuxARM64() {
		return Unsupported("ChromeDriver", i.Platform, "Google publishes no Linux ARM64 build")
	}

	chromeVersion, err := i.chromeVersionForDriver(ctx)
	if err != nil {
		return err
	}
	i.logf("installed Chrome is %s", chromeVersion)

	driverVersion, downloadURL, err := i.resolveChromeDriver(ctx, chromeVersion)
	if err != nil {
		return err
	}

	driver := filepath.Join(opts.InstallDir, "chromedriver")
	if installed, found := i.installedDriverVersion(ctx, driver); found {
		if installed == driverVersion {
			i.logf("ChromeDriver %s is already installed in %s", installed, opts.InstallDir)
			return i.putOnPath(opts.InstallDir, "chromedriver")
		}
		i.logf("ChromeDriver %s is installed in %s; replacing it with %s", installed, opts.InstallDir, driverVersion)
		_ = i.Runner.Exec(ctx, "rm", "-f", driver)
	}
	if err := i.requireWritable("ChromeDriver", opts.InstallDir); err != nil {
		return err
	}

	if err := i.downloadAndInstallBinary(ctx, downloadURL, "chromedriver", opts.InstallDir, "chromedriver.zip"); err != nil {
		return err
	}

	installed, found := i.installedDriverVersion(ctx, driver)
	if !found {
		return fmt.Errorf("no working chromedriver at %s after installing %s", driver, driverVersion)
	}
	i.logf("installed ChromeDriver %s", installed)
	RecordVersion(i.Out, "ChromeDriver", installed)
	return i.putOnPath(opts.InstallDir, "chromedriver")
}

// installChromeDriverAlpine short-circuits: Alpine packages the driver alongside
// Chromium, and the install directory cannot be chosen.
func (i *Installer) installChromeDriverAlpine(ctx context.Context) error {
	if err := i.requirePrivilege("ChromeDriver", "run apk"); err != nil {
		return err
	}
	if err := i.Runner.RunWithRetry(ctx, Command{Name: "apk", Args: []string{"update"}}); err != nil {
		return err
	}
	if err := i.Runner.Exec(ctx, "apk", "add", "--no-cache", "chromium-chromedriver"); err != nil {
		return err
	}

	installed, found := i.installedDriverVersion(ctx, "chromedriver")
	if !found {
		return fmt.Errorf("no working chromedriver on PATH after installing chromium-chromedriver")
	}
	i.logf("installed ChromeDriver %s", installed)
	RecordVersion(i.Out, "ChromeDriver", installed)
	return nil
}

// chromeVersionForDriver reads the version from whichever Chrome is present,
// including the Beta bundle on macOS, since the driver has to match it.
func (i *Installer) chromeVersionForDriver(ctx context.Context) (string, error) {
	for _, channel := range []string{ChannelStable, ChannelBeta} {
		if v, found := i.installedChromeVersion(ctx, channel); found {
			return v, nil
		}
	}
	if i.Platform.IsMacOS() {
		for _, app := range []string{"Google Chrome", "Google Chrome Beta"} {
			binary := filepath.Join("/Applications", app+".app", "Contents", "MacOS", app)
			if _, err := os.Stat(binary); err != nil {
				continue
			}
			if banner, err := i.Runner.Output(ctx, binary, "--version"); err == nil {
				if v := ParseBrowserVersion(banner); v != "" {
					return v, nil
				}
			}
		}
	}
	return "", fmt.Errorf("Chrome is not installed; ChromeDriver needs it to pick a matching version") //nolint:staticcheck // proper noun
}

// driverPlatform is the download target name, which differs per OS and arch.
func (i *Installer) driverPlatform() string {
	if !i.Platform.IsMacOS() {
		return "linux64"
	}
	if i.Platform.Arch == ArchARM64 {
		return "mac-arm64"
	}
	return "mac-x64"
}

// resolveChromeDriver returns the driver version to install and where to get it.
// Three regimes, matching the orb: a table for Chrome 43-69, the
// LATEST_RELEASE_<major> endpoint for 70-114, and the Chrome for Testing
// catalogue from 115.
func (i *Installer) resolveChromeDriver(ctx context.Context, chromeVersion string) (version, url string, err error) {
	major, err := majorVersion(chromeVersion)
	if err != nil {
		return "", "", err
	}

	switch {
	case major < chromeDriverMinMajor:
		return "", "", fmt.Errorf("Chrome %s is too old: version %d or newer is required to use ChromeDriver", //nolint:staticcheck // proper noun
			chromeVersion, chromeDriverMinMajor)

	case major < 70:
		v := legacyChromeDriverVersions[major]
		return v, i.legacyDriverURL(v), nil

	case major <= chromeLegacyLatestTo:
		v, err := i.Fetcher.Text(ctx, fmt.Sprintf("%s/LATEST_RELEASE_%d", i.Endpoints.ChromeDriverLegacy, major))
		if err != nil {
			return "", "", err
		}
		return v, i.legacyDriverURL(v), nil

	default:
		return i.chromeForTestingDriver(ctx, chromeVersion, major)
	}
}

func (i *Installer) legacyDriverURL(version string) string {
	return fmt.Sprintf("%s/%s/chromedriver_%s.zip", i.Endpoints.ChromeDriverLegacy, version, i.driverPlatform())
}

// chromeForTestingDriver looks for a driver built for exactly this Chrome
// version, falling back to the newest build in the same milestone when there is
// no exact match, as the orb does.
func (i *Installer) chromeForTestingDriver(ctx context.Context, chromeVersion string, major int) (version, url string, err error) {
	wanted := numericVersion(chromeVersion)

	var catalogue struct {
		Versions []struct {
			Version   string `json:"version"`
			Downloads struct {
				ChromeDriver []struct {
					Platform string `json:"platform"`
					URL      string `json:"url"`
				} `json:"chromedriver"`
			} `json:"downloads"`
		} `json:"versions"`
	}
	if err := i.Fetcher.JSON(ctx, i.Endpoints.ChromeForTesting+"/known-good-versions-with-downloads.json", &catalogue); err != nil {
		return "", "", err
	}

	for _, entry := range catalogue.Versions {
		if entry.Version != wanted {
			continue
		}
		for _, dl := range entry.Downloads.ChromeDriver {
			if dl.Platform == i.driverPlatform() {
				return wanted, dl.URL, nil
			}
		}
	}

	i.logf("no ChromeDriver published for exactly %s; falling back to the newest build for milestone %d", wanted, major)
	return i.milestoneDriver(ctx, major)
}

func (i *Installer) milestoneDriver(ctx context.Context, major int) (version, url string, err error) {
	var milestones struct {
		Milestones map[string]struct {
			Version   string `json:"version"`
			Downloads struct {
				ChromeDriver []struct {
					Platform string `json:"platform"`
					URL      string `json:"url"`
				} `json:"chromedriver"`
			} `json:"downloads"`
		} `json:"milestones"`
	}
	endpoint := i.Endpoints.ChromeForTesting + "/latest-versions-per-milestone-with-downloads.json"
	if err := i.Fetcher.JSON(ctx, endpoint, &milestones); err != nil {
		return "", "", err
	}

	entry, ok := milestones.Milestones[strconv.Itoa(major)]
	if !ok {
		return "", "", fmt.Errorf("no ChromeDriver published for Chrome milestone %d", major)
	}
	for _, dl := range entry.Downloads.ChromeDriver {
		if dl.Platform == i.driverPlatform() {
			return entry.Version, dl.URL, nil
		}
	}
	return "", "", fmt.Errorf("no ChromeDriver for %s on %s", entry.Version, i.driverPlatform())
}

// downloadAndInstallBinary fetches an archive, extracts it in a temp directory
// and moves just the wanted binary into installDir. Staging outside the working
// directory is what keeps the archive's other members — licence files, driver
// notes — out of the caller's checkout.
func (i *Installer) downloadAndInstallBinary(ctx context.Context, url, binary, installDir, archiveName string) error {
	dir, cleanup, err := TempDir(binary)
	if err != nil {
		return err
	}
	defer cleanup()

	archive, err := i.Fetcher.Download(ctx, url, dir, archiveName)
	if err != nil {
		return err
	}

	extracted := filepath.Join(dir, "extracted")
	if err := mkdirAll(extracted); err != nil {
		return err
	}
	if err := Extract(archive, extracted); err != nil {
		return err
	}

	found, err := findFile(extracted, binary)
	if err != nil {
		return err
	}

	// An --install-dir that does not exist yet is created, with the same
	// privilege as the copy into it, rather than left for cp to fail on.
	if err := i.Runner.Exec(ctx, "mkdir", "-p", installDir); err != nil {
		return err
	}
	dest := filepath.Join(installDir, binary)
	if err := i.Runner.Exec(ctx, "cp", found, dest); err != nil {
		return err
	}
	return i.Runner.Exec(ctx, "chmod", "+x", dest)
}

// findFile locates name anywhere under root, since archives differ in whether
// they nest the binary in a per-platform directory.
func findFile(root, name string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == name && found == "" {
			found = path
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("search for %s: %w", name, err)
	}
	if found == "" {
		return "", fmt.Errorf("%s was not in the downloaded archive", name)
	}
	return found, nil
}

// installedDriverVersion reports the version binary prints. A driver installed
// into a directory is passed by its full path, so what gets checked is the copy
// in that directory, not whichever one PATH happens to resolve; only Alpine's
// package-managed driver, whose location the job does not choose, is looked up
// by name.
func (i *Installer) installedDriverVersion(ctx context.Context, binary string) (string, bool) {
	if !i.Host.has(binary) {
		return "", false
	}
	banner, err := i.Runner.Output(ctx, binary, "--version")
	if err != nil {
		return "", false
	}
	v := ParseBrowserVersion(banner)
	return v, v != ""
}

// putOnPath makes the binary just checked in installDir the one later steps
// find by name. A step inherits the job's environment rather than the one the
// previous step left, so an installDir not on PATH is put at the front of it
// through $BASH_ENV. That reaches bash steps, which run steps are by default.
//
// Everything else only warns, since the driver is installed and works by its
// full path regardless. An installDir already on PATH behind another copy is
// not moved ahead of it: the default /usr/local/bin would then also jump ahead
// of a version manager's shims, changing which node or python later steps run.
// Outside a CircleCI job there is no $BASH_ENV to write to.
func (i *Installer) putOnPath(installDir, binary string) error {
	dir, err := filepath.Abs(installDir)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", installDir, err)
	}
	want := filepath.Join(dir, binary)

	resolved, lookErr := i.Host.lookPath(binary)
	if lookErr == nil && samePath(resolved, want) {
		return nil
	}
	if slices.Contains(pathlink.SearchDirs(""), dir) {
		if lookErr == nil {
			i.logf("PATH resolves %s to %s, ahead of %s; later steps will run that copy unless they call %s or put %s earlier on PATH",
				binary, resolved, want, want, dir)
		}
		return nil
	}

	reason := dir + " is not on PATH"
	if lookErr == nil {
		reason = fmt.Sprintf("PATH resolves %s to %s, and %s is not on PATH", binary, resolved, dir)
	}
	// AddToPath writes the directory unquoted, so one the shell would split or
	// expand when sourcing $BASH_ENV is not written at all.
	if !shellSafePath.MatchString(dir) {
		i.logf("%s, and %s cannot be put on PATH safely; later steps should run %s by its full path", reason, dir, want)
		return nil
	}

	err = bashenv.AddToPath(dir)
	if errors.Is(err, bashenv.ErrNoBashEnv) {
		i.logf("%s, and $BASH_ENV is not set, so later steps will not find %s by name; run it as %s", reason, binary, want)
		return nil
	}
	if err != nil {
		return fmt.Errorf("put %s on PATH for later steps: %w", dir, err)
	}
	i.logf("%s; put %s at the front of PATH for later steps via $BASH_ENV", reason, dir)
	return nil
}

// shellSafePath matches a path bash reads back unchanged from an unquoted
// PATH assignment. A colon is left out because PATH would split on it.
var shellSafePath = regexp.MustCompile(`^[A-Za-z0-9._/+@~,=-]+$`)

// samePath reports whether a and b are the same file. A link on PATH to the
// installed binary runs that binary, so it counts.
func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(infoA, infoB)
}

func majorVersion(version string) (int, error) {
	head := version
	if idx := strings.Index(version, "."); idx > 0 {
		head = version[:idx]
	}
	major, err := strconv.Atoi(head)
	if err != nil {
		return 0, fmt.Errorf("cannot read a major version from %q", version)
	}
	return major, nil
}

func sameMajor(a, b string) bool {
	majorA, errA := majorVersion(a)
	majorB, errB := majorVersion(b)
	return errA == nil && errB == nil && majorA == majorB
}

// numericVersion strips anything that is not a digit or dot, which is how an
// "esr" or build suffix is dropped before a catalogue lookup.
func numericVersion(version string) string {
	var b strings.Builder
	for _, r := range version {
		if (r >= '0' && r <= '9') || r == '.' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
