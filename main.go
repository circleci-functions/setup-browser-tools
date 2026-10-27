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
	InstallBrowserTools installBrowserToolsCmd `cmd:"" help:"Install a default set of browsers and drivers: Chrome, ChromeDriver, Firefox and geckodriver."`

	InstallChrome       installChromeCmd       `cmd:"" help:"Install Google Chrome."`
	InstallChromedriver installChromedriverCmd `cmd:"" help:"Install ChromeDriver, matching the installed Chrome."`
	InstallFirefox      installFirefoxCmd      `cmd:"" help:"Install Mozilla Firefox."`
	InstallGeckodriver  installGeckodriverCmd  `cmd:"" help:"Install geckodriver, Mozilla's WebDriver proxy for Firefox."`

	InstallChromeForTesting installChromeForTestingCmd `cmd:"" help:"Install Chrome for Testing, and optionally its matching ChromeDriver."`
	InstallEdge             installEdgeCmd             `cmd:"" help:"Install Microsoft Edge."`
	InstallEdgeDriver       installEdgeDriverCmd       `cmd:"" help:"Install Edge WebDriver, matching the installed Edge."`

	StartXvfb startXvfbCmd `cmd:"" help:"Start a virtual X display (Xvfb) for headed browsers on Linux, and export DISPLAY to later steps."`
}

type installBrowserToolsCmd struct {
	Chrome       bool `help:"Install Google Chrome." default:"true" negatable:""`
	Chromedriver bool `help:"Install ChromeDriver." default:"true" negatable:""`
	Firefox      bool `help:"Install Mozilla Firefox." default:"true" negatable:""`
	Geckodriver  bool `help:"Install geckodriver." default:"true" negatable:""`

	ChromeForTesting       bool `help:"Install Chrome for Testing." default:"false"`
	ChromeForTestingDriver bool `help:"Install the Chrome for Testing ChromeDriver alongside Chrome for Testing, in place of the default ChromeDriver. Passing --chromedriver as well is an error." default:"false"`
	Edge                   bool `help:"Install Microsoft Edge." default:"false"`
	EdgeDriver             bool `help:"Install Edge WebDriver." default:"false"`

	ChromeVersion          string `help:"Version of Chrome to install." default:"latest"`
	ReplaceExistingChrome  bool   `help:"Replace an existing Chrome installation when it is a different version." default:"false"`
	ChromedriverInstallDir string `help:"Directory to install ChromeDriver into." default:"/usr/local/bin"`

	ChromeForTestingVersion    string `help:"Version of Chrome for Testing to install." default:"latest"`
	ChromeForTestingInstallDir string `help:"Directory to symlink Chrome for Testing into and copy its driver into." default:"/usr/local/bin"`

	FirefoxVersion    string `help:"Version of Firefox to install." default:"latest"`
	FirefoxInstallDir string `help:"Directory to install Firefox into." default:"/usr/local/bin"`

	GeckodriverVersion    string `help:"Release tag of geckodriver to install." default:"latest"`
	GeckodriverInstallDir string `help:"Directory to install geckodriver into." default:"/usr/local/bin"`

	EdgeVersion          string `help:"Version of Edge to install." default:"latest"`
	EdgeDriverInstallDir string `help:"Directory to install Edge WebDriver into." default:"/usr/local/bin"`

	Xvfb bool `help:"After installing, start a virtual X display (Xvfb) on :99 and export DISPLAY to later steps, as start-xvfb does." default:"false"`
}

func (c *installBrowserToolsCmd) Run(ctx context.Context, client *sdk.Client, explicit browser.ExplicitFlags) error {
	installer, err := browser.NewInstaller(client, os.Stderr)
	if err != nil {
		return err
	}
	return installer.InstallBrowserTools(ctx, browser.BrowserToolsOptions{
		Chrome:                 c.Chrome,
		ChromeDriver:           c.Chromedriver,
		Firefox:                c.Firefox,
		Geckodriver:            c.Geckodriver,
		ChromeForTesting:       c.ChromeForTesting,
		ChromeForTestingDriver: c.ChromeForTestingDriver,
		Edge:                   c.Edge,
		EdgeDriver:             c.EdgeDriver,

		ChromeVersion:              c.ChromeVersion,
		ReplaceExistingChrome:      c.ReplaceExistingChrome,
		ChromeDriverInstallDir:     c.ChromedriverInstallDir,
		ChromeForTestingVersion:    c.ChromeForTestingVersion,
		ChromeForTestingInstallDir: c.ChromeForTestingInstallDir,
		FirefoxVersion:             c.FirefoxVersion,
		FirefoxInstallDir:          c.FirefoxInstallDir,
		GeckodriverVersion:         c.GeckodriverVersion,
		GeckodriverInstallDir:      c.GeckodriverInstallDir,
		EdgeVersion:                c.EdgeVersion,
		EdgeDriverInstallDir:       c.EdgeDriverInstallDir,

		Xvfb: c.Xvfb,

		Explicit: explicit,
	})
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
	InstallDir   string `help:"Directory to symlink Chrome for Testing into and copy its driver into." default:"/usr/local/bin"`
	Chromedriver bool   `help:"Also install the matching Chrome for Testing ChromeDriver." default:"true" negatable:""`
}

func (c *installChromeForTestingCmd) Run(ctx context.Context, client *sdk.Client) error {
	installer, err := browser.NewInstaller(client, os.Stderr)
	if err != nil {
		return err
	}
	return installer.InstallChromeForTesting(ctx, browser.ChromeForTestingOptions{
		Version:      c.Version,
		InstallDir:   c.InstallDir,
		ChromeDriver: c.Chromedriver,
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

type startXvfbCmd struct {
	Display string `help:"X display to start Xvfb on, and to export as DISPLAY." default:":99"`
	Screen  string `help:"Geometry and colour depth of the virtual screen, as WIDTHxHEIGHTxDEPTH." default:"1280x1024x24"`
}

func (c *startXvfbCmd) Run(ctx context.Context, client *sdk.Client) error {
	installer, err := browser.NewInstaller(client, os.Stderr)
	if err != nil {
		return err
	}
	return installer.StartXvfb(ctx, browser.XvfbOptions{Display: c.Display, Screen: c.Screen})
}

func main() {
	sdk.Run(run)
}

func run(ctx context.Context, client *sdk.Client) error {
	var flags cli
	kCtx := kong.Parse(&flags,
		kong.Name("setup-browser-tools"),
		kong.Description("Install browsers and browser-testing tools.\n\nExample:\n  circleci run github.com/circleci-functions/setup-browser-tools@<version> -- install-chrome --version latest"),
		kong.UsageOnError(),
		functionyaml.Embed(),
	)
	kCtx.BindTo(ctx, (*context.Context)(nil))
	kCtx.Bind(client)
	kCtx.Bind(explicitFlags(kCtx))
	return kCtx.Run(ctx)
}

// explicitFlags returns the flags that appeared in argv, including the negated
// --no-x forms. kong.Value.Set cannot be used for this: Value.ApplyDefault sets
// it too, so it is true for defaulted flags as well. Path only gains a flag entry
// when one is actually parsed, and Resolved marks the ones a resolver supplied
// rather than the command line.
func explicitFlags(kCtx *kong.Context) browser.ExplicitFlags {
	out := browser.ExplicitFlags{}
	for _, p := range kCtx.Path {
		if p.Flag != nil && !p.Resolved {
			out[p.Flag.Name] = true
		}
	}
	return out
}
