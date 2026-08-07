// Command setup-browser-tools installs browsers and browser-testing tools into
// a CircleCI job, on macOS, Debian/Ubuntu, Alpine and RHEL/CentOS Linux.
// Coverage varies by browser; see README.md for the matrix.
package main

import (
	"context"
	"os"

	"github.com/alecthomas/kong"

	"github.com/circleci-functions/toolkit/functionyaml"
	"github.com/circleci-functions/toolkit/sdk"

	"github.com/circleci-functions/setup-browser-tools/browser"
)

type cli struct {
	InstallChrome       installChromeCmd       `cmd:"" help:"Install Google Chrome."`
	InstallChromedriver installChromedriverCmd `cmd:"" help:"Install ChromeDriver, matching the installed Chrome."`
	InstallFirefox      installFirefoxCmd      `cmd:"" help:"Install Mozilla Firefox."`
	InstallGeckodriver  installGeckodriverCmd  `cmd:"" help:"Install geckodriver, Mozilla's WebDriver proxy for Firefox."`

	InstallChromeForTesting installChromeForTestingCmd `cmd:"" help:"Install Chrome for Testing, and optionally its matching ChromeDriver."`
	InstallEdge             installEdgeCmd             `cmd:"" help:"Install Microsoft Edge."`
	InstallEdgeDriver       installEdgeDriverCmd       `cmd:"" help:"Install Edge WebDriver, matching the installed Edge."`
}

type installChromeCmd struct {
	Version         string `help:"Version of Chrome to install, e.g. 147.0.7727.55. Defaults to the latest stable release." default:"latest"`
	Channel         string `help:"Release channel to install." enum:"stable,beta" default:"stable"`
	ReplaceExisting bool   `help:"Replace an existing Chrome installation when it is a different version." default:"false"`
	UseCache        bool   `help:"Restore and save Chrome via the job cache. Increases cost and rarely speeds things up; use only when downloads are unreliable." default:"false"`
	CacheKey        string `help:"Cache key prefix, for invalidating cached entries." default:"v1"`
}

func (c *installChromeCmd) Run(ctx context.Context, client *sdk.Client) error {
	installer, err := browser.NewInstaller(client, os.Stderr)
	if err != nil {
		return err
	}
	return installer.InstallChrome(ctx, browser.ChromeOptions{
		Version:         c.Version,
		Channel:         c.Channel,
		ReplaceExisting: c.ReplaceExisting,
		UseCache:        c.UseCache,
		CacheKey:        c.CacheKey,
	})
}

type installChromedriverCmd struct {
	InstallDir string `help:"Directory to install ChromeDriver into. Not honoured on Alpine, where the package manager chooses." default:"/usr/local/bin"`
}

func (c *installChromedriverCmd) Run(ctx context.Context, client *sdk.Client) error {
	installer, err := browser.NewInstaller(client, os.Stderr)
	if err != nil {
		return err
	}
	return installer.InstallChromeDriver(ctx, browser.ChromeDriverOptions{InstallDir: c.InstallDir})
}

type installGeckodriverCmd struct {
	Version    string `help:"Release tag of geckodriver to install, e.g. v0.36.0. Defaults to the latest release." default:"latest"`
	InstallDir string `help:"Directory to install geckodriver into." default:"/usr/local/bin"`
}

func (c *installGeckodriverCmd) Run(ctx context.Context, client *sdk.Client) error {
	installer, err := browser.NewInstaller(client, os.Stderr)
	if err != nil {
		return err
	}
	return installer.InstallGeckodriver(ctx, browser.GeckodriverOptions{
		Version:    c.Version,
		InstallDir: c.InstallDir,
	})
}

type installFirefoxCmd struct {
	Version    string `help:"Version of Firefox to install, e.g. 149.0.2 or 140.9.1esr. Defaults to the latest stable release." default:"latest"`
	InstallDir string `help:"Directory to install Firefox into. A symlink is also created in /usr/local/bin." default:"/usr/local/bin"`
	UseCache   bool   `help:"Restore and save the Firefox installer via the job cache. Ignored when --version is latest." default:"false"`
	CacheKey   string `help:"Cache key prefix, for invalidating cached entries." default:"v1"`
}

func (c *installFirefoxCmd) Run(ctx context.Context, client *sdk.Client) error {
	installer, err := browser.NewInstaller(client, os.Stderr)
	if err != nil {
		return err
	}
	return installer.InstallFirefox(ctx, browser.FirefoxOptions{
		Version:    c.Version,
		InstallDir: c.InstallDir,
		UseCache:   c.UseCache,
		CacheKey:   c.CacheKey,
	})
}

type installChromeForTestingCmd struct {
	Version      string `help:"Version of Chrome for Testing to install. Defaults to the latest stable release." default:"latest"`
	InstallDir   string `help:"Directory to symlink Chrome for Testing and its driver into." default:"/usr/local/bin"`
	ChromeDriver bool   `help:"Also install the matching Chrome for Testing ChromeDriver." default:"true" negatable:""`
}

func (c *installChromeForTestingCmd) Run(ctx context.Context, client *sdk.Client) error {
	installer, err := browser.NewInstaller(client, os.Stderr)
	if err != nil {
		return err
	}
	return installer.InstallChromeForTesting(ctx, browser.ChromeForTestingOptions{
		Version:      c.Version,
		InstallDir:   c.InstallDir,
		ChromeDriver: c.ChromeDriver,
	})
}

type installEdgeCmd struct {
	Version string `help:"Version of Edge to install. Only honoured on Debian/Ubuntu; macOS always installs the latest." default:"latest"`
}

func (c *installEdgeCmd) Run(ctx context.Context, client *sdk.Client) error {
	installer, err := browser.NewInstaller(client, os.Stderr)
	if err != nil {
		return err
	}
	return installer.InstallEdge(ctx, browser.EdgeOptions{Version: c.Version})
}

type installEdgeDriverCmd struct {
	InstallDir string `help:"Directory to install Edge WebDriver into." default:"/usr/local/bin"`
}

func (c *installEdgeDriverCmd) Run(ctx context.Context, client *sdk.Client) error {
	installer, err := browser.NewInstaller(client, os.Stderr)
	if err != nil {
		return err
	}
	return installer.InstallEdgeDriver(ctx, browser.EdgeDriverOptions{InstallDir: c.InstallDir})
}

func main() {
	sdk.Run(run)
}

func run(ctx context.Context, client *sdk.Client) error {
	var flags cli
	kCtx := kong.Parse(&flags,
		kong.Name("setup-browser-tools"),
		kong.Description("Install browsers and browser-testing tools.\n\nExample:\n  circleci run setup-browser-tools@<version> install-chrome --version latest"),
		kong.UsageOnError(),
		functionyaml.Embed(),
	)
	kCtx.BindTo(ctx, (*context.Context)(nil))
	kCtx.Bind(client)
	return kCtx.Run(ctx)
}
