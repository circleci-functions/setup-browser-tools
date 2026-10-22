package browser

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

type GeckodriverOptions struct {
	Version    string
	InstallDir string
}

func (i *Installer) InstallGeckodriver(ctx context.Context, opts GeckodriverOptions) error {
	requested, err := ExpandVersion(opts.Version, "--version", nil)
	if err != nil {
		return err
	}

	tag, err := i.resolveGeckodriverTag(ctx, requested)
	if err != nil {
		return err
	}
	i.logf("selected geckodriver %s", tag)

	// Releases are tagged v0.36.0 but the binary reports 0.36.0.
	wanted := strings.TrimPrefix(tag, "v")

	if installed, found := i.installedDriverVersion(ctx, "geckodriver"); found {
		if installed == wanted {
			i.logf("geckodriver %s is already installed", installed)
			return nil
		}
		i.logf("geckodriver %s is installed; replacing it with %s", installed, wanted)
		_ = i.Runner.Exec(ctx, "rm", "-f", filepath.Join(opts.InstallDir, "geckodriver"))
	}

	if err := i.requireWritable("geckodriver", opts.InstallDir); err != nil {
		return err
	}

	url := i.geckodriverURL(tag)
	if err := i.downloadAndInstallBinary(ctx, url, "geckodriver", opts.InstallDir, "geckodriver.tar.gz"); err != nil {
		return err
	}

	installed, found := i.installedDriverVersion(ctx, "geckodriver")
	if !found {
		return fmt.Errorf("no working geckodriver on PATH after installing %s", tag)
	}
	if installed != wanted {
		return fmt.Errorf("installed geckodriver is %s, but %s was requested", installed, wanted)
	}
	i.logf("installed geckodriver %s", installed)
	RecordVersion(i.Out, "geckodriver", installed)
	return nil
}

// resolveGeckodriverTag turns "latest" into a concrete tag by following the
// releases/latest redirect and taking the final path segment, which is what the
// orb does. Any other value is used as the tag directly.
func (i *Installer) resolveGeckodriverTag(ctx context.Context, requested string) (string, error) {
	if requested != Latest {
		return requested, nil
	}

	final, err := i.Fetcher.FinalURL(ctx, i.Endpoints.GeckodriverGitHub+"/releases/latest")
	if err != nil {
		return "", err
	}
	tag, err := LastPathSegment(final)
	if err != nil {
		return "", fmt.Errorf("cannot read a geckodriver tag from %s: %w", final, err)
	}
	return tag, nil
}

// geckodriverPlatform picks the release asset for the host. Unlike the orb,
// Intel Macs get the x86-64 macos build: up to v0.36.0 macos-aarch64 is
// arm64-only.
func (i *Installer) geckodriverPlatform() string {
	if i.Platform.IsMacOS() {
		if i.Platform.Arch == ArchAMD64 {
			return "macos"
		}
		return "macos-aarch64"
	}
	if i.Platform.Arch == ArchARM64 {
		return "linux-aarch64"
	}
	return "linux64"
}

func (i *Installer) geckodriverURL(tag string) string {
	return fmt.Sprintf("%s/releases/download/%s/geckodriver-%s-%s.tar.gz",
		i.Endpoints.GeckodriverGitHub, tag, tag, i.geckodriverPlatform())
}
