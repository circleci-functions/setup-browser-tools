package browser

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// edgeMacBinary is where the cask puts Edge; there is no wrapper on PATH, so the
// bundle is asked for its version directly.
const edgeMacBinary = "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"

type EdgeOptions struct {
	Version string
}

func (i *Installer) InstallEdge(ctx context.Context, opts EdgeOptions) error {
	if err := i.edgeSupported("Edge"); err != nil {
		return err
	}

	version, err := ExpandVersion(opts.Version, "--version", nil)
	if err != nil {
		return err
	}

	if i.Platform.IsMacOS() {
		if err := i.installEdgeMacOS(ctx); err != nil {
			return err
		}
	} else if err := i.installEdgeDebian(ctx, version); err != nil {
		return err
	}

	installed, found := i.installedEdgeVersion(ctx)
	if !found {
		return fmt.Errorf("no working Microsoft Edge after installing %s", version)
	}
	i.logf("installed Microsoft Edge %s", installed)
	RecordVersion(i.Out, "Microsoft Edge", installed)
	return nil
}

// edgeSupported reports the platforms Microsoft publishes Edge for. The orb's
// if/elif had no else, so Alpine and RHEL exited 0 having installed nothing.
func (i *Installer) edgeSupported(component string) error {
	switch {
	case i.Platform.IsMacOS():
		return nil
	case i.Platform.Distro == DistroDebian && i.Platform.Arch == ArchAMD64:
		return nil
	case i.Platform.IsLinuxARM64():
		return Unsupported(component, i.Platform, "Microsoft publishes no Linux ARM64 build")
	default:
		return Unsupported(component, i.Platform, "builds are published for macOS and Debian/Ubuntu amd64 only")
	}
}

func (i *Installer) installEdgeMacOS(ctx context.Context) error {
	if !i.Host.has("brew") {
		return fmt.Errorf("brew is required to install Edge on macOS but is not installed")
	}
	_, err := i.Runner.Run(ctx, Command{
		Name: "brew",
		Args: []string{"install", "--cask", "microsoft-edge"},
		Env:  []string{"HOMEBREW_NO_AUTO_UPDATE=1"},
	})
	return err
}

// edgeKeyringPath keeps Microsoft's signing key armored. apt reads an .asc
// keyring directly, so no gpg --dearmor is needed and Edge installs on images
// without gpg.
const edgeKeyringPath = "/usr/share/keyrings/microsoft-archive-keyring.asc"

// installEdgeDebian adds Microsoft's signed apt repository and installs from it.
// Every privileged step goes through the resolved prefix; the orb used a literal
// sudo here, which breaks in a root container with no sudo installed.
func (i *Installer) installEdgeDebian(ctx context.Context, version string) error {
	dir, cleanup, err := TempDir("edge")
	if err != nil {
		return err
	}
	defer cleanup()

	key, err := i.Fetcher.Download(ctx, i.Endpoints.MicrosoftPackages+"/keys/microsoft.asc", dir, "microsoft.asc")
	if err != nil {
		return err
	}

	if err := i.Runner.Exec(ctx, "cp", key, edgeKeyringPath); err != nil {
		return err
	}
	// apt runs as the _apt user and silently ignores a keyring it cannot read.
	if err := i.Runner.Exec(ctx, "chmod", "0644", edgeKeyringPath); err != nil {
		return err
	}

	list := filepath.Join(dir, "microsoft-edge.list")
	entry := fmt.Sprintf("deb [arch=amd64 signed-by=%s] %s/repos/edge stable main\n",
		edgeKeyringPath, i.Endpoints.MicrosoftPackages)
	if err := writeFile(list, strings.NewReader(entry), 0o644); err != nil {
		return err
	}
	const listPath = "/etc/apt/sources.list.d/microsoft-edge-stable.list"
	if err := i.Runner.Exec(ctx, "cp", list, listPath); err != nil {
		return err
	}
	if err := i.Runner.Exec(ctx, "chmod", "0644", listPath); err != nil {
		return err
	}

	if err := i.Runner.AptUpdate(ctx); err != nil {
		return err
	}
	return i.aptInstall(ctx, edgePackageSpec(version))
}

// edgePackageSpec pins the apt version when one was asked for, matching the
// orb's "=<version>-1" suffix.
func edgePackageSpec(version string) string {
	if version == Latest {
		return "microsoft-edge-stable"
	}
	return "microsoft-edge-stable=" + version + "-1"
}

func (i *Installer) installedEdgeVersion(ctx context.Context) (string, bool) {
	candidates := []string{"microsoft-edge", "microsoft-edge-stable"}
	for _, bin := range candidates {
		if !i.Host.has(bin) {
			continue
		}
		if banner, err := i.Runner.Output(ctx, bin, "--version"); err == nil {
			if v := ParseBrowserVersion(banner); v != "" {
				return v, true
			}
		}
	}

	if i.Platform.IsMacOS() {
		if banner, err := i.Runner.Output(ctx, edgeMacBinary, "--version"); err == nil {
			if v := ParseBrowserVersion(banner); v != "" {
				return v, true
			}
		}
	}
	return "", false
}

type EdgeDriverOptions struct {
	InstallDir string
}

func (i *Installer) InstallEdgeDriver(ctx context.Context, opts EdgeDriverOptions) error {
	if err := i.edgeSupported("Edge WebDriver"); err != nil {
		return err
	}

	version, found := i.installedEdgeVersion(ctx)
	if !found {
		return fmt.Errorf("Microsoft Edge is not installed; Edge WebDriver has to match its version") //nolint:staticcheck // proper noun
	}
	i.logf("installed Edge is %s", version)

	url := i.edgeDriverURL(version)
	if err := i.downloadAndInstallBinary(ctx, url, "msedgedriver", opts.InstallDir, "edgedriver.zip"); err != nil {
		return err
	}

	installed, found := i.installedDriverVersion(ctx, "msedgedriver")
	if !found {
		return fmt.Errorf("no working msedgedriver on PATH after installing %s", version)
	}
	if installed != version {
		return fmt.Errorf("installed Edge WebDriver is %s, but Edge is %s", installed, version)
	}
	i.logf("installed Edge WebDriver %s", installed)
	RecordVersion(i.Out, "Edge WebDriver", installed)
	return nil
}

func (i *Installer) edgeDriverPlatform() string {
	if i.Platform.IsMacOS() {
		return "mac64_m1"
	}
	return "linux64"
}

func (i *Installer) edgeDriverURL(version string) string {
	return fmt.Sprintf("%s/%s/edgedriver_%s.zip", i.Endpoints.MSEdgeDriver, version, i.edgeDriverPlatform())
}
