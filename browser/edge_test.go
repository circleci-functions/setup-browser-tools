package browser

import (
	"context"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestEdgeSupported(t *testing.T) {
	tests := []struct {
		name       string
		distro     Distro
		arch       string
		wantErr    bool
		wantReason string
	}{
		{name: "macOS", distro: DistroMacOS, arch: ArchARM64},
		{name: "Debian amd64", distro: DistroDebian, arch: ArchAMD64},
		{name: "Debian arm64", distro: DistroDebian, arch: ArchARM64, wantErr: true, wantReason: "ARM64"},
		{name: "Alpine", distro: DistroAlpine, arch: ArchAMD64, wantErr: true, wantReason: "macOS and Debian"},
		{name: "RHEL", distro: DistroRHEL, arch: ArchAMD64, wantErr: true, wantReason: "macOS and Debian"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, _ := testInstaller(t, installerOpts{distro: tc.distro, arch: tc.arch})

			err := i.edgeSupported("Edge")
			if tc.wantErr {
				assert.Assert(t, IsUnsupported(err), "got %v", err)
				assert.Assert(t, strings.Contains(err.Error(), tc.wantReason), "got %v", err)
				return
			}
			assert.NilError(t, err)
		})
	}
}

func TestEdgePackageSpec(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{name: "latest installs the unpinned package", version: Latest, want: "microsoft-edge-stable"},
		{name: "a version pins with the -1 revision suffix", version: "147.0.3065.51", want: "microsoft-edge-stable=147.0.3065.51-1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, edgePackageSpec(tc.version), tc.want)
		})
	}
}

func TestInstallEdgeMacOSNeedsBrew(t *testing.T) {
	i, _ := testInstaller(t, installerOpts{distro: DistroMacOS, arch: ArchARM64})

	err := i.installEdgeMacOS(context.Background())
	assert.Assert(t, err != nil)
	assert.Assert(t, strings.Contains(err.Error(), "brew is required"))
}

func TestInstallEdgeDebianNeverEmitsABareSudo(t *testing.T) {
	// The orb used a literal sudo in this branch rather than $SUDO, which breaks
	// in a root container with no sudo installed.
	i, calls := testInstaller(t, installerOpts{distro: DistroDebian})
	i.Fetcher = Fetcher{}

	// The download will fail with no server, which is fine: the assertion is
	// about what was invoked before that, and nothing may be a bare sudo.
	_ = i.installEdgeDebian(context.Background(), Latest)

	for _, c := range *calls {
		assert.Assert(t, c.name != "sudo", "a sudo invocation was emitted while running as root: %+v", *calls)
	}
}

func TestEdgeDriverURL(t *testing.T) {
	tests := []struct {
		name   string
		distro Distro
		arch   string
		want   string
	}{
		{
			name:   "linux",
			distro: DistroDebian,
			arch:   ArchAMD64,
			want:   "https://msedgedriver.microsoft.com/147.0.3065.51/edgedriver_linux64.zip",
		},
		{
			name:   "macOS",
			distro: DistroMacOS,
			arch:   ArchARM64,
			want:   "https://msedgedriver.microsoft.com/147.0.3065.51/edgedriver_mac64_m1.zip",
		},
		{
			name:   "macOS amd64",
			distro: DistroMacOS,
			arch:   ArchAMD64,
			want:   "https://msedgedriver.microsoft.com/147.0.3065.51/edgedriver_mac64.zip",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, _ := testInstaller(t, installerOpts{distro: tc.distro, arch: tc.arch})
			assert.Equal(t, i.edgeDriverURL("147.0.3065.51"), tc.want)
		})
	}
}

func TestInstallEdgeDriverNeedsEdge(t *testing.T) {
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

			err := i.InstallEdgeDriver(context.Background(), EdgeDriverOptions{InstallDir: "/usr/local/bin"})
			assert.Assert(t, err != nil)
			assert.Assert(t, strings.Contains(err.Error(), "Microsoft Edge is not installed"), "got %v", err)
			assert.Assert(t, !IsUnsupported(err), "a missing Edge is not an unsupported platform")
		})
	}
}

func TestInstallEdgeDriverUnsupportedPlatforms(t *testing.T) {
	for _, distro := range []Distro{DistroAlpine, DistroRHEL} {
		t.Run(string(distro), func(t *testing.T) {
			i, _ := testInstaller(t, installerOpts{distro: distro})

			err := i.InstallEdgeDriver(context.Background(), EdgeDriverOptions{InstallDir: "/usr/local/bin"})
			assert.Assert(t, IsUnsupported(err), "got %v", err)
		})
	}
}

func TestInstalledEdgeVersion(t *testing.T) {
	tests := []struct {
		name      string
		distro    Distro
		onPath    []string
		banner    string
		wantFound bool
		want      string
	}{
		{
			name:      "microsoft-edge on PATH",
			distro:    DistroDebian,
			onPath:    []string{"microsoft-edge"},
			banner:    "Microsoft Edge 147.0.3065.51 \n",
			wantFound: true,
			want:      "147.0.3065.51",
		},
		{
			name:      "microsoft-edge-stable on PATH",
			distro:    DistroDebian,
			onPath:    []string{"microsoft-edge-stable"},
			banner:    "Microsoft Edge 147.0.3065.51 \n",
			wantFound: true,
			want:      "147.0.3065.51",
		},
		{
			name:      "nothing installed",
			distro:    DistroDebian,
			wantFound: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, _ := testInstaller(t, installerOpts{
				distro:  tc.distro,
				onPath:  tc.onPath,
				stdouts: map[string]string{"--version": tc.banner},
			})

			got, found := i.installedEdgeVersion(context.Background())
			assert.Equal(t, found, tc.wantFound)
			if tc.wantFound {
				assert.Equal(t, got, tc.want)
			}
		})
	}
}

func TestInstallEdgeRejectsUnresolvableVersion(t *testing.T) {
	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})

	err := i.InstallEdge(context.Background(), EdgeOptions{Version: "${EDGE_NOT_SET}"})
	assert.Assert(t, err != nil)
	assert.Assert(t, strings.Contains(err.Error(), "EDGE_NOT_SET"))
}
