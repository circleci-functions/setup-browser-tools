package browser

// Endpoints holds the base URLs this package reads from. Every field has a
// production default; tests override them to point at a local server, which is
// what keeps URL construction and version resolution testable without network.
type Endpoints struct {
	ChromiumDash       string
	GoogleLinux        string
	GoogleChromeMac    string
	CentOSMirror       string
	ChromeDriverLegacy string
	ChromeForTesting   string
}

func DefaultEndpoints() Endpoints {
	return Endpoints{
		ChromiumDash:       "https://chromiumdash.appspot.com",
		GoogleLinux:        "https://dl.google.com/linux",
		GoogleChromeMac:    "https://dl.google.com/chrome/mac",
		CentOSMirror:       "http://mirror.centos.org",
		ChromeDriverLegacy: "https://chromedriver.storage.googleapis.com",
		ChromeForTesting:   "https://googlechromelabs.github.io/chrome-for-testing",
	}
}
