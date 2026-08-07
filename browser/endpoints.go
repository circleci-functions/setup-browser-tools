package browser

// Endpoints holds the base URLs this package reads from. Every field has a
// production default; tests override them to point at a local server, which is
// what keeps URL construction and version resolution testable without network.
type Endpoints struct {
	ChromiumDash        string
	GoogleLinux         string
	GoogleChromeMac     string
	ChromeDriverLegacy  string
	ChromeForTesting    string
	GeckodriverGitHub   string
	MozillaArchive      string
	MozillaProduct      string
	ChromeForTestingCDN string
	MicrosoftPackages   string
	MSEdgeDriver        string
}

func DefaultEndpoints() Endpoints {
	return Endpoints{
		ChromiumDash:        "https://chromiumdash.appspot.com",
		GoogleLinux:         "https://dl.google.com/linux",
		GoogleChromeMac:     "https://dl.google.com/chrome/mac",
		ChromeDriverLegacy:  "https://chromedriver.storage.googleapis.com",
		ChromeForTesting:    "https://googlechromelabs.github.io/chrome-for-testing",
		GeckodriverGitHub:   "https://github.com/mozilla/geckodriver",
		MozillaArchive:      "https://archive.mozilla.org/pub/firefox/releases",
		MozillaProduct:      "https://product-details.mozilla.org/1.0",
		ChromeForTestingCDN: "https://storage.googleapis.com/chrome-for-testing-public",
		MicrosoftPackages:   "https://packages.microsoft.com",
		MSEdgeDriver:        "https://msedgedriver.microsoft.com",
	}
}
