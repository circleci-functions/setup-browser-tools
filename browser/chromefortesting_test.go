package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestChromeForTestingPlatform(t *testing.T) {
	tests := []struct {
		name    string
		distro  Distro
		arch    string
		want    string
		wantErr bool
	}{
		{name: "macOS arm64", distro: DistroMacOS, arch: ArchARM64, want: "mac-arm64"},
		{name: "macOS amd64", distro: DistroMacOS, arch: ArchAMD64, want: "mac-x64"},
		{name: "Debian amd64", distro: DistroDebian, arch: ArchAMD64, want: "linux64"},
		{name: "Debian arm64", distro: DistroDebian, arch: ArchARM64, want: "linux-arm64"},
		{name: "Alpine arm64 is unsupported", distro: DistroAlpine, arch: ArchARM64, wantErr: true},
		{name: "RHEL arm64 is unsupported", distro: DistroRHEL, arch: ArchARM64, wantErr: true},
		{name: "Alpine is unsupported rather than a silent no-op", distro: DistroAlpine, arch: ArchAMD64, wantErr: true},
		{name: "RHEL is unsupported rather than a silent no-op", distro: DistroRHEL, arch: ArchAMD64, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, _ := testInstaller(t, installerOpts{distro: tc.distro, arch: tc.arch})

			got, err := i.chromeForTestingPlatform()
			if tc.wantErr {
				assert.Assert(t, IsUnsupported(err), "got %v", err)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestInstallChromeForTestingUnsupportedPlatformsError(t *testing.T) {
	// These two exited 0 in the orb, having installed nothing, because its
	// if/elif chain had no else.
	for _, distro := range []Distro{DistroAlpine, DistroRHEL} {
		t.Run(string(distro), func(t *testing.T) {
			i, _ := testInstaller(t, installerOpts{distro: distro})

			err := i.InstallChromeForTesting(context.Background(), ChromeForTestingOptions{
				Version:    Latest,
				InstallDir: "/usr/local/bin",
			})
			assert.Assert(t, IsUnsupported(err), "got %v", err)
		})
	}
}

func TestChromeForTestingURL(t *testing.T) {
	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})

	assert.Equal(t, i.chromeForTestingURL("147.0.7727.55", "linux64", "chrome"),
		"https://storage.googleapis.com/chrome-for-testing-public/147.0.7727.55/linux64/chrome-linux64.zip")
	assert.Equal(t, i.chromeForTestingURL("147.0.7727.55", "mac-arm64", "chromedriver"),
		"https://storage.googleapis.com/chrome-for-testing-public/147.0.7727.55/mac-arm64/chromedriver-mac-arm64.zip")
}

func TestResolveChromeForTestingVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"channels":{"Stable":{"version":"147.0.7727.55"}}}`)
	}))
	defer srv.Close()

	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})
	i.Endpoints.ChromeForTesting = srv.URL
	i.Fetcher = Fetcher{HTTPClient: srv.Client()}

	got, err := i.resolveChromeForTestingVersion(context.Background(), Latest)
	assert.NilError(t, err)
	assert.Equal(t, got, "147.0.7727.55")

	got, err = i.resolveChromeForTestingVersion(context.Background(), "140.0.1.1")
	assert.NilError(t, err)
	assert.Equal(t, got, "140.0.1.1")
}

func TestInstallChromeForTestingUnpublishedVersionFailsOnEveryPlatform(t *testing.T) {
	// The orb's macOS branch had its exit commented out, so a bad version there
	// printed "doesn't exist" and succeeded, while Linux failed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	for _, tc := range []struct {
		name   string
		distro Distro
		arch   string
	}{
		{name: "macOS", distro: DistroMacOS, arch: ArchARM64},
		{name: "Debian", distro: DistroDebian, arch: ArchAMD64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i, _ := testInstaller(t, installerOpts{distro: tc.distro, arch: tc.arch})
			i.Endpoints.ChromeForTestingCDN = srv.URL
			i.Fetcher = Fetcher{HTTPClient: srv.Client()}

			platform, err := i.chromeForTestingPlatform()
			assert.NilError(t, err)

			err = i.installChromeForTestingBrowser(context.Background(), t.TempDir(), "999.0.0.0", platform, "/usr/local/bin")
			assert.Assert(t, err != nil, "an unpublished version must fail on %s", tc.name)
			assert.Assert(t, strings.Contains(err.Error(), "does not exist"), "got %v", err)
		})
	}
}

func TestReadDebDeps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deb.deps")
	assert.NilError(t, os.WriteFile(path,
		[]byte("libasound2 (>= 1.0.17)\n\nlibatk1.0-0 (>= 2.2.0)\n   libc6 (>= 2.17)   \n"), 0o600))

	deps, err := readDebDeps(path)
	assert.NilError(t, err)
	assert.DeepEqual(t, deps, []string{"libasound2 (>= 1.0.17)", "libatk1.0-0 (>= 2.2.0)", "libc6 (>= 2.17)"})

	_, err = readDebDeps(filepath.Join(dir, "absent"))
	assert.Assert(t, err != nil)
}

func TestSatisfyChromeForTestingDepsRunsOnePerLine(t *testing.T) {
	unpacked := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(unpacked, "deb.deps"),
		[]byte("libasound2 (>= 1.0.17)\nlibc6 (>= 2.17)\n"), 0o600))

	i, calls := testInstaller(t, installerOpts{distro: DistroDebian})
	assert.NilError(t, i.satisfyChromeForTestingDeps(context.Background(), unpacked))

	var satisfied []string
	for _, c := range *calls {
		if c.name == "apt-get" && len(c.args) > 0 && c.args[0] == "satisfy" {
			satisfied = append(satisfied, c.args[len(c.args)-1])
		}
	}
	assert.DeepEqual(t, satisfied, []string{"libasound2 (>= 1.0.17)", "libc6 (>= 2.17)"})
}

func TestSatisfyChromeForTestingDepsToleratesNoDebDeps(t *testing.T) {
	i, calls := testInstaller(t, installerOpts{distro: DistroDebian})

	assert.NilError(t, i.satisfyChromeForTestingDeps(context.Background(), t.TempDir()))
	for _, c := range *calls {
		assert.Assert(t, c.name != "apt-get", "nothing should be installed without deb.deps: %+v", *calls)
	}
}

func TestInstallChromeForTestingRejectsUnresolvableVersion(t *testing.T) {
	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})

	err := i.InstallChromeForTesting(context.Background(), ChromeForTestingOptions{
		Version:    "$CFT_NOT_SET",
		InstallDir: "/usr/local/bin",
	})
	assert.Assert(t, err != nil)
	assert.Assert(t, strings.Contains(err.Error(), "CFT_NOT_SET"))
}

func TestInstallChromeForTestingMakesTheTreeTraversable(t *testing.T) {
	// The tree is extracted as the job user and copied in as root, so without
	// widening the mode the job user cannot traverse it — and a symlink into it
	// fails to resolve, which a shell reports as "command not found".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		zip, err := os.ReadFile(filepath.Join("testdata", "archive.zip"))
		assert.NilError(t, err)
		_, _ = w.Write(zip)
	}))
	defer srv.Close()

	i, calls := testInstaller(t, installerOpts{distro: DistroMacOS, arch: ArchARM64})
	i.Endpoints.ChromeForTestingCDN = srv.URL
	i.Fetcher = Fetcher{HTTPClient: srv.Client()}

	// The copy will fail against the fixture archive's layout; the assertion is
	// about the mode fix being issued at all.
	_ = i.installChromeForTestingBrowser(context.Background(), t.TempDir(), "147.0.7727.55", "mac-arm64", "/usr/local/bin")

	var found bool
	for _, c := range *calls {
		if c.name == "chmod" && len(c.args) >= 2 && c.args[0] == "-R" && c.args[1] == "a+rX" {
			found = true
		}
	}
	assert.Assert(t, found, "the installed tree was never made traversable: %+v", *calls)
}
