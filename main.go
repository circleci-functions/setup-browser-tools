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
