package browser

import (
	"context"
	"errors"
)

// ExplicitFlags names the flags a caller actually passed, as opposed to those
// left at their default. It decides whether an unsupported platform is fatal.
type ExplicitFlags map[string]bool

func (e ExplicitFlags) Has(flag string) bool { return e[flag] }

type BrowserToolsOptions struct {
	Chrome                 bool
	ChromeDriver           bool
	Firefox                bool
	Geckodriver            bool
	ChromeForTesting       bool
	ChromeForTestingDriver bool
	Edge                   bool
	EdgeDriver             bool

	ChromeVersion              string
	ReplaceExistingChrome      bool
	ChromeForTestingVersion    string
	ChromeForTestingInstallDir string
	ChromeDriverInstallDir     string
	FirefoxVersion             string
	FirefoxInstallDir          string
	GeckodriverVersion         string
	GeckodriverInstallDir      string
	EdgeVersion                string
	EdgeDriverInstallDir       string

	Explicit ExplicitFlags
}

// component is one browser the aggregate can install.
type component struct {
	// Name is for log messages.
	Name string
	// Flag is the flag that turns it on, used to tell an explicit request from a
	// default.
	Flag    string
	Enabled bool
	Install func(context.Context) error
}

// InstallBrowserTools installs the enabled set in the orb's dispatch order.
//
// An unsupported platform is fatal only when the caller asked for that component
// by name. A component that is merely on by default is warned about and skipped,
// because erroring would turn a partial success into a hard failure — a bare run
// on Alpine would fail outright rather than installing the two browsers that do
// work there. Any other failure is always fatal.
func (i *Installer) InstallBrowserTools(ctx context.Context, opts BrowserToolsOptions) error {
	opts, err := i.settleChromeDrivers(opts)
	if err != nil {
		return err
	}

	for _, c := range i.browserToolsComponents(opts) {
		if !c.Enabled {
			continue
		}

		err := c.Install(ctx)
		switch {
		case err == nil:
		case IsUnsupported(err) && !opts.Explicit.Has(c.Flag):
			i.logf("skipping %s: %v", c.Name, err)
		default:
			return err
		}
	}
	return nil
}

// settleChromeDrivers keeps the set to one chromedriver. The Chrome for Testing
// driver and ChromeDriver are both installed as chromedriver on PATH, so with
// both on the second would replace the first. The Chrome for Testing driver is
// off by default, so it wins over a ChromeDriver that is only on by default;
// asking for both by name is an error.
func (i *Installer) settleChromeDrivers(opts BrowserToolsOptions) (BrowserToolsOptions, error) {
	if !opts.ChromeForTesting || !opts.ChromeForTestingDriver || !opts.ChromeDriver {
		return opts, nil
	}
	if opts.Explicit.Has("chromedriver") && opts.Explicit.Has("chrome-for-testing-driver") {
		return opts, errors.New("--chromedriver and --chrome-for-testing-driver both install chromedriver; pass only one")
	}
	i.logf("skipping ChromeDriver: using the Chrome for Testing chromedriver")
	opts.ChromeDriver = false
	return opts, nil
}

// browserToolsComponents returns the components to install, honouring the test
// seam when one is set.
func (i *Installer) browserToolsComponents(opts BrowserToolsOptions) []component {
	if i.componentsOverride != nil {
		return i.componentsOverride(opts)
	}
	return i.defaultComponents(opts)
}

// defaultComponents lists the components in the orb's dispatch order: Firefox,
// geckodriver, Chrome for Testing, Chrome, ChromeDriver, Edge, Edge WebDriver.
// The order matters — ChromeDriver reads the installed Chrome, and Edge WebDriver
// reads the installed Edge.
func (i *Installer) defaultComponents(opts BrowserToolsOptions) []component {
	var all []component
	all = append(all, i.mozillaComponents(opts)...)
	all = append(all, i.chromeComponents(opts)...)
	all = append(all, i.edgeComponents(opts)...)
	return all
}

func (i *Installer) mozillaComponents(opts BrowserToolsOptions) []component {
	return []component{
		{
			Name: "Firefox", Flag: "firefox", Enabled: opts.Firefox,
			Install: func(ctx context.Context) error {
				return i.InstallFirefox(ctx, FirefoxOptions{
					Version:    opts.FirefoxVersion,
					InstallDir: opts.FirefoxInstallDir,
				})
			},
		},
		{
			Name: "geckodriver", Flag: "geckodriver", Enabled: opts.Geckodriver,
			Install: func(ctx context.Context) error {
				return i.InstallGeckodriver(ctx, GeckodriverOptions{
					Version:    opts.GeckodriverVersion,
					InstallDir: opts.GeckodriverInstallDir,
				})
			},
		},
	}
}

func (i *Installer) chromeComponents(opts BrowserToolsOptions) []component {
	return []component{
		{
			Name: "Chrome for Testing", Flag: "chrome-for-testing", Enabled: opts.ChromeForTesting,
			Install: func(ctx context.Context) error {
				return i.InstallChromeForTesting(ctx, ChromeForTestingOptions{
					Version:      opts.ChromeForTestingVersion,
					InstallDir:   opts.ChromeForTestingInstallDir,
					ChromeDriver: opts.ChromeForTestingDriver,
				})
			},
		},
		{
			Name: "Chrome", Flag: "chrome", Enabled: opts.Chrome,
			Install: func(ctx context.Context) error {
				return i.InstallChrome(ctx, ChromeOptions{
					Version:         opts.ChromeVersion,
					Channel:         ChannelStable,
					ReplaceExisting: opts.ReplaceExistingChrome,
				})
			},
		},
		{
			Name: "ChromeDriver", Flag: "chromedriver", Enabled: opts.ChromeDriver,
			Install: func(ctx context.Context) error {
				return i.InstallChromeDriver(ctx, ChromeDriverOptions{
					InstallDir: opts.ChromeDriverInstallDir,
				})
			},
		},
	}
}

func (i *Installer) edgeComponents(opts BrowserToolsOptions) []component {
	return []component{
		{
			Name: "Microsoft Edge", Flag: "edge", Enabled: opts.Edge,
			Install: func(ctx context.Context) error {
				return i.InstallEdge(ctx, EdgeOptions{Version: opts.EdgeVersion})
			},
		},
		{
			Name: "Edge WebDriver", Flag: "edge-driver", Enabled: opts.EdgeDriver,
			Install: func(ctx context.Context) error {
				return i.InstallEdgeDriver(ctx, EdgeDriverOptions{
					InstallDir: opts.EdgeDriverInstallDir,
				})
			},
		},
	}
}

// BrowserToolsOrder returns the component names in dispatch order, for tests and
// for reporting what will run.
func (i *Installer) BrowserToolsOrder() []string {
	all := i.defaultComponents(BrowserToolsOptions{})
	names := make([]string, 0, len(all))
	for _, c := range all {
		names = append(names, c.Name)
	}
	return names
}
