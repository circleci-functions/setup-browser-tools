package browser

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/circleci-functions/toolkit/bashenv"
	"github.com/circleci-functions/toolkit/pathlink"

	"gotest.tools/v3/assert"
)

func TestResolveGeckodriverTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			http.Redirect(w, r, "/mozilla/geckodriver/releases/tag/v0.36.0", http.StatusFound)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})
	i.Endpoints.GeckodriverGitHub = srv.URL
	i.Fetcher = Fetcher{HTTPClient: srv.Client()}

	tests := []struct {
		name      string
		requested string
		want      string
	}{
		{name: "latest follows the redirect to a tag", requested: Latest, want: "v0.36.0"},
		{name: "an explicit tag is used as given", requested: "v0.35.0", want: "v0.35.0"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := i.resolveGeckodriverTag(context.Background(), tc.requested)
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestGeckodriverURL(t *testing.T) {
	tests := []struct {
		name   string
		distro Distro
		arch   string
		tag    string
		want   string
	}{
		{
			name:   "linux",
			distro: DistroDebian,
			arch:   ArchAMD64,
			tag:    "v0.36.0",
			want:   "https://github.com/mozilla/geckodriver/releases/download/v0.36.0/geckodriver-v0.36.0-linux64.tar.gz",
		},
		{
			name:   "linux arm64",
			distro: DistroDebian,
			arch:   ArchARM64,
			tag:    "v0.36.0",
			want:   "https://github.com/mozilla/geckodriver/releases/download/v0.36.0/geckodriver-v0.36.0-linux-aarch64.tar.gz",
		},
		{
			name:   "alpine arm64 takes the same static build",
			distro: DistroAlpine,
			arch:   ArchARM64,
			tag:    "v0.37.1",
			want:   "https://github.com/mozilla/geckodriver/releases/download/v0.37.1/geckodriver-v0.37.1-linux-aarch64.tar.gz",
		},
		{
			name:   "macOS arm64",
			distro: DistroMacOS,
			arch:   ArchARM64,
			tag:    "v0.36.0",
			want:   "https://github.com/mozilla/geckodriver/releases/download/v0.36.0/geckodriver-v0.36.0-macos-aarch64.tar.gz",
		},
		{
			name:   "macOS amd64 uses the x86-64 build",
			distro: DistroMacOS,
			arch:   ArchAMD64,
			tag:    "v0.35.0",
			want:   "https://github.com/mozilla/geckodriver/releases/download/v0.35.0/geckodriver-v0.35.0-macos.tar.gz",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, _ := testInstaller(t, installerOpts{distro: tc.distro, arch: tc.arch})
			assert.Equal(t, i.geckodriverURL(tc.tag), tc.want)
		})
	}
}

func TestInstallGeckodriverSkipsWhenAlreadyAtTheTag(t *testing.T) {
	// The tag carries a leading v that the binary's own banner does not, so a
	// naive comparison would reinstall on every run.
	i, calls := testInstaller(t, installerOpts{
		distro:  DistroDebian,
		onPath:  []string{"/usr/local/bin/geckodriver"},
		stdouts: map[string]string{"--version": "geckodriver 0.36.0 ( 2025-01-01)\n"},
	})

	assert.NilError(t, i.InstallGeckodriver(context.Background(), GeckodriverOptions{
		Version:    "v0.36.0",
		InstallDir: "/usr/local/bin",
	}))

	for _, c := range *calls {
		assert.Assert(t, c.name != "rm", "an up-to-date geckodriver was removed: %+v", *calls)
	}
}

func TestInstallGeckodriverRejectsUnresolvableVersion(t *testing.T) {
	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})

	err := i.InstallGeckodriver(context.Background(), GeckodriverOptions{
		Version:    "${GECKO_NOT_SET}",
		InstallDir: "/usr/local/bin",
	})
	assert.Assert(t, err != nil)
	assert.Assert(t, strings.Contains(err.Error(), "GECKO_NOT_SET"))
}

// fakeDriver is a stand-in driver binary: a script that prints banner, as
// `geckodriver --version` would.
func fakeDriver(banner string) string {
	return "#!/bin/sh\necho '" + banner + "'\n"
}

// writeFakeDriver puts an executable fakeDriver at path.
func writeFakeDriver(t *testing.T, path, banner string) {
	t.Helper()
	assert.NilError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	assert.NilError(t, os.WriteFile(path, []byte(fakeDriver(banner)), 0o755)) //nolint:gosec // the test runs it
}

type geckodriverRig struct {
	installer *Installer
	// sysBin is on PATH ahead of the system directories, standing in for
	// /usr/local/bin.
	sysBin  string
	bashEnv string
	log     *bytes.Buffer
}

// realGeckodriverInstall wires an installer that installs for real, into
// temporary directories: commands run, PATH is looked up as on the host, and
// the release served is a fakeDriver reporting served. Only the network is
// faked, which is what lets a test see what a later step would resolve.
func realGeckodriverInstall(t *testing.T, served string) geckodriverRig {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell to run the stand-in driver")
	}

	script := fakeDriver("geckodriver " + served + " (abc 2025-01-01)")
	archive, err := os.ReadFile(writeTarGz(t,
		[]*tar.Header{{Name: "geckodriver", Typeflag: tar.TypeReg, Size: int64(len(script)), Mode: 0o755}},
		[]string{script}))
	assert.NilError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	t.Cleanup(srv.Close)

	rig := geckodriverRig{
		sysBin:  t.TempDir(),
		bashEnv: filepath.Join(t.TempDir(), "bash_env"),
		log:     &bytes.Buffer{},
	}
	t.Setenv("PATH", strings.Join([]string{rig.sysBin, "/usr/bin", "/bin"}, string(os.PathListSeparator)))
	t.Setenv("BASH_ENV", rig.bashEnv)
	t.Setenv("HOME", t.TempDir())

	rig.installer = &Installer{
		Host:      Host{GOOS: "linux", GOARCH: ArchAMD64},
		Platform:  Platform{Distro: DistroDebian, Arch: ArchAMD64},
		Runner:    &Runner{Priv: rootPrivilege(), Out: io.Discard},
		Fetcher:   Fetcher{HTTPClient: srv.Client()},
		Endpoints: Endpoints{GeckodriverGitHub: srv.URL},
		Out:       rig.log,
	}
	return rig
}

func (r geckodriverRig) install(t *testing.T, version, installDir string) error {
	t.Helper()
	return r.installer.InstallGeckodriver(context.Background(), GeckodriverOptions{Version: version, InstallDir: installDir})
}

// exported is what the install left in $BASH_ENV for later steps.
func (r geckodriverRig) exported(t *testing.T) string {
	t.Helper()
	got, err := os.ReadFile(r.bashEnv)
	if errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	assert.NilError(t, err)
	return string(got)
}

// laterStepResolves is the geckodriver a later step finds by name, with PATH as
// $BASH_ENV leaves it.
func (r geckodriverRig) laterStepResolves(t *testing.T) string {
	t.Helper()
	path := os.Getenv("PATH")
	if _, err := os.Stat(r.bashEnv); err == nil {
		path, err = bashenv.Path(r.bashEnv)
		assert.NilError(t, err)
	}
	dirs := pathlink.SearchDirs(path)
	found := pathlink.IndexOf(dirs, "geckodriver")
	if found < 0 {
		return ""
	}
	return filepath.Join(dirs[found], "geckodriver")
}

func driverBanner(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command(path, "--version").Output() //nolint:gosec // a test fixture
	assert.NilError(t, err)
	return strings.TrimSpace(string(out))
}

func TestInstallGeckodriverIntoADirectoryOffPath(t *testing.T) {
	// The check after installing looked the driver up on PATH, so a directory
	// not on PATH failed with "no working geckodriver on PATH" although the
	// driver just installed there worked.
	rig := realGeckodriverInstall(t, "0.36.0")
	drivers := t.TempDir()

	assert.NilError(t, rig.install(t, "v0.36.0", drivers))

	want := filepath.Join(drivers, "geckodriver")
	assert.Equal(t, driverBanner(t, want), "geckodriver 0.36.0 (abc 2025-01-01)")
	assert.Equal(t, rig.exported(t), "export PATH="+drivers+":$PATH\n")
	assert.Equal(t, rig.laterStepResolves(t), want)
	assert.Assert(t, strings.Contains(rig.log.String(), drivers+" is not on PATH"), rig.log.String())
}

func TestInstallGeckodriverAheadOfAnOlderOneOnPath(t *testing.T) {
	// The check after installing ran the 0.35.0 PATH resolved, and failed with
	// "installed geckodriver is 0.35.0, but 0.36.0 was requested".
	rig := realGeckodriverInstall(t, "0.36.0")
	older := filepath.Join(rig.sysBin, "geckodriver")
	writeFakeDriver(t, older, "geckodriver 0.35.0")
	drivers := t.TempDir()

	assert.NilError(t, rig.install(t, "v0.36.0", drivers))

	assert.Equal(t, rig.exported(t), "export PATH="+drivers+":$PATH\n")
	assert.Equal(t, rig.laterStepResolves(t), filepath.Join(drivers, "geckodriver"))
	assert.Assert(t, strings.Contains(rig.log.String(), "PATH resolves geckodriver to "+older), rig.log.String())
	assert.Equal(t, driverBanner(t, older), "geckodriver 0.35.0",
		"a copy outside --install-dir is not this install's to replace")
}

func TestInstallGeckodriverIgnoresTheSameVersionElsewhereOnPath(t *testing.T) {
	// The check before installing found 0.35.0 on PATH, reported it already
	// installed, and wrote nothing into --install-dir.
	rig := realGeckodriverInstall(t, "0.35.0")
	writeFakeDriver(t, filepath.Join(rig.sysBin, "geckodriver"), "geckodriver 0.35.0")
	drivers := t.TempDir()

	assert.NilError(t, rig.install(t, "v0.35.0", drivers))

	assert.Equal(t, driverBanner(t, filepath.Join(drivers, "geckodriver")), "geckodriver 0.35.0 (abc 2025-01-01)")
	assert.Assert(t, !strings.Contains(rig.log.String(), "already installed"), rig.log.String())
	assert.Equal(t, rig.laterStepResolves(t), filepath.Join(drivers, "geckodriver"))
}

func TestInstallGeckodriverCreatesAMissingInstallDir(t *testing.T) {
	// cp failed with "cannot create regular file", and without sudo the
	// preflight said the directory needed root when it only needed creating.
	rig := realGeckodriverInstall(t, "0.36.0")
	rig.installer.Runner.Priv = Privilege{}
	drivers := filepath.Join(t.TempDir(), "home", "drivers")

	assert.NilError(t, rig.install(t, "v0.36.0", drivers))

	assert.Equal(t, driverBanner(t, filepath.Join(drivers, "geckodriver")), "geckodriver 0.36.0 (abc 2025-01-01)")
	assert.Equal(t, rig.laterStepResolves(t), filepath.Join(drivers, "geckodriver"))
}

func TestInstallGeckodriverOnPathExportsNothing(t *testing.T) {
	// The default /usr/local/bin is already on PATH, so later steps should see
	// PATH exactly as before.
	rig := realGeckodriverInstall(t, "0.36.0")

	assert.NilError(t, rig.install(t, "v0.36.0", rig.sysBin))

	assert.Equal(t, rig.exported(t), "")
	assert.Equal(t, rig.laterStepResolves(t), filepath.Join(rig.sysBin, "geckodriver"))
}

func TestInstallGeckodriverLeavesPathOrderAloneWhenShadowed(t *testing.T) {
	// The install dir is on PATH behind another copy. Putting it at the front
	// would also move everything else in it, the default /usr/local/bin
	// included, ahead of a version manager's shims, so this only warns.
	rig := realGeckodriverInstall(t, "0.36.0")
	shims := t.TempDir()
	writeFakeDriver(t, filepath.Join(shims, "geckodriver"), "geckodriver 0.35.0")
	t.Setenv("PATH", strings.Join([]string{shims, rig.sysBin, "/usr/bin", "/bin"}, string(os.PathListSeparator)))

	assert.NilError(t, rig.install(t, "v0.36.0", rig.sysBin))

	assert.Equal(t, rig.exported(t), "")
	assert.Equal(t, driverBanner(t, filepath.Join(rig.sysBin, "geckodriver")), "geckodriver 0.36.0 (abc 2025-01-01)")
	assert.Assert(t, strings.Contains(rig.log.String(),
		"PATH resolves geckodriver to "+filepath.Join(shims, "geckodriver")+", ahead of "+filepath.Join(rig.sysBin, "geckodriver")),
		rig.log.String())
}

func TestInstallGeckodriverDoesNotExportAnUnsafeDirectory(t *testing.T) {
	// bashenv.AddToPath writes the directory unquoted, so a space would split
	// the PATH assignment when a later step sources $BASH_ENV.
	rig := realGeckodriverInstall(t, "0.36.0")
	drivers := filepath.Join(t.TempDir(), "my drivers")

	assert.NilError(t, rig.install(t, "v0.36.0", drivers))

	assert.Equal(t, rig.exported(t), "")
	assert.Equal(t, driverBanner(t, filepath.Join(drivers, "geckodriver")), "geckodriver 0.36.0 (abc 2025-01-01)")
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

func TestInstallGeckodriverAlreadyInADirectoryOffPath(t *testing.T) {
	// A rerun, or a directory restored from a cache, skips the download but
	// still has to leave the driver resolvable for later steps.
	rig := realGeckodriverInstall(t, "0.36.0")
	drivers := t.TempDir()
	writeFakeDriver(t, filepath.Join(drivers, "geckodriver"), "geckodriver 0.36.0 (abc 2025-01-01)")

	assert.NilError(t, rig.install(t, "v0.36.0", drivers))

	assert.Assert(t, strings.Contains(rig.log.String(), "already installed"), rig.log.String())
	assert.Equal(t, rig.exported(t), "export PATH="+drivers+":$PATH\n")
}

func TestInstallGeckodriverOffPathOutsideAJob(t *testing.T) {
	// Without $BASH_ENV there is nowhere to export to, but the driver is
	// installed and works by its full path, so that only warns.
	rig := realGeckodriverInstall(t, "0.36.0")
	t.Setenv("BASH_ENV", "")
	drivers := t.TempDir()

	assert.NilError(t, rig.install(t, "v0.36.0", drivers))

	assert.Assert(t, strings.Contains(rig.log.String(), "$BASH_ENV is not set"), rig.log.String())
	assert.Equal(t, driverBanner(t, filepath.Join(drivers, "geckodriver")), "geckodriver 0.36.0 (abc 2025-01-01)")
}
