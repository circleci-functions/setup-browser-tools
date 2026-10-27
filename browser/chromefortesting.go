package browser

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// chromeForTestingTree is where the build is unpacked. The orb uses /opt rather
// than an app bundle on macOS too, treating it as temporary.
const chromeForTestingTree = "/opt/chrome-for-testing"

type ChromeForTestingOptions struct {
	Version      string
	InstallDir   string
	ChromeDriver bool
}

func (i *Installer) InstallChromeForTesting(ctx context.Context, opts ChromeForTestingOptions) error {
	platform, err := i.chromeForTestingPlatform()
	if err != nil {
		return err
	}
	if err := i.chromeForTestingPrivilege(opts.InstallDir); err != nil {
		return err
	}

	requested, err := ExpandVersion(opts.Version, "--version", nil)
	if err != nil {
		return err
	}

	version, err := i.resolveChromeForTestingVersion(ctx, requested)
	if err != nil {
		return err
	}
	i.logf("installing Chrome for Testing %s", version)

	dir, cleanup, err := TempDir("chrome-for-testing")
	if err != nil {
		return err
	}
	defer cleanup()

	if err := i.installChromeForTestingBrowser(ctx, dir, version, platform, opts.InstallDir); err != nil {
		return err
	}
	RecordVersion(i.Out, "Chrome for Testing", version)
	i.exportBrowserBin("Chrome for Testing", chromeBinEnv, absPath(filepath.Join(opts.InstallDir, "chrome")))

	if !opts.ChromeDriver {
		return nil
	}
	return i.installChromeForTestingDriver(ctx, version, platform, opts.InstallDir)
}

// chromeForTestingPlatform returns the download target, or an unsupported error.
// Only macOS and Debian are supported, since the Linux build's libraries are
// installed from its deb.deps; the orb's if/elif had no else, so on Alpine and
// RHEL it exited 0 having installed nothing. Google publishes linux-arm64 builds
// from 153.0.8001.0, so an older version there fails as unpublished.
func (i *Installer) chromeForTestingPlatform() (string, error) {
	switch {
	case i.Platform.IsMacOS() && i.Platform.Arch == ArchAMD64:
		return "mac-x64", nil
	case i.Platform.IsMacOS():
		return "mac-arm64", nil
	case i.Platform.Distro == DistroDebian && i.Platform.Arch == ArchAMD64:
		return "linux64", nil
	case i.Platform.Distro == DistroDebian && i.Platform.Arch == ArchARM64:
		return "linux-arm64", nil
	default:
		return "", Unsupported("Chrome for Testing", i.Platform, "builds are published for macOS and Debian/Ubuntu only")
	}
}

// chromeForTestingPrivilege checks what the install will need before anything
// is downloaded: apt-get for the build's libraries on Debian, and on macOS the
// unpack and symlink directories.
func (i *Installer) chromeForTestingPrivilege(installDir string) error {
	if i.Platform.Distro == DistroDebian {
		return i.requirePrivilege("Chrome for Testing", "run apt-get")
	}
	return i.requireWritable("Chrome for Testing", filepath.Dir(chromeForTestingTree), installDir)
}

func (i *Installer) resolveChromeForTestingVersion(ctx context.Context, requested string) (string, error) {
	if requested != Latest {
		return requested, nil
	}

	var good struct {
		Channels struct {
			Stable struct {
				Version string `json:"version"`
			} `json:"Stable"`
		} `json:"channels"`
	}
	if err := i.Fetcher.JSON(ctx, i.Endpoints.ChromeForTesting+"/last-known-good-versions.json", &good); err != nil {
		return "", err
	}
	if good.Channels.Stable.Version == "" {
		return "", fmt.Errorf("no stable Chrome for Testing version reported")
	}
	return good.Channels.Stable.Version, nil
}

func (i *Installer) chromeForTestingURL(version, platform, artefact string) string {
	return fmt.Sprintf("%s/%s/%s/%s-%s.zip", i.Endpoints.ChromeForTestingCDN, version, platform, artefact, platform)
}

// installChromeForTestingBrowser fetches and unpacks the browser. A version that
// was never published is an error on every platform: the orb's macOS branch had
// its exit commented out, so a bad version there printed a message and succeeded.
func (i *Installer) installChromeForTestingBrowser(ctx context.Context, dir, version, platform, installDir string) error {
	url := i.chromeForTestingURL(version, platform, "chrome")

	published, err := i.Fetcher.Exists(ctx, url)
	if err != nil {
		return err
	}
	if !published {
		return fmt.Errorf("Chrome for Testing %s does not exist for %s", version, platform) //nolint:staticcheck // proper noun
	}

	archive, err := i.Fetcher.Download(ctx, url, dir, "chrome-for-testing.zip")
	if err != nil {
		return err
	}

	extracted := filepath.Join(dir, "chrome")
	if err := mkdirAll(extracted); err != nil {
		return err
	}
	if err := Extract(archive, extracted); err != nil {
		return err
	}

	unpacked := filepath.Join(extracted, "chrome-"+platform)
	_ = i.Runner.Exec(ctx, "rm", "-rf", chromeForTestingTree)
	if err := i.Runner.Exec(ctx, "cp", "-R", unpacked, chromeForTestingTree); err != nil {
		return err
	}
	// The tree is extracted as the job user and then copied in as root, which
	// leaves it owned by root with the extractor's own directory mode. Without
	// this the job user cannot traverse it, and a symlink into it fails to
	// resolve — which a shell reports as "command not found" rather than as a
	// permissions problem.
	if err := i.Runner.Exec(ctx, "chmod", "-R", "a+rX", chromeForTestingTree); err != nil {
		return err
	}

	// The symlink comes before the driver, so the directory the driver install
	// would create has to exist by now.
	if err := i.Runner.Exec(ctx, "mkdir", "-p", installDir); err != nil {
		return err
	}
	if err := i.Runner.Exec(ctx, "ln", "-sf",
		filepath.Join(chromeForTestingTree, "chrome"), filepath.Join(installDir, "chrome")); err != nil {
		return err
	}

	if i.Platform.Distro == DistroDebian {
		if err := i.satisfyChromeForTestingDeps(ctx, unpacked); err != nil {
			return err
		}
	}

	// The driver was verified but the browser never was, so a tree that copied
	// into place unusable still reported success.
	if _, err := i.Runner.Output(ctx, filepath.Join(installDir, "chrome"), "--version"); err != nil {
		return fmt.Errorf("Chrome for Testing %s did not run after installing: %w", version, err) //nolint:staticcheck // proper noun
	}
	return nil
}

// satisfyChromeForTestingDeps installs the shared libraries the build lists in
// deb.deps, one apt-get satisfy per line, as the orb does.
func (i *Installer) satisfyChromeForTestingDeps(ctx context.Context, unpacked string) error {
	deps, err := readDebDeps(filepath.Join(unpacked, "deb.deps"))
	if err != nil {
		i.logf("no deb.deps in the Chrome for Testing build; skipping dependency install")
		return nil
	}

	if err := i.Runner.AptUpdate(ctx); err != nil {
		return err
	}
	for _, dep := range deps {
		if _, err := i.Runner.Run(ctx, Command{
			Name: "apt-get",
			Args: []string{"satisfy", "-y", "--no-install-recommends", dep},
			Env:  []string{"DEBIAN_FRONTEND=noninteractive"},
		}); err != nil {
			return err
		}
	}
	return nil
}

func readDebDeps(path string) ([]string, error) {
	f, err := os.Open(path) //nolint:gosec // path is inside a directory this process just extracted
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var deps []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			deps = append(deps, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return deps, nil
}

func (i *Installer) installChromeForTestingDriver(ctx context.Context, version, platform, installDir string) error {
	url := i.chromeForTestingURL(version, platform, "chromedriver")
	if err := i.downloadAndInstallBinary(ctx, url, "chromedriver", installDir, "chrome-for-testing-driver.zip"); err != nil {
		return err
	}

	driver := filepath.Join(installDir, "chromedriver")
	installed, found := i.installedDriverVersion(ctx, driver)
	if !found {
		return fmt.Errorf("no working chromedriver at %s after installing %s", driver, version)
	}
	if installed != version {
		return fmt.Errorf("installed chromedriver is %s, but %s was requested", installed, version)
	}
	i.logf("installed Chrome for Testing chromedriver %s", installed)
	RecordVersion(i.Out, "Chrome for Testing chromedriver", installed)
	return i.putOnPath(installDir, "chromedriver")
}
