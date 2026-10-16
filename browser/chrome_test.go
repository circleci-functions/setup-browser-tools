package browser

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

// scriptedExec returns an Executor whose stdout is chosen by matching the
// command line against stdouts, and which records every invocation.
func scriptedExec(calls *[]invocation, stdouts map[string]string) Executor {
	return func(_ context.Context, name string, args, env []string, out io.Writer) (string, error) {
		*calls = append(*calls, invocation{name: name, args: args, env: env})

		line := strings.TrimSpace(name + " " + strings.Join(args, " "))
		for match, stdout := range stdouts {
			if strings.Contains(line, match) {
				fmt.Fprint(out, stdout)
				return stdout, nil
			}
		}
		return "", nil
	}
}

type installerOpts struct {
	distro   Distro
	arch     string
	onPath   []string
	stdouts  map[string]string
	endpoint string
}

func testInstaller(t *testing.T, opts installerOpts) (*Installer, *[]invocation) {
	t.Helper()

	if opts.arch == "" {
		opts.arch = ArchAMD64
	}
	goos := "linux"
	if opts.distro == DistroMacOS {
		goos = "darwin"
	}

	host := Host{
		GOOS:     goos,
		GOARCH:   opts.arch,
		LookPath: fakePath(opts.onPath...),
		Geteuid:  func() int { return 0 },
	}

	calls := &[]invocation{}
	endpoints := DefaultEndpoints()
	if opts.endpoint != "" {
		endpoints = Endpoints{
			ChromiumDash:    opts.endpoint,
			GoogleLinux:     opts.endpoint,
			GoogleChromeMac: opts.endpoint,
		}
	}

	return &Installer{
		Host:      host,
		Platform:  Platform{Distro: opts.distro, Arch: opts.arch},
		Runner:    &Runner{Priv: rootPrivilege(), Out: io.Discard, Sleep: func(time.Duration) {}, Executor: scriptedExec(calls, opts.stdouts)},
		Fetcher:   Fetcher{},
		Cache:     Cache{Out: io.Discard},
		Endpoints: endpoints,
		Out:       io.Discard,
	}, calls
}

func TestChromeURLs(t *testing.T) {
	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})

	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "chromiumdash on Linux stable",
			got:  i.chromiumDashURL(ChannelStable),
			want: "https://chromiumdash.appspot.com/fetch_releases?channel=Stable&platform=Linux",
		},
		{
			name: "chromiumdash on Linux beta",
			got:  i.chromiumDashURL(ChannelBeta),
			want: "https://chromiumdash.appspot.com/fetch_releases?channel=Beta&platform=Linux",
		},
		{
			name: "pinned deb comes from the resolved version",
			got:  i.chromeDebURL("147.0.7727.55", ChannelStable),
			want: "https://dl.google.com/linux/chrome/deb/pool/main/g/google-chrome-stable/google-chrome-stable_147.0.7727.55-1_amd64.deb",
		},
		{
			name: "pinned deb on beta",
			got:  i.chromeDebURL("147.0.7727.55", ChannelBeta),
			want: "https://dl.google.com/linux/chrome/deb/pool/main/g/google-chrome-beta/google-chrome-beta_147.0.7727.55-1_amd64.deb",
		},
		{
			name: "latest rpm uses the current-release path",
			got:  i.chromeRPMURL("147.0.7727.55", Latest, ChannelStable),
			want: "https://dl.google.com/linux/direct/google-chrome-stable_current_x86_64.rpm",
		},
		{
			name: "pinned rpm uses the versioned path",
			got:  i.chromeRPMURL("147.0.7727.55", "147.0.7727.55", ChannelStable),
			want: "https://dl.google.com/linux/chrome/rpm/stable/x86_64/google-chrome-stable-147.0.7727.55-1.x86_64.rpm",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.got, tc.want)
		})
	}
}

func TestChromeMacURLAndDashPlatform(t *testing.T) {
	i, _ := testInstaller(t, installerOpts{distro: DistroMacOS, arch: ArchARM64})

	assert.Equal(t, i.chromiumDashURL(ChannelStable),
		"https://chromiumdash.appspot.com/fetch_releases?channel=Stable&platform=Mac")

	url := i.chromeMacURL(ChannelBeta)
	assert.Assert(t, strings.HasPrefix(url, "https://dl.google.com/chrome/mac/beta/"), "got %q", url)
	assert.Assert(t, strings.HasSuffix(url, "/googlechrome.pkg"), "got %q", url)
	// Without the accept_tos segment the package prompts for a licence.
	assert.Assert(t, strings.Contains(url, "accept_tos"), "got %q", url)
}

func TestResolveChromeVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("channel") {
		case "Stable":
			fmt.Fprint(w, `[{"version":"147.0.7727.55"},{"version":"147.0.7727.40"}]`)
		case "Beta":
			fmt.Fprint(w, `[{"version":"148.0.7800.10"}]`)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	defer srv.Close()

	i, _ := testInstaller(t, installerOpts{distro: DistroDebian, endpoint: srv.URL})
	i.Fetcher = Fetcher{HTTPClient: srv.Client()}

	tests := []struct {
		name      string
		requested string
		channel   string
		want      string
		wantErr   bool
	}{
		{name: "latest stable takes the first release", requested: Latest, channel: ChannelStable, want: "147.0.7727.55"},
		{name: "latest beta", requested: Latest, channel: ChannelBeta, want: "148.0.7800.10"},
		{name: "a pinned version is passed through untouched", requested: "140.0.1.2", channel: ChannelStable, want: "140.0.1.2"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := i.resolveChromeVersion(context.Background(), tc.requested, tc.channel)
			if tc.wantErr {
				assert.Assert(t, err != nil)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestResolveChromeVersionWithNoReleases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	i, _ := testInstaller(t, installerOpts{distro: DistroDebian, endpoint: srv.URL})
	i.Fetcher = Fetcher{HTTPClient: srv.Client()}

	_, err := i.resolveChromeVersion(context.Background(), Latest, ChannelStable)
	assert.Assert(t, err != nil)
	assert.Assert(t, strings.Contains(err.Error(), "no Chrome releases"))
}

func TestChromeAlreadyInstalled(t *testing.T) {
	tests := []struct {
		name            string
		distro          Distro
		onPath          []string
		banner          string
		target          string
		replaceExisting bool
		wantSkip        bool
	}{
		{
			name:     "not installed proceeds",
			distro:   DistroDebian,
			target:   "147.0.7727.55",
			wantSkip: false,
		},
		{
			name:     "same version without --replace-existing skips",
			distro:   DistroDebian,
			onPath:   []string{"google-chrome-stable"},
			banner:   "Google Chrome 147.0.7727.55 \n",
			target:   "147.0.7727.55",
			wantSkip: true,
		},
		{
			name:     "different version without --replace-existing still skips",
			distro:   DistroDebian,
			onPath:   []string{"google-chrome-stable"},
			banner:   "Google Chrome 140.0.1.2 \n",
			target:   "147.0.7727.55",
			wantSkip: true,
		},
		{
			name:            "different version with --replace-existing reinstalls",
			distro:          DistroDebian,
			onPath:          []string{"google-chrome-stable"},
			banner:          "Google Chrome 140.0.1.2 \n",
			target:          "147.0.7727.55",
			replaceExisting: true,
			wantSkip:        false,
		},
		{
			name:            "same version with --replace-existing skips rather than reinstalling",
			distro:          DistroDebian,
			onPath:          []string{"google-chrome-stable"},
			banner:          "Google Chrome 147.0.7727.55 \n",
			target:          "147.0.7727.55",
			replaceExisting: true,
			wantSkip:        true,
		},
		{
			name:            "the same holds on macOS, where the orb always reinstalled",
			distro:          DistroMacOS,
			onPath:          []string{"google-chrome-stable"},
			banner:          "Google Chrome 147.0.7727.55 \n",
			target:          "147.0.7727.55",
			replaceExisting: true,
			wantSkip:        true,
		},
		{
			name:            "Alpine reads chromium-browser",
			distro:          DistroAlpine,
			onPath:          []string{"chromium-browser"},
			banner:          "Chromium 120.0.6099.129 \n",
			target:          "120.0.6099.129",
			replaceExisting: true,
			wantSkip:        true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stdouts := map[string]string{"--version": tc.banner}
			i, _ := testInstaller(t, installerOpts{
				distro:  tc.distro,
				onPath:  tc.onPath,
				stdouts: stdouts,
			})

			skip := i.chromeAlreadyInstalled(context.Background(), tc.target, ChromeOptions{
				Channel:         ChannelStable,
				ReplaceExisting: tc.replaceExisting,
			})
			assert.Equal(t, skip, tc.wantSkip)
		})
	}
}

func TestInstallChromeDebianPinnedUsesResolvedVersion(t *testing.T) {
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		fmt.Fprint(w, "deb-contents")
	}))
	defer srv.Close()

	i, calls := testInstaller(t, installerOpts{distro: DistroDebian, endpoint: srv.URL})
	i.Fetcher = Fetcher{HTTPClient: srv.Client()}

	assert.NilError(t, i.installChromeDebianPackage(context.Background(), "147.0.7727.55", ChannelStable))

	// The orb built this URL from the raw flag rather than the resolved version,
	// so an unexpanded $VAR reached the URL.
	assert.Equal(t, len(requested), 1)
	assert.Assert(t, strings.Contains(requested[0], "google-chrome-stable_147.0.7727.55-1_amd64.deb"),
		"requested %q", requested[0])

	var aptInstall *invocation
	for idx := range *calls {
		c := (*calls)[idx]
		if c.name == "apt-get" && len(c.args) > 0 && c.args[0] == "install" {
			aptInstall = &c
		}
	}
	assert.Assert(t, aptInstall != nil, "apt-get install was never run: %+v", *calls)
	assert.Assert(t, strings.HasSuffix(aptInstall.args[len(aptInstall.args)-1], "chrome.deb"),
		"apt-get should install the downloaded file, got %v", aptInstall.args)
	assert.DeepEqual(t, aptInstall.env, []string{"DEBIAN_FRONTEND=noninteractive"})
}

func TestInstallChromeRejectsLinuxARM64(t *testing.T) {
	i, _ := testInstaller(t, installerOpts{distro: DistroDebian, arch: ArchARM64})

	err := i.InstallChrome(context.Background(), ChromeOptions{Version: Latest, Channel: ChannelStable})
	assert.Assert(t, IsUnsupported(err), "got %v", err)
	assert.Assert(t, strings.Contains(err.Error(), "ARM64"))
}

func TestInstallChromeOnAlpineARM64UsesApk(t *testing.T) {
	i, calls := testInstaller(t, installerOpts{
		distro:  DistroAlpine,
		arch:    ArchARM64,
		stdouts: map[string]string{"--version": "Chromium 136.0.7103.113 Alpine Linux\n"},
	})
	i.Host.LookPath = func(name string) (string, error) {
		for _, c := range *calls {
			if name == "chromium-browser" && c.name == "apk" && len(c.args) > 0 && c.args[0] == "add" {
				return "/usr/bin/chromium-browser", nil
			}
		}
		return "", fmt.Errorf("%s: not found", name)
	}

	assert.NilError(t, i.InstallChrome(context.Background(), ChromeOptions{Version: Latest, Channel: ChannelStable}))

	var added bool
	for _, c := range *calls {
		if c.name == "apk" && strings.Join(c.args, " ") == "add --no-cache chromium" {
			added = true
		}
	}
	assert.Assert(t, added, "apk add chromium was never run: %+v", *calls)
}

func TestInstallChromeRejectsUnresolvableVersion(t *testing.T) {
	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})

	err := i.InstallChrome(context.Background(), ChromeOptions{Version: "$NOT_SET_ANYWHERE", Channel: ChannelStable})
	assert.Assert(t, err != nil)
	assert.Assert(t, strings.Contains(err.Error(), "NOT_SET_ANYWHERE"))
	assert.Assert(t, !IsUnsupported(err))
}

func TestVerifyChrome(t *testing.T) {
	tests := []struct {
		name      string
		banner    string
		onPath    []string
		requested string
		target    string
		want      string
		wantErr   bool
	}{
		{
			name:      "exact match",
			onPath:    []string{"google-chrome-stable"},
			banner:    "Google Chrome 147.0.7727.55 \n",
			requested: "147.0.7727.55",
			target:    "147.0.7727.55",
			want:      "147.0.7727.55",
		},
		{
			name:      "latest accepts whatever the package manager installed",
			onPath:    []string{"google-chrome-stable"},
			banner:    "Google Chrome 136.0.7103.113 \n",
			requested: Latest,
			target:    "151.0.7922.108",
			want:      "136.0.7103.113",
		},
		{
			name:      "packaged build differs in the last component",
			onPath:    []string{"google-chrome-stable"},
			banner:    "Google Chrome 147.0.7727.60 \n",
			requested: "147.0.7727.55",
			target:    "147.0.7727.55",
			want:      "147.0.7727.60",
		},
		{
			name:      "a pinned version that did not install fails",
			onPath:    []string{"google-chrome-stable"},
			banner:    "Google Chrome 140.0.1.2 \n",
			requested: "147.0.7727.55",
			target:    "147.0.7727.55",
			wantErr:   true,
		},
		{
			name:      "nothing on PATH fails even for latest",
			requested: Latest,
			target:    "147.0.7727.55",
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, _ := testInstaller(t, installerOpts{
				distro:  DistroDebian,
				onPath:  tc.onPath,
				stdouts: map[string]string{"--version": tc.banner},
			})

			got, err := i.verifyChrome(context.Background(), tc.requested, tc.target, ChannelStable)
			if tc.wantErr {
				assert.Assert(t, err != nil, "got %q", got)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestParseBrowserVersion(t *testing.T) {
	tests := []struct {
		name   string
		banner string
		want   string
	}{
		{name: "chrome", banner: "Google Chrome 147.0.7727.55 ", want: "147.0.7727.55"},
		{name: "chromium", banner: "Chromium 120.0.6099.129 ", want: "120.0.6099.129"},
		{name: "firefox", banner: "Mozilla Firefox 149.0.2", want: "149.0.2"},
		{name: "firefox esr", banner: "Mozilla Firefox 140.9.1esr", want: "140.9.1esr"},
		{name: "chromedriver", banner: "ChromeDriver 147.0.7727.55 (abc123)", want: "147.0.7727.55"},
		{name: "geckodriver", banner: "geckodriver 0.36.0 ( 2024-01-01)", want: "0.36.0"},
		{name: "edge", banner: "Microsoft Edge 147.0.3065.51 ", want: "147.0.3065.51"},
		{name: "no version present", banner: "command not found", want: ""},
		{name: "empty", banner: "", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, ParseBrowserVersion(tc.banner), tc.want)
		})
	}
}

func TestVersionPrefix(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "147.0.7727.55", want: "147.0.7727"},
		{in: "149.0.2", want: "149.0"},
		{in: "149", want: "149"},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, versionPrefix(tc.in), tc.want)
		})
	}
}

func TestResolveChromeVersionSkipsChromiumDashOnAlpine(t *testing.T) {
	// apk decides the version on Alpine, so there is nothing to resolve against.
	// Any request to chromiumdash here would be both pointless and, since the
	// resolved value was previously used as a verification target, actively wrong.
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		fmt.Fprint(w, `[{"version":"151.0.7922.108"}]`)
	}))
	defer srv.Close()

	i, _ := testInstaller(t, installerOpts{distro: DistroAlpine, endpoint: srv.URL})
	i.Fetcher = Fetcher{HTTPClient: srv.Client()}

	got, err := i.resolveChromeVersion(context.Background(), Latest, ChannelStable)
	assert.NilError(t, err)
	assert.Equal(t, got, Latest)
	assert.Equal(t, hits, 0, "Alpine must not query chromiumdash")
}

func TestInstallChromeDebianRepoMakesTheKeyringReadable(t *testing.T) {
	// apt runs as _apt and silently ignores a keyring it cannot read, which
	// surfaces as NO_PUBKEY. Download writes 0600 and cp preserves it, so the
	// mode has to be widened explicitly.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "-----BEGIN PGP PUBLIC KEY BLOCK-----")
	}))
	defer srv.Close()

	i, calls := testInstaller(t, installerOpts{distro: DistroDebian, endpoint: srv.URL})
	i.Fetcher = Fetcher{HTTPClient: srv.Client()}

	_ = i.installChromeDebianRepo(context.Background(), ChannelStable)

	var chmodded []string
	for _, c := range *calls {
		if c.name == "chmod" && len(c.args) == 2 && c.args[0] == "0644" {
			chmodded = append(chmodded, c.args[1])
		}
	}
	assert.Assert(t, len(chmodded) >= 1, "the keyring was never made readable: %+v", *calls)
	assert.Assert(t, strings.Contains(strings.Join(chmodded, " "), "trusted.gpg.d"),
		"got %v", chmodded)
}

func TestInstallChromeRHELResolvesDepsViaYum(t *testing.T) {
	// The orb downloads liberation-fonts from mirror.centos.org/centos/7, retired
	// with CentOS 7 in 2024 and now a 404, so its RHEL path cannot work at all.
	// yum install on a local file resolves dependencies from the distro's repos.
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		fmt.Fprint(w, "rpm-contents")
	}))
	defer srv.Close()

	i, calls := testInstaller(t, installerOpts{distro: DistroRHEL, endpoint: srv.URL})
	i.Fetcher = Fetcher{HTTPClient: srv.Client()}

	assert.NilError(t, i.installChromeRHEL(context.Background(), "147.0.7727.55", Latest, ChannelStable))

	for _, p := range paths {
		assert.Assert(t, !strings.Contains(p, "liberation-fonts"),
			"the dead CentOS 7 font mirror must not be fetched: %v", paths)
	}

	var yum *invocation
	for idx := range *calls {
		if c := (*calls)[idx]; c.name == "yum" {
			yum = &c
		}
	}
	assert.Assert(t, yum != nil, "yum was never run: %+v", *calls)
	assert.Equal(t, yum.args[0], "install", "localinstall does not resolve dependencies")
}
