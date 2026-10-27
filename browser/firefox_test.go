package browser

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/circleci-functions/toolkit/sdk"
	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"gotest.tools/v3/assert"
)

func TestFirefoxRelease(t *testing.T) {
	tests := []struct {
		name         string
		distro       Distro
		arch         string
		version      string
		wantFilename string
		wantURL      string
	}{
		{
			name:         "linux at 135 uses tar.xz",
			distro:       DistroDebian,
			version:      "135.0",
			wantFilename: "firefox-135.0.tar.xz",
			wantURL:      "https://archive.mozilla.org/pub/firefox/releases/135.0/linux-x86_64/en-US/firefox-135.0.tar.xz",
		},
		{
			name:         "linux below 135 uses tar.bz2",
			distro:       DistroDebian,
			version:      "134.0.2",
			wantFilename: "firefox-134.0.2.tar.bz2",
			wantURL:      "https://archive.mozilla.org/pub/firefox/releases/134.0.2/linux-x86_64/en-US/firefox-134.0.2.tar.bz2",
		},
		{
			name:         "linux well above 135",
			distro:       DistroDebian,
			version:      "149.0.2",
			wantFilename: "firefox-149.0.2.tar.xz",
			wantURL:      "https://archive.mozilla.org/pub/firefox/releases/149.0.2/linux-x86_64/en-US/firefox-149.0.2.tar.xz",
		},
		{
			name:         "esr release",
			distro:       DistroDebian,
			version:      "140.9.1esr",
			wantFilename: "firefox-140.9.1esr.tar.xz",
			wantURL:      "https://archive.mozilla.org/pub/firefox/releases/140.9.1esr/linux-x86_64/en-US/firefox-140.9.1esr.tar.xz",
		},
		{
			name:         "macOS uses a dmg with a space in the name",
			distro:       DistroMacOS,
			arch:         ArchARM64,
			version:      "149.0.2",
			wantFilename: "Firefox 149.0.2.dmg",
			wantURL:      "https://archive.mozilla.org/pub/firefox/releases/149.0.2/mac/en-US/Firefox%20149.0.2.dmg",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, _ := testInstaller(t, installerOpts{distro: tc.distro, arch: tc.arch})

			rel, err := i.firefoxRelease(tc.version)
			assert.NilError(t, err)
			assert.Equal(t, rel.Filename, tc.wantFilename)
			assert.Equal(t, rel.URL, tc.wantURL)
		})
	}
}

func TestFirefoxReleaseManifestEntryIsTheFullPath(t *testing.T) {
	// One Mozilla manifest lists the same base name for every architecture and
	// every locale, so the lookup has to use the full path.
	tests := []struct {
		name    string
		distro  Distro
		arch    string
		version string
		want    string
	}{
		{
			name:    "linux",
			distro:  DistroDebian,
			arch:    ArchAMD64,
			version: "149.0.2",
			want:    "linux-x86_64/en-US/firefox-149.0.2.tar.xz",
		},
		{
			name:    "macOS",
			distro:  DistroMacOS,
			arch:    ArchARM64,
			version: "149.0.2",
			want:    "mac/en-US/Firefox 149.0.2.dmg",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, _ := testInstaller(t, installerOpts{distro: tc.distro, arch: tc.arch})

			rel, err := i.firefoxRelease(tc.version)
			assert.NilError(t, err)
			assert.Equal(t, rel.ManifestEntry, tc.want)
			// The local copy keeps just the file name.
			assert.Equal(t, rel.Filename, filepath.Base(tc.want))
		})
	}
}

func TestResolveFirefoxVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"LATEST_FIREFOX_VERSION":"149.0.2","FIREFOX_ESR":"140.9.1esr"}`)
	}))
	defer srv.Close()

	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})
	i.Endpoints.MozillaProduct = srv.URL
	i.Fetcher = Fetcher{HTTPClient: srv.Client()}

	tests := []struct {
		name      string
		requested string
		want      string
	}{
		{name: "latest reads LATEST_FIREFOX_VERSION", requested: Latest, want: "149.0.2"},
		{name: "a pinned version passes through", requested: "134.0.2", want: "134.0.2"},
		{name: "an esr version passes through", requested: "140.9.1esr", want: "140.9.1esr"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := i.resolveFirefoxVersion(context.Background(), tc.requested)
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestResolveFirefoxVersionEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})
	i.Endpoints.MozillaProduct = srv.URL
	i.Fetcher = Fetcher{HTTPClient: srv.Client()}

	_, err := i.resolveFirefoxVersion(context.Background(), Latest)
	assert.Assert(t, err != nil)
	assert.Assert(t, strings.Contains(err.Error(), "no latest Firefox version"))
}

func TestFirefoxCacheKey(t *testing.T) {
	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})

	tests := []struct {
		name string
		opts FirefoxOptions
		want string
	}{
		{
			name: "caching off gives no key",
			opts: FirefoxOptions{UseCache: false, Version: "149.0.2", CacheKey: "v1"},
			want: "",
		},
		{
			name: "latest is never cached",
			opts: FirefoxOptions{UseCache: true, Version: Latest, CacheKey: "v1"},
			want: "",
		},
		{
			name: "a pinned version with caching on",
			opts: FirefoxOptions{UseCache: true, Version: "149.0.2", CacheKey: "v1"},
			want: "setup-browser-tools-firefox-release-v1-149.0.2-linux-amd64",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, i.firefoxCacheKey("149.0.2", tc.opts), tc.want)
		})
	}
}

func TestLinkFirefoxCreatesBothSymlinks(t *testing.T) {
	tests := []struct {
		name       string
		installDir string
		wantLinks  []string
	}{
		{
			name:       "a custom install dir also gets a /usr/local/bin link",
			installDir: "/home/circleci/bin",
			wantLinks:  []string{"/home/circleci/bin/firefox", "/usr/local/bin/firefox"},
		},
		{
			name:       "the default install dir gets one link, not two identical ones",
			installDir: "/usr/local/bin",
			wantLinks:  []string{"/usr/local/bin/firefox"},
		},
		{
			name:       "a trailing slash is still recognised as /usr/local/bin",
			installDir: "/usr/local/bin/",
			wantLinks:  []string{"/usr/local/bin/firefox"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, calls := testInstaller(t, installerOpts{distro: DistroDebian})

			assert.NilError(t, i.linkFirefox(context.Background(), "/opt/firefox-149.0.2/firefox", tc.installDir))

			var links []string
			for _, c := range *calls {
				if c.name == "ln" {
					links = append(links, c.args[len(c.args)-1])
				}
			}
			assert.DeepEqual(t, links, tc.wantLinks)
		})
	}
}

func TestInstallFirefoxUnsupportedPlatforms(t *testing.T) {
	tests := []struct {
		name       string
		distro     Distro
		arch       string
		wantReason string
	}{
		{name: "Alpine", distro: DistroAlpine, arch: ArchAMD64, wantReason: "Alpine build"},
		{name: "Linux ARM64", distro: DistroDebian, arch: ArchARM64, wantReason: "linux-x86_64"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, _ := testInstaller(t, installerOpts{distro: tc.distro, arch: tc.arch})

			err := i.InstallFirefox(context.Background(), FirefoxOptions{Version: Latest, InstallDir: "/usr/local/bin"})
			assert.Assert(t, IsUnsupported(err), "got %v", err)
			assert.Assert(t, strings.Contains(err.Error(), tc.wantReason), "got %v", err)
		})
	}
}

func TestVerifyFirefox(t *testing.T) {
	tests := []struct {
		name    string
		banner  string
		onPath  []string
		version string
		want    string
		wantErr bool
	}{
		{
			name:    "exact match",
			onPath:  []string{"firefox"},
			banner:  "Mozilla Firefox 149.0.2\n",
			version: "149.0.2",
			want:    "149.0.2",
		},
		{
			name:    "an esr banner drops the suffix the version carries",
			onPath:  []string{"firefox"},
			banner:  "Mozilla Firefox 140.9.1\n",
			version: "140.9.1esr",
			want:    "140.9.1",
		},
		{
			name:    "a different version fails",
			onPath:  []string{"firefox"},
			banner:  "Mozilla Firefox 134.0.2\n",
			version: "149.0.2",
			wantErr: true,
		},
		{
			name:    "nothing on PATH fails",
			version: "149.0.2",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, _ := testInstaller(t, installerOpts{
				distro:  DistroDebian,
				onPath:  tc.onPath,
				stdouts: map[string]string{"--version": tc.banner},
			})

			got, err := i.verifyFirefox(context.Background(), tc.version)
			if tc.wantErr {
				assert.Assert(t, err != nil, "got %q", got)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestInstallFirefoxRejectsUnresolvableVersion(t *testing.T) {
	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})

	err := i.InstallFirefox(context.Background(), FirefoxOptions{Version: "$FF_NOT_SET", InstallDir: "/usr/local/bin"})
	assert.Assert(t, err != nil)
	assert.Assert(t, strings.Contains(err.Error(), "FF_NOT_SET"))
}

func TestInstallFirefoxDependenciesAddsGPGWhenMissing(t *testing.T) {
	tests := []struct {
		name    string
		distro  Distro
		onPath  []string
		command string
		wantGPG string
	}{
		{name: "Debian without gpg", distro: DistroDebian, command: "apt-get", wantGPG: "gnupg"},
		{name: "Debian with gpg", distro: DistroDebian, onPath: []string{"gpg"}, command: "apt-get"},
		{name: "RHEL without gpg", distro: DistroRHEL, command: "yum", wantGPG: "gnupg2"},
		{name: "RHEL with gpg", distro: DistroRHEL, onPath: []string{"gpg"}, command: "yum"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, calls := testInstaller(t, installerOpts{distro: tc.distro, onPath: tc.onPath})

			assert.NilError(t, i.installFirefoxDependencies(context.Background()))

			var install []string
			for _, c := range *calls {
				if c.name == tc.command && len(c.args) > 0 && c.args[0] == "install" {
					install = c.args
				}
			}
			assert.Assert(t, install != nil, "no %s install: %+v", tc.command, *calls)
			hasGnupg := false
			for _, arg := range install {
				if strings.HasPrefix(arg, "gnupg") {
					assert.Equal(t, arg, tc.wantGPG)
					hasGnupg = true
				}
			}
			assert.Equal(t, hasGnupg, tc.wantGPG != "", "install args: %v", install)
		})
	}
}

const cachedFirefoxVersion = "149.0.2"

// fakeMozilla stands in for archive.mozilla.org serving one Linux release,
// signed over SHA256SUMS only, and records every request it gets.
type fakeMozilla struct {
	srv     *httptest.Server
	archive []byte

	mu       sync.Mutex
	requests []string
}

func newFakeMozilla(t *testing.T) *fakeMozilla {
	t.Helper()

	// A real archive, since the install extracts whatever it is given.
	archive, err := os.ReadFile(filepath.Join("testdata", "archive.tar.xz"))
	assert.NilError(t, err)

	m := &fakeMozilla{archive: archive}
	base := "/" + cachedFirefoxVersion
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.requests = append(m.requests, r.URL.Path)
		m.mu.Unlock()

		switch r.URL.Path {
		case "/firefox_versions.json":
			fmt.Fprintf(w, `{"LATEST_FIREFOX_VERSION":%q}`, cachedFirefoxVersion)
		case base + "/linux-x86_64/en-US/firefox-" + cachedFirefoxVersion + ".tar.xz":
			_, _ = w.Write(archive)
		case base + "/KEY":
			fmt.Fprint(w, "mozilla release key")
		case base + "/SHA256SUMS":
			fmt.Fprint(w, firefoxSums(archive))
		case base + "/SHA256SUMS.asc":
			fmt.Fprint(w, "signature")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(m.srv.Close)
	return m
}

func firefoxSums(archive []byte) string {
	return fmt.Sprintf("%x  linux-x86_64/en-US/firefox-%s.tar.xz\n", sha256.Sum256(archive), cachedFirefoxVersion)
}

// takeRequests returns the requests made since it was last called.
func (m *fakeMozilla) takeRequests() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	got := m.requests
	m.requests = nil
	return got
}

// firefoxInstaller returns an installer for one pipeline on a fresh Debian
// host, downloading from m and caching through client.
func firefoxInstaller(t *testing.T, m *fakeMozilla, client *sdk.Client) (*Installer, *[]invocation) {
	t.Helper()

	i, calls := testInstaller(t, installerOpts{
		distro:  DistroDebian,
		stdouts: map[string]string{"firefox --version": "Mozilla Firefox " + cachedFirefoxVersion + "\n"},
	})
	i.Endpoints.MozillaArchive = m.srv.URL
	i.Endpoints.MozillaProduct = m.srv.URL
	i.Fetcher = Fetcher{HTTPClient: m.srv.Client()}
	i.Cache = Cache{Client: client, Out: io.Discard}
	// firefox is on PATH only once the install has linked it, so each pipeline
	// starts without one, as a fresh job does.
	i.Host.LookPath = func(name string) (string, error) {
		if name == "gpg" {
			return "/usr/bin/gpg", nil
		}
		if name == "firefox" {
			for _, c := range *calls {
				if c.name == "ln" {
					return "/usr/local/bin/firefox", nil
				}
			}
		}
		return "", fmt.Errorf("%s: not found", name)
	}
	return i, calls
}

// cachedFirefoxSetup points the user's cache directory at a temp dir and
// returns the options for a pinned, cached install along with the cache key and
// directory it uses.
func cachedFirefoxSetup(t *testing.T) (opts FirefoxOptions, key, dir string) {
	t.Helper()

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	opts = FirefoxOptions{Version: cachedFirefoxVersion, InstallDir: usrLocalBin, UseCache: true, CacheKey: "v1"}
	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})
	key = i.firefoxCacheKey(cachedFirefoxVersion, opts)
	dir, err := cacheDir(key)
	assert.NilError(t, err)
	return opts, key, dir
}

func gpgVerifications(calls []invocation) [][]string {
	var got [][]string
	for _, c := range calls {
		if c.name == "gpg" && len(c.args) > 0 && c.args[0] == "--verify" {
			got = append(got, c.args)
		}
	}
	return got
}

func TestInstallFirefoxFromCacheNeedsNoMozilla(t *testing.T) {
	opts, key, dir := cachedFirefoxSetup(t)
	m := newFakeMozilla(t)
	// The fake job cache restores an entry to the absolute path it was saved
	// from, as CircleCI's does, which is what a download into a temp dir never
	// hit again.
	srv := sdktest.NewServer(t)
	ctx := context.Background()

	i, _ := firefoxInstaller(t, m, srv.Client())
	assert.NilError(t, i.InstallFirefox(ctx, opts))
	base := "/" + cachedFirefoxVersion
	assert.DeepEqual(t, m.takeRequests(), []string{
		base + "/linux-x86_64/en-US/firefox-" + cachedFirefoxVersion + ".tar.xz",
		base + "/KEY",
		base + "/SHA256SUMS",
		base + "/SHA256SUMS.asc",
	})
	assert.DeepEqual(t, srv.SavedKeys(), []string{key})

	// The next pipeline runs on a fresh machine, with nothing left locally.
	assert.NilError(t, os.RemoveAll(dir))

	i, calls := firefoxInstaller(t, m, srv.Client())
	assert.NilError(t, i.InstallFirefox(ctx, opts))
	assert.DeepEqual(t, m.takeRequests(), []string(nil))
	// A hit is still verified, against the key, manifest and signature cached
	// with the archive.
	assert.DeepEqual(t, gpgVerifications(*calls), [][]string{
		{"--verify", filepath.Join(dir, "SHA256SUMS.asc"), filepath.Join(dir, "SHA256SUMS")},
	})
}

func TestInstallFirefoxDownloadsAgainOverABadCacheEntry(t *testing.T) {
	archiveName := "firefox-" + cachedFirefoxVersion + ".tar.xz"
	tests := []struct {
		name string
		// files is what the cache entry holds, by file name.
		files func(archive []byte) map[string]string
		// rejectSignature makes gpg reject the cached signature.
		rejectSignature bool
	}{
		{
			name: "the archive does not match the manifest",
			files: func(archive []byte) map[string]string {
				return map[string]string{
					archiveName:      "truncated",
					"mozilla.key":    "mozilla release key",
					"SHA256SUMS":     firefoxSums(archive),
					"SHA256SUMS.asc": "signature",
				}
			},
		},
		{
			name: "the signature does not verify",
			files: func(archive []byte) map[string]string {
				return map[string]string{
					archiveName:      string(archive),
					"mozilla.key":    "mozilla release key",
					"SHA256SUMS":     firefoxSums(archive),
					"SHA256SUMS.asc": "forged",
				}
			},
			rejectSignature: true,
		},
		{
			name: "the signature is missing",
			files: func(archive []byte) map[string]string {
				return map[string]string{
					archiveName:   string(archive),
					"mozilla.key": "mozilla release key",
					"SHA256SUMS":  firefoxSums(archive),
				}
			},
		},
		{
			// What an entry saved before the key and manifest were cached holds.
			name: "only the archive is cached",
			files: func(archive []byte) map[string]string {
				return map[string]string{archiveName: string(archive)}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts, key, dir := cachedFirefoxSetup(t)
			m := newFakeMozilla(t)
			srv := sdktest.NewServer(t)
			ctx := context.Background()

			assert.NilError(t, os.MkdirAll(dir, 0o750))
			for name, content := range tc.files(m.archive) {
				assert.NilError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
			}
			assert.NilError(t, srv.Client().SaveCache(ctx, key, []string{dir}))
			assert.NilError(t, os.RemoveAll(dir))

			i, calls := firefoxInstaller(t, m, srv.Client())
			if tc.rejectSignature {
				scripted := i.Runner.Executor
				i.Runner.Executor = func(ctx context.Context, name string, args, env []string, out io.Writer) (string, error) {
					stdout, err := scripted(ctx, name, args, env, out)
					if len(gpgVerifications(*calls)) == 1 && name == "gpg" && args[0] == "--verify" {
						return "", errors.New("BAD signature")
					}
					return stdout, err
				}
			}

			assert.NilError(t, i.InstallFirefox(ctx, opts))
			assert.Equal(t, len(m.takeRequests()), 4)
			// The download replaced the bad entry rather than joining it.
			got, err := os.ReadFile(filepath.Join(dir, archiveName))
			assert.NilError(t, err)
			assert.DeepEqual(t, got, m.archive)
			sig, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS.asc"))
			assert.NilError(t, err)
			assert.Equal(t, string(sig), "signature")
		})
	}
}

func TestInstallFirefoxWithoutCacheLeavesNothingBehind(t *testing.T) {
	tests := []struct {
		name string
		opts FirefoxOptions
	}{
		{name: "caching off", opts: FirefoxOptions{Version: cachedFirefoxVersion, CacheKey: "v1"}},
		{name: "latest with caching on", opts: FirefoxOptions{Version: Latest, UseCache: true, CacheKey: "v1"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			m := newFakeMozilla(t)
			srv := sdktest.NewServer(t)

			i, _ := firefoxInstaller(t, m, srv.Client())
			tc.opts.InstallDir = usrLocalBin
			assert.NilError(t, i.InstallFirefox(context.Background(), tc.opts))

			assert.Assert(t, len(m.takeRequests()) >= 4)
			assert.DeepEqual(t, srv.SavedKeys(), []string{})
			base, err := os.UserCacheDir()
			assert.NilError(t, err)
			_, err = os.Stat(filepath.Join(base, cacheKeyPrefix))
			assert.Assert(t, errors.Is(err, os.ErrNotExist), "cache directory created: %v", err)
		})
	}
}
