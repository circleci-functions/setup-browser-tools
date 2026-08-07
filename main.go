// Command setup-browser-tools installs browsers and browser-testing tools into
// a CircleCI job: Google Chrome, Chrome for Testing, ChromeDriver, Microsoft
// Edge and its WebDriver, Firefox, and geckodriver. Each is a subcommand, and
// install-browser-tools installs a default set in one step.
//
// Supported platforms: macOS, Debian/Ubuntu, Alpine and RHEL/CentOS Linux.
// Coverage varies by browser; see README.md for the matrix.
package main

import (
	"context"

	"github.com/alecthomas/kong"

	"github.com/circleci-functions/toolkit/functionyaml"
	"github.com/circleci-functions/toolkit/sdk"
)

type cli struct{}

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
