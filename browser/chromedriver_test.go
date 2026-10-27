package browser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// chromeForTestingServer serves the two catalogues the 115+ path reads. The
// known-good catalogue deliberately omits 150.0.1.1 so the milestone fallback
// can be exercised.
func chromeForTestingServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/known-good-versions-with-downloads.json"):
			fmt.Fprint(w, `{"versions":[
				{"version":"120.0.6099.109","downloads":{"chromedriver":[
					{"platform":"linux64","url":"https://cft.example/120/linux64/chromedriver.zip"},
					{"platform":"mac-arm64","url":"https://cft.example/120/mac-arm64/chromedriver.zip"}]}}
			]}`)
		case strings.HasSuffix(r.URL.Path, "/latest-versions-per-milestone-with-downloads.json"):
			fmt.Fprint(w, `{"milestones":{"150":{"version":"150.0.9999.9","downloads":{"chromedriver":[
				{"platform":"linux64","url":"https://cft.example/150/linux64/chromedriver.zip"}]}}}}`)
		case strings.Contains(r.URL.Path, "/LATEST_RELEASE_"):
			fmt.Fprint(w, "114.0.5735.90\n")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestResolveChromeDriverBoundaries(t *testing.T) {
	srv := chromeForTestingServer(t)
	defer srv.Close()

	tests := []struct {
		name        string
		chrome      string
		wantVersion string
		wantURLPart string
		wantErr     string
	}{
		{
			name:    "below 43 is rejected",
			chrome:  "42.0.1.1",
			wantErr: "too old",
		},
		{
			name:        "43 comes from the table",
			chrome:      "43.0.1.1",
			wantVersion: "2.20",
			wantURLPart: "/2.20/chromedriver_linux64.zip",
		},
		{
			name:        "69 is the last table entry",
			chrome:      "69.0.1.1",
			wantVersion: "2.44",
			wantURLPart: "/2.44/chromedriver_linux64.zip",
		},
		{
			name:        "70 uses the LATEST_RELEASE endpoint",
			chrome:      "70.0.1.1",
			wantVersion: "114.0.5735.90",
			wantURLPart: "/114.0.5735.90/chromedriver_linux64.zip",
		},
		{
			name:        "114 still uses the LATEST_RELEASE endpoint",
			chrome:      "114.0.5735.90",
			wantVersion: "114.0.5735.90",
			wantURLPart: "chromedriver_linux64.zip",
		},
		{
			name:        "115 and above use the Chrome for Testing catalogue",
			chrome:      "120.0.6099.109",
			wantVersion: "120.0.6099.109",
			wantURLPart: "https://cft.example/120/linux64/chromedriver.zip",
		},
		{
			name:        "no exact match falls back to the milestone build",
			chrome:      "150.0.1.1",
			wantVersion: "150.0.9999.9",
			wantURLPart: "https://cft.example/150/linux64/chromedriver.zip",
		},
		{
			name:    "an unknown milestone is an error",
			chrome:  "160.0.1.1",
			wantErr: "milestone 160",
		},
		{
			name:    "an unparseable version is an error",
			chrome:  "not-a-version",
			wantErr: "major version",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, _ := testInstaller(t, installerOpts{distro: DistroDebian})
			i.Endpoints.ChromeDriverLegacy = srv.URL
			i.Endpoints.ChromeForTesting = srv.URL
			i.Fetcher = Fetcher{HTTPClient: srv.Client()}

			version, url, err := i.resolveChromeDriver(context.Background(), tc.chrome)

			if tc.wantErr != "" {
				assert.Assert(t, err != nil, "got %s %s", version, url)
				assert.Assert(t, strings.Contains(err.Error(), tc.wantErr),
					"error %q should contain %q", err.Error(), tc.wantErr)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, version, tc.wantVersion)
			assert.Assert(t, strings.Contains(url, tc.wantURLPart), "url %q should contain %q", url, tc.wantURLPart)
		})
	}
}

func TestLegacyChromeDriverTableIsComplete(t *testing.T) {
	// Every Chrome major from 43 to 69 must resolve, or the port silently
	// narrowed the range the orb supported.
	for major := chromeDriverMinMajor; major < 70; major++ {
		v, ok := legacyChromeDriverVersions[major]
		assert.Assert(t, ok, "Chrome %d has no ChromeDriver mapping", major)
		assert.Assert(t, v != "", "Chrome %d maps to an empty version", major)
	}
	assert.Equal(t, len(legacyChromeDriverVersions), 70-chromeDriverMinMajor)
}

func TestDriverPlatform(t *testing.T) {
	tests := []struct {
		name   string
		distro Distro
		arch   string
		want   string
	}{
		{name: "linux amd64", distro: DistroDebian, arch: ArchAMD64, want: "linux64"},
		{name: "macOS arm64", distro: DistroMacOS, arch: ArchARM64, want: "mac-arm64"},
		{name: "macOS amd64", distro: DistroMacOS, arch: ArchAMD64, want: "mac-x64"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, _ := testInstaller(t, installerOpts{distro: tc.distro, arch: tc.arch})
			assert.Equal(t, i.driverPlatform(), tc.want)
		})
	}
}

func TestChromeForTestingPicksTheMatchingPlatform(t *testing.T) {
	srv := chromeForTestingServer(t)
	defer srv.Close()

	i, _ := testInstaller(t, installerOpts{distro: DistroMacOS, arch: ArchARM64})
	i.Endpoints.ChromeForTesting = srv.URL
	i.Fetcher = Fetcher{HTTPClient: srv.Client()}

	_, url, err := i.resolveChromeDriver(context.Background(), "120.0.6099.109")
	assert.NilError(t, err)
	assert.Equal(t, url, "https://cft.example/120/mac-arm64/chromedriver.zip")
}

func TestChromeForTestingErrorsWhenPlatformMissingFromMilestone(t *testing.T) {
	srv := chromeForTestingServer(t)
	defer srv.Close()

	// The milestone fixture only publishes linux64, so macOS must fail rather
	// than install a Linux binary.
	i, _ := testInstaller(t, installerOpts{distro: DistroMacOS, arch: ArchARM64})
	i.Endpoints.ChromeForTesting = srv.URL
	i.Fetcher = Fetcher{HTTPClient: srv.Client()}

	_, _, err := i.resolveChromeDriver(context.Background(), "150.0.1.1")
	assert.Assert(t, err != nil)
	assert.Assert(t, strings.Contains(err.Error(), "mac-arm64"), "got %v", err)
}

func TestInstallChromeDriverRejectsLinuxARM64(t *testing.T) {
	i, _ := testInstaller(t, installerOpts{distro: DistroDebian, arch: ArchARM64})

	err := i.InstallChromeDriver(context.Background(), ChromeDriverOptions{InstallDir: "/usr/local/bin"})
	assert.Assert(t, IsUnsupported(err), "got %v", err)
}

func TestInstallChromeDriverNeedsChrome(t *testing.T) {
	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})

	err := i.InstallChromeDriver(context.Background(), ChromeDriverOptions{InstallDir: "/usr/local/bin"})
	assert.Assert(t, err != nil)
	assert.Assert(t, strings.Contains(err.Error(), "Chrome is not installed"))
	assert.Assert(t, !IsUnsupported(err))
}

func TestDownloadAndInstallBinaryLeavesWorkingDirClean(t *testing.T) {
	// The orb unzipped in the checkout and then deleted the stray
	// LICENSE.chromedriver it left behind.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		zip, err := os.ReadFile(filepath.Join("testdata", "archive.zip"))
		assert.NilError(t, err)
		_, _ = w.Write(zip)
	}))
	defer srv.Close()

	workDir := t.TempDir()
	before, err := os.ReadDir(workDir)
	assert.NilError(t, err)

	installDir := t.TempDir()
	i, calls := testInstaller(t, installerOpts{distro: DistroDebian})
	i.Fetcher = Fetcher{HTTPClient: srv.Client()}

	assert.NilError(t, i.downloadAndInstallBinary(
		context.Background(), srv.URL+"/chromedriver.zip", "chromedriver", installDir, "chromedriver.zip"))

	after, err := os.ReadDir(workDir)
	assert.NilError(t, err)
	assert.Equal(t, len(before), len(after), "the working directory gained files")

	var copied *invocation
	for idx := range *calls {
		if c := (*calls)[idx]; c.name == "cp" {
			copied = &c
		}
	}
	assert.Assert(t, copied != nil, "the binary was never copied: %+v", *calls)
	assert.Equal(t, copied.args[1], filepath.Join(installDir, "chromedriver"))
	// The archive nests nothing extra into installDir — only the binary is moved.
	assert.Assert(t, strings.HasSuffix(copied.args[0], "chromedriver"), "got %v", copied.args)
}

func TestFindFile(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "chromedriver-linux64")
	assert.NilError(t, os.MkdirAll(nested, 0o750))
	assert.NilError(t, os.WriteFile(filepath.Join(nested, "chromedriver"), []byte("bin"), 0o600))
	assert.NilError(t, os.WriteFile(filepath.Join(root, "LICENSE.chromedriver"), []byte("licence"), 0o600))

	got, err := findFile(root, "chromedriver")
	assert.NilError(t, err)
	assert.Equal(t, got, filepath.Join(nested, "chromedriver"))

	_, err = findFile(root, "msedgedriver")
	assert.Assert(t, err != nil)
	assert.Assert(t, strings.Contains(err.Error(), "was not in the downloaded archive"))
}

func TestMajorVersionAndNumericVersion(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantMajor   int
		wantNumeric string
		wantErr     bool
	}{
		{name: "chrome", in: "147.0.7727.55", wantMajor: 147, wantNumeric: "147.0.7727.55"},
		{name: "major only", in: "115", wantMajor: 115, wantNumeric: "115"},
		{name: "esr suffix is stripped", in: "140.9.1esr", wantMajor: 140, wantNumeric: "140.9.1"},
		{name: "not a version", in: "abc", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			major, err := majorVersion(tc.in)
			if tc.wantErr {
				assert.Assert(t, err != nil)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, major, tc.wantMajor)
			assert.Equal(t, numericVersion(tc.in), tc.wantNumeric)
		})
	}
}

// pathRig is an installer whose PATH lookups are the host's own, over a PATH
// of temporary directories, for the cases of putOnPath a real install can't
// cheaply set up. The acceptance suite covers the rest.
type pathRig struct {
	installer *Installer
	bashEnv   string
	log       *bytes.Buffer
}

func newPathRig(t *testing.T, path ...string) pathRig {
	t.Helper()
	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})
	i.Host.LookPath = nil
	log := &bytes.Buffer{}
	i.Out = log
	t.Setenv("PATH", strings.Join(append(path, "/usr/bin", "/bin"), string(os.PathListSeparator)))
	return pathRig{installer: i, bashEnv: os.Getenv("BASH_ENV"), log: log}
}

// exported is what putOnPath left in $BASH_ENV for later steps.
func (r pathRig) exported(t *testing.T) string {
	t.Helper()
	got, err := os.ReadFile(r.bashEnv)
	if errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	assert.NilError(t, err)
	return string(got)
}

// writeGeckodriver puts an executable geckodriver in dir.
func writeGeckodriver(t *testing.T, dir string) {
	t.Helper()
	assert.NilError(t, os.MkdirAll(dir, 0o750))
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "geckodriver"), []byte("#!/bin/sh\n"), 0o755)) //nolint:gosec // must be executable
}

func TestPutOnPathLeavesPathOrderAloneWhenShadowed(t *testing.T) {
	// The install dir is on PATH behind another copy. Putting it at the front
	// would also move everything else in it, the default /usr/local/bin
	// included, ahead of a version manager's shims, so this only warns.
	shims, installDir := t.TempDir(), t.TempDir()
	writeGeckodriver(t, shims)
	writeGeckodriver(t, installDir)
	rig := newPathRig(t, shims, installDir)

	assert.NilError(t, rig.installer.putOnPath(installDir, "geckodriver"))

	assert.Equal(t, rig.exported(t), "")
	assert.Assert(t, strings.Contains(rig.log.String(),
		"PATH resolves geckodriver to "+filepath.Join(shims, "geckodriver")+", ahead of "+filepath.Join(installDir, "geckodriver")),
		rig.log.String())
}

func TestPutOnPathDoesNotExportAnUnsafeDirectory(t *testing.T) {
	// bashenv.AddToPath writes the directory unquoted, so a space would split
	// the PATH assignment when a later step sources $BASH_ENV.
	installDir := filepath.Join(t.TempDir(), "my drivers")
	writeGeckodriver(t, installDir)
	rig := newPathRig(t)

	assert.NilError(t, rig.installer.putOnPath(installDir, "geckodriver"))

	assert.Equal(t, rig.exported(t), "")
	assert.Assert(t, strings.Contains(rig.log.String(), "cannot be put on PATH safely"), rig.log.String())
}

func TestShellSafePath(t *testing.T) {
	for _, dir := range []string{"/usr/local/bin", "/home/circleci/drivers", "/opt/a.b_c+d@e~f,g=h-i"} {
		assert.Assert(t, shellSafePath.MatchString(dir), dir)
	}
	for _, dir := range []string{"/my drivers", "/a:b", "/a$HOME", "/a`x`", "/a;b", "/a'b", `/a"b`, "/a*b", "/a\tb", ""} {
		assert.Assert(t, !shellSafePath.MatchString(dir), dir)
	}
}

func TestPutOnPathOutsideAJob(t *testing.T) {
	// Without $BASH_ENV there is nowhere to export to, but the driver is
	// installed and works by its full path, so that only warns.
	installDir := t.TempDir()
	writeGeckodriver(t, installDir)
	rig := newPathRig(t)
	t.Setenv("BASH_ENV", "")

	assert.NilError(t, rig.installer.putOnPath(installDir, "geckodriver"))

	assert.Assert(t, strings.Contains(rig.log.String(), "$BASH_ENV is not set"), rig.log.String())
}

func TestPutOnPathFailsWhenBashEnvCannotBeWritten(t *testing.T) {
	// $BASH_ENV is set, so this is a job, and later steps were promised the
	// driver by name; a write that fails has to fail the step.
	installDir := t.TempDir()
	writeGeckodriver(t, installDir)
	rig := newPathRig(t)
	t.Setenv("BASH_ENV", filepath.Join(t.TempDir(), "missing", "bash_env"))

	err := rig.installer.putOnPath(installDir, "geckodriver")
	assert.ErrorContains(t, err, "put "+installDir+" on PATH for later steps")
}
