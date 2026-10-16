package browser

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

func unprivilegedInstaller(t *testing.T, opts installerOpts, writable ...string) (*Installer, *[]invocation) {
	t.Helper()
	i, calls := testInstaller(t, opts)
	i.Runner.Priv = Privilege{}
	i.Host.Writable = func(dir string) bool {
		for _, w := range writable {
			if dir == w {
				return true
			}
		}
		return false
	}
	return i, calls
}

func TestInstallsNeedingPrivilegeFailBeforeDownloading(t *testing.T) {
	tests := []struct {
		name    string
		distro  Distro
		install func(*Installer) error
		wantErr string
	}{
		{
			name:   "Chrome on Debian",
			distro: DistroDebian,
			install: func(i *Installer) error {
				return i.InstallChrome(context.Background(), ChromeOptions{Version: "147.0.7727.55", Channel: ChannelStable})
			},
			wantErr: "Chrome needs root or sudo to run apt-get, and this job is not root and has no sudo",
		},
		{
			name:   "Chrome on RHEL",
			distro: DistroRHEL,
			install: func(i *Installer) error {
				return i.InstallChrome(context.Background(), ChromeOptions{Version: "147.0.7727.55", Channel: ChannelStable})
			},
			wantErr: "Chrome needs root or sudo to run yum",
		},
		{
			name:   "Chrome on macOS",
			distro: DistroMacOS,
			install: func(i *Installer) error {
				return i.InstallChrome(context.Background(), ChromeOptions{Version: "147.0.7727.55", Channel: ChannelStable})
			},
			wantErr: "Chrome needs root or sudo to run /usr/sbin/installer",
		},
		{
			name:    "ChromeDriver on Alpine",
			distro:  DistroAlpine,
			install: func(i *Installer) error { return i.InstallChromeDriver(context.Background(), ChromeDriverOptions{}) },
			wantErr: "ChromeDriver needs root or sudo to run apk",
		},
		{
			name:   "Firefox on Debian",
			distro: DistroDebian,
			install: func(i *Installer) error {
				return i.InstallFirefox(context.Background(), FirefoxOptions{Version: "149.0.2", InstallDir: "/tmp"})
			},
			wantErr: "Firefox needs root or sudo to run apt-get",
		},
		{
			name:   "Firefox on macOS writes to /Applications",
			distro: DistroMacOS,
			install: func(i *Installer) error {
				return i.InstallFirefox(context.Background(), FirefoxOptions{Version: "149.0.2", InstallDir: "/tmp"})
			},
			wantErr: "Firefox needs root or sudo to write to /Applications",
		},
		{
			name:    "Edge on Debian",
			distro:  DistroDebian,
			install: func(i *Installer) error { return i.InstallEdge(context.Background(), EdgeOptions{Version: Latest}) },
			wantErr: "Microsoft Edge needs root or sudo to run apt-get",
		},
		{
			name:   "Chrome for Testing on Debian",
			distro: DistroDebian,
			install: func(i *Installer) error {
				return i.InstallChromeForTesting(context.Background(), ChromeForTestingOptions{Version: "147.0.7727.55", InstallDir: "/tmp"})
			},
			wantErr: "Chrome for Testing needs root or sudo to run apt-get",
		},
		{
			name:   "geckodriver into a directory the job cannot write",
			distro: DistroDebian,
			install: func(i *Installer) error {
				return i.InstallGeckodriver(context.Background(), GeckodriverOptions{Version: "v0.36.0", InstallDir: "/usr/bin"})
			},
			wantErr: "geckodriver needs root or sudo to write to /usr/bin",
		},
		{
			name:   "Edge WebDriver into a directory the job cannot write",
			distro: DistroDebian,
			install: func(i *Installer) error {
				return i.InstallEdgeDriver(context.Background(), EdgeDriverOptions{InstallDir: "/usr/bin"})
			},
			wantErr: "Edge WebDriver needs root or sudo to write to /usr/bin",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, calls := unprivilegedInstaller(t, installerOpts{distro: tc.distro, endpoint: "http://127.0.0.1:0"}, "/tmp")
			i.Endpoints.GeckodriverGitHub = "http://127.0.0.1:0"
			i.Endpoints.MozillaArchive = "http://127.0.0.1:0"
			i.Endpoints.ChromeForTestingCDN = "http://127.0.0.1:0"

			assert.ErrorContains(t, tc.install(i), tc.wantErr)
			for _, c := range *calls {
				assert.Assert(t, c.name != "apt-get" && c.name != "yum" && c.name != "apk" && c.name != "cp",
					"a privileged step ran before the check: %+v", *calls)
			}
		})
	}
}

func TestRequireWritable(t *testing.T) {
	t.Run("a writable directory needs no privilege", func(t *testing.T) {
		i, _ := unprivilegedInstaller(t, installerOpts{distro: DistroDebian}, "/home/circleci/bin")
		assert.NilError(t, i.requireWritable("geckodriver", "/home/circleci/bin"))
	})

	t.Run("every directory has to be writable", func(t *testing.T) {
		i, _ := unprivilegedInstaller(t, installerOpts{distro: DistroMacOS}, "/home/circleci/bin")
		assert.ErrorContains(t, i.requireWritable("Firefox", "/home/circleci/bin", "/usr/local/bin"),
			"Firefox needs root or sudo to write to /usr/local/bin")
	})

	t.Run("root or sudo makes any directory acceptable", func(t *testing.T) {
		i, _ := testInstaller(t, installerOpts{distro: DistroDebian})
		i.Host.Writable = func(string) bool { return false }
		assert.NilError(t, i.requireWritable("geckodriver", "/usr/bin"))

		i.Runner.Priv = sudoPrivilege()
		assert.NilError(t, i.requireWritable("geckodriver", "/usr/bin"))
		assert.NilError(t, i.requirePrivilege("Chrome", "run apt-get"))
	})
}

func TestInstalledChromeNeedsNoPrivilege(t *testing.T) {
	i, _ := unprivilegedInstaller(t, installerOpts{
		distro:  DistroDebian,
		onPath:  []string{"google-chrome-stable"},
		stdouts: map[string]string{"--version": "Google Chrome 147.0.7727.55 \n"},
	})

	assert.NilError(t, i.InstallChrome(context.Background(), ChromeOptions{Version: "147.0.7727.55", Channel: ChannelStable}))
}

func TestHostWritable(t *testing.T) {
	dir := t.TempDir()
	assert.Assert(t, Host{}.writable(dir))

	entries, err := os.ReadDir(dir)
	assert.NilError(t, err)
	assert.Equal(t, len(entries), 0, "the probe file was left behind")

	assert.Assert(t, !Host{}.writable(filepath.Join(dir, "missing")))
}
