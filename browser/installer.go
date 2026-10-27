package browser

import (
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/circleci-functions/toolkit/sdk"
)

// Installer carries everything the install commands share, so each one takes
// options rather than eight collaborators.
type Installer struct {
	Host      Host
	Platform  Platform
	Runner    *Runner
	Fetcher   Fetcher
	Cache     Cache
	Endpoints Endpoints
	Out       io.Writer
	XServer   XServer

	// exported names the browser each *_BIN variable was exported for in this
	// run. The first install to export one keeps it, so in install-browser-tools
	// Chrome for Testing, which runs before Chrome, wins CHROME_BIN: it is off by
	// default, so asking for it alongside Chrome means it is the browser the
	// tests are meant to drive, just as its chromedriver takes the place of the
	// default one in settleChromeDrivers.
	exported map[string]string

	// componentsOverride replaces the aggregate's component list. Tests set it to
	// exercise the unsupported-platform policy without installing anything.
	componentsOverride func(BrowserToolsOptions) []component
}

// NewInstaller detects the host and wires the collaborators together. client may
// be nil, which is what running outside a CircleCI job looks like; only caching
// is affected.
func NewInstaller(client *sdk.Client, out io.Writer) (*Installer, error) {
	if out == nil {
		out = os.Stderr
	}

	host := Host{}
	platform, err := Detect(host)
	if err != nil {
		return nil, err
	}

	priv := ResolvePrivilege(host)

	return &Installer{
		Host:      host,
		Platform:  platform,
		Runner:    &Runner{Priv: priv, Out: out},
		Fetcher:   Fetcher{},
		Cache:     Cache{Client: client, Out: out},
		Endpoints: DefaultEndpoints(),
		Out:       out,
	}, nil
}

func (i *Installer) logf(format string, args ...any) {
	fmt.Fprintf(i.Out, cacheKeyPrefix+": "+format+"\n", args...)
}

// versionPattern matches a dotted release number, which is how a version is
// pulled out of the assorted "Google Chrome 147.0.7727.55" and
// "Mozilla Firefox 149.0.2" banners the browsers print.
var versionPattern = regexp.MustCompile(`[0-9]+(\.[0-9]+)+[a-z0-9]*`)

// ParseBrowserVersion extracts the release number from a --version banner,
// returning "" when there is none.
func ParseBrowserVersion(banner string) string {
	return versionPattern.FindString(banner)
}
