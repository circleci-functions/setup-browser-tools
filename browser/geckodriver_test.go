package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
