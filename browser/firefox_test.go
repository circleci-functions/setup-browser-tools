package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

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

func TestFirefoxReleaseFilenameMatchesManifestBaseName(t *testing.T) {
	// The archive is saved under its own name so the checksum manifest, which
	// lists "linux-x86_64/en-US/firefox-149.0.2.tar.xz", matches on base name.
	// The orb saved it flattened and had to rewrite SHA256SUMS with perl.
	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})

	rel, err := i.firefoxRelease("149.0.2")
	assert.NilError(t, err)
	assert.Equal(t, rel.Filename, filepath.Base("linux-x86_64/en-US/firefox-149.0.2.tar.xz"))
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
			want: "setup-browser-tools-firefox-v1-149.0.2-linux-amd64",
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
