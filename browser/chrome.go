package browser

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	ChannelStable = "stable"
	ChannelBeta   = "beta"
)

type ChromeOptions struct {
	Version         string
	Channel         string
	ReplaceExisting bool
	UseCache        bool
	CacheKey        string
}

// chromeLinuxTree is where the Debian package puts Chrome, and so what gets
// cached on Linux.
const chromeLinuxTree = "/opt/google/chrome"

func (i *Installer) InstallChrome(ctx context.Context, opts ChromeOptions) error {
	if i.Platform.IsLinuxARM64() {
		return Unsupported("Chrome", i.Platform, "Google publishes no Linux ARM64 build")
	}

	requested, err := ExpandVersion(opts.Version, "--version", nil)
	if err != nil {
		return err
	}

	target, err := i.resolveChromeVersion(ctx, requested, opts.Channel)
	if err != nil {
		return err
	}

	if i.chromeAlreadyInstalled(ctx, target, opts) {
		return nil
	}

	if err := i.installChromeForPlatform(ctx, target, requested, opts); err != nil {
		return err
	}

	installed, err := i.verifyChrome(ctx, requested, target, opts.Channel)
	if err != nil {
		return err
	}
	i.logf("installed Chrome %s", installed)
	RecordVersion(i.Out, "Chrome", installed)

	if opts.UseCache {
		i.Cache.Save(ctx, ChromeCacheKey(i.Host, opts.CacheKey, opts.Channel, target), []string{chromeLinuxTree})
	}
	return nil
}

// resolveChromeVersion turns "latest" into a concrete release, and passes any
// other value through untouched.
func (i *Installer) resolveChromeVersion(ctx context.Context, requested, channel string) (string, error) {
	if requested != Latest {
		return requested, nil
	}
	// On Alpine the apk package decides the version, so there is nothing to
	// resolve it against.
	if i.Platform.Distro == DistroAlpine {
		return Latest, nil
	}

	var releases []struct {
		Version string `json:"version"`
	}
	if err := i.Fetcher.JSON(ctx, i.chromiumDashURL(channel), &releases); err != nil {
		return "", err
	}
	if len(releases) == 0 {
		return "", fmt.Errorf("no Chrome releases reported for channel %s", channel)
	}
	return releases[0].Version, nil
}

func (i *Installer) chromiumDashURL(channel string) string {
	dashPlatform := "Linux"
	if i.Platform.IsMacOS() {
		dashPlatform = "Mac"
	}
	dashChannel := "Stable"
	if channel == ChannelBeta {
		dashChannel = "Beta"
	}
	return fmt.Sprintf("%s/fetch_releases?channel=%s&platform=%s",
		i.Endpoints.ChromiumDash, dashChannel, dashPlatform)
}

// chromeAlreadyInstalled reports whether the install can be skipped, removing an
// existing install first when a different version is wanted and
// --replace-existing is set.
//
// Unlike the orb, the installed version is parsed out of the banner on every
// platform. The orb only did that on Debian and compared a raw
// "Google Chrome 147.0.7727.55" against a bare "147.0.7727.55" elsewhere, so
// --replace-existing always reinstalled there even at the same version.
func (i *Installer) chromeAlreadyInstalled(ctx context.Context, target string, opts ChromeOptions) bool {
	installed, found := i.installedChromeVersion(ctx, opts.Channel)
	if !found {
		i.logf("Chrome is not installed; installing it")
		return false
	}

	if installed == target {
		i.logf("Chrome %s is already installed", installed)
		return true
	}
	if !opts.ReplaceExisting {
		if target == Latest {
			i.logf("Chrome %s is already installed; pass --replace-existing to replace it", installed)
		} else {
			i.logf("Chrome %s is installed but %s was requested; keeping it (pass --replace-existing to replace it)", installed, target)
		}
		return true
	}

	i.logf("Chrome %s is installed; replacing it with %s", installed, target)
	i.removeChrome(ctx, opts.Channel)
	return false
}

// chromeBinaries lists the names a channel's Chrome can be found under. The bare
// google-chrome name counts only for stable: any channel's package or macOS
// wrapper can claim it, so accepting it for beta lets an installed stable
// satisfy a beta request.
func (i *Installer) chromeBinaries(channel string) []string {
	if i.Platform.Distro == DistroAlpine {
		return []string{"chromium-browser"}
	}
	if channel != ChannelStable {
		return []string{"google-chrome-" + channel}
	}
	return []string{"google-chrome-" + channel, "google-chrome"}
}

func (i *Installer) installedChromeVersion(ctx context.Context, channel string) (string, bool) {
	for _, bin := range i.chromeBinaries(channel) {
		if !i.Host.has(bin) {
			continue
		}
		banner, err := i.Runner.Output(ctx, bin, "--version")
		if err != nil {
			continue
		}
		if v := ParseBrowserVersion(banner); v != "" {
			return v, true
		}
	}
	return "", false
}

// removeChrome removes only the given channel's Chrome, so replacing beta
// leaves an installed stable alone. It is best-effort, as in the orb: a failure
// to remove is not fatal because the install that follows either succeeds or
// reports its own error.
func (i *Installer) removeChrome(ctx context.Context, channel string) {
	switch i.Platform.Distro {
	case DistroMacOS:
		_ = i.Runner.Exec(ctx, "rm", "-rf", chromeMacApp(channel))
	case DistroAlpine:
		_ = i.Runner.Exec(ctx, "apk", "del", "--force-broken-world", "chromium")
	case DistroRHEL:
		_ = i.Runner.Exec(ctx, "yum", "remove", "-y", "google-chrome-"+channel)
	case DistroDebian:
		_ = i.Runner.Exec(ctx, "apt-get", "-y", "--purge", "remove", "google-chrome-"+channel)
	}
}

func (i *Installer) installChromeForPlatform(ctx context.Context, target, requested string, opts ChromeOptions) error {
	if opts.UseCache && i.restoreChromeFromCache(ctx, target, opts) {
		return nil
	}

	switch i.Platform.Distro {
	case DistroMacOS:
		return i.installChromeMacOS(ctx, opts.Channel)
	case DistroAlpine:
		return i.installChromeAlpine(ctx)
	case DistroRHEL:
		return i.installChromeRHEL(ctx, target, requested, opts.Channel)
	case DistroDebian:
		return i.installChromeDebian(ctx, target, requested, opts.Channel)
	default:
		return Unsupported("Chrome", i.Platform, "")
	}
}

// restoreChromeFromCache is Linux-only: on macOS Chrome lives in /Applications
// and has to be registered by the installer, so there is no tree to drop back
// into place.
func (i *Installer) restoreChromeFromCache(ctx context.Context, target string, opts ChromeOptions) bool {
	if i.Platform.IsMacOS() {
		return false
	}
	key := ChromeCacheKey(i.Host, opts.CacheKey, opts.Channel, target)
	if !i.Cache.Restore(ctx, key, chromeLinuxTree) {
		return false
	}
	return i.linkChromeFromTree(ctx, opts.Channel) == nil
}

// linkChromeFromTree recreates the symlinks the Debian package would have made,
// which a restored tree does not carry.
func (i *Installer) linkChromeFromTree(ctx context.Context, channel string) error {
	target := filepath.Join(chromeLinuxTree, "google-chrome")
	link := filepath.Join("/usr/bin", "google-chrome-"+channel)

	if err := i.Runner.Exec(ctx, "ln", "-sf", target, link); err != nil {
		return err
	}
	return i.Runner.Exec(ctx, "ln", "-sf", link, "/usr/bin/google-chrome")
}

func (i *Installer) installChromeAlpine(ctx context.Context) error {
	if err := i.Runner.RunWithRetry(ctx, Command{Name: "apk", Args: []string{"update"}}); err != nil {
		return err
	}
	return i.Runner.Exec(ctx, "apk", "add", "--no-cache", "chromium")
}

func (i *Installer) installChromeMacOS(ctx context.Context, channel string) error {
	dir, cleanup, err := TempDir("chrome")
	if err != nil {
		return err
	}
	defer cleanup()

	pkg, err := i.Fetcher.Download(ctx, i.chromeMacURL(channel), dir, "googlechrome.pkg")
	if err != nil {
		return err
	}
	if err := i.Runner.Exec(ctx, "/usr/sbin/installer", "-pkg", pkg, "-target", "/"); err != nil {
		return err
	}

	app := chromeMacApp(channel)
	// Chrome ships quarantined; without clearing the attribute it refuses to
	// launch headless.
	_ = i.Runner.Exec(ctx, "xattr", "-rc", app)

	return i.writeChromeWrapper(ctx, dir, channel, app)
}

func chromeMacApp(channel string) string {
	if channel == ChannelBeta {
		return "/Applications/Google Chrome Beta.app"
	}
	return "/Applications/Google Chrome.app"
}

// writeChromeWrapper puts a google-chrome-<channel> script on PATH, since the
// macOS package only installs an app bundle.
func (i *Installer) writeChromeWrapper(ctx context.Context, dir, channel, app string) error {
	binary := filepath.Join(app, "Contents", "MacOS", strings.TrimSuffix(filepath.Base(app), ".app"))
	script := fmt.Sprintf("#!/usr/bin/env bash\nexec %q \"$@\"\n", binary)

	name := "google-chrome-" + channel
	local := filepath.Join(dir, name)
	if err := writeFile(local, strings.NewReader(script), 0o755); err != nil {
		return err
	}

	dest := filepath.Join("/usr/local/bin", name)
	if err := i.Runner.Exec(ctx, "cp", local, dest); err != nil {
		return err
	}
	if err := i.Runner.Exec(ctx, "chmod", "+x", dest); err != nil {
		return err
	}
	return i.Runner.Exec(ctx, "ln", "-sf", dest, "/usr/local/bin/google-chrome")
}

func (i *Installer) chromeMacURL(channel string) string {
	// The accept_tos path segment is what makes the package install without an
	// interactive licence prompt.
	const acceptTOS = "accept_tos%3Dhttps%253A%252F%252Fwww.google.com%252Fintl%252Fen_ph%252Fchrome%252Fterms%252F%26_and_accept_tos%3Dhttps%253A%252F%252Fpolicies.google.com%252Fterms"
	return fmt.Sprintf("%s/%s/%s/googlechrome.pkg", i.Endpoints.GoogleChromeMac, channel, acceptTOS)
}

// installChromeRHEL installs the .rpm and lets yum resolve its dependencies from
// the distro's own repositories.
//
// The orb instead downloads liberation-fonts from a hardcoded
// mirror.centos.org/centos/7 URL. That mirror was retired along with CentOS 7 in
// 2024 and now answers 404, so the orb's RHEL path cannot succeed on any host
// today. `yum install` on a local file resolves dependencies, which is what the
// separate font download was working around on CentOS 7's older yum.
func (i *Installer) installChromeRHEL(ctx context.Context, target, requested, channel string) error {
	dir, cleanup, err := TempDir("chrome")
	if err != nil {
		return err
	}
	defer cleanup()

	rpm, err := i.Fetcher.Download(ctx, i.chromeRPMURL(target, requested, channel), dir, "google-chrome.rpm")
	if err != nil {
		return err
	}
	return i.Runner.Exec(ctx, "yum", "install", "-y", rpm)
}

func (i *Installer) chromeRPMURL(target, requested, channel string) string {
	if requested == Latest {
		return fmt.Sprintf("%s/direct/google-chrome-%s_current_x86_64.rpm", i.Endpoints.GoogleLinux, channel)
	}
	return fmt.Sprintf("%s/chrome/rpm/stable/x86_64/google-chrome-%s-%s-1.x86_64.rpm",
		i.Endpoints.GoogleLinux, channel, target)
}

func (i *Installer) installChromeDebian(ctx context.Context, target, requested, channel string) error {
	if requested == Latest {
		return i.installChromeDebianRepo(ctx, channel)
	}
	return i.installChromeDebianPackage(ctx, target, channel)
}

func (i *Installer) installChromeDebianRepo(ctx context.Context, channel string) error {
	dir, cleanup, err := TempDir("chrome")
	if err != nil {
		return err
	}
	defer cleanup()

	key, err := i.Fetcher.Download(ctx, i.Endpoints.GoogleLinux+"/linux_signing_key.pub", dir, "google.asc")
	if err != nil {
		return err
	}
	const keyring = "/etc/apt/trusted.gpg.d/linux_signing_key.asc"
	if err := i.Runner.Exec(ctx, "cp", key, keyring); err != nil {
		return err
	}
	// apt runs as the _apt user and silently ignores a keyring it cannot read,
	// which surfaces later as NO_PUBKEY rather than a permissions error.
	if err := i.Runner.Exec(ctx, "chmod", "0644", keyring); err != nil {
		return err
	}

	list := filepath.Join(dir, "google-chrome.list")
	entry := fmt.Sprintf("deb [arch=amd64] %s/chrome/deb/ stable main\n", i.Endpoints.GoogleLinux)
	if err := writeFile(list, strings.NewReader(entry), 0o644); err != nil {
		return err
	}
	const listPath = "/etc/apt/sources.list.d/google-chrome.list"
	if err := i.Runner.Exec(ctx, "cp", list, listPath); err != nil {
		return err
	}
	if err := i.Runner.Exec(ctx, "chmod", "0644", listPath); err != nil {
		return err
	}

	if err := i.Runner.AptUpdate(ctx); err != nil {
		return err
	}
	return i.aptInstall(ctx, "google-chrome-"+channel)
}

// installChromeDebianPackage fetches a specific .deb, since Google keeps only the
// current release in the apt repo.
func (i *Installer) installChromeDebianPackage(ctx context.Context, target, channel string) error {
	dir, cleanup, err := TempDir("chrome")
	if err != nil {
		return err
	}
	defer cleanup()

	deb, err := i.Fetcher.Download(ctx, i.chromeDebURL(target, channel), dir, "chrome.deb")
	if err != nil {
		return err
	}

	if err := i.Runner.AptUpdate(ctx); err != nil {
		return err
	}
	return i.aptInstall(ctx, deb)
}

// chromeDebURL builds the pooled package URL from the resolved version. The orb
// built this from the raw flag instead, so an unexpanded $VAR reached the URL.
func (i *Installer) chromeDebURL(target, channel string) string {
	return fmt.Sprintf("%s/chrome/deb/pool/main/g/google-chrome-%s/google-chrome-%s_%s-1_amd64.deb",
		i.Endpoints.GoogleLinux, channel, channel, target)
}

func (i *Installer) aptInstall(ctx context.Context, packages ...string) error {
	args := append([]string{"install", "-y"}, packages...)
	_, err := i.Runner.Run(ctx, Command{
		Name: "apt-get",
		Args: args,
		Env:  []string{"DEBIAN_FRONTEND=noninteractive"},
	})
	return err
}

// verifyChrome confirms Chrome runs and reports the version wanted.
//
// When the caller asked for "latest", any working install satisfies it. The
// package manager picks the version, and it is routinely not the newest release
// Google has announced — Alpine's apk Chromium trails it by months, and apt can
// be a build behind. Resolving "latest" is still useful for the installation
// check and the cache key, but it is not something to hold the install to.
//
// A pinned version is accepted on a major.minor.build prefix as well, matching
// the orb, since the packaged build can differ in its last component.
func (i *Installer) verifyChrome(ctx context.Context, requested, target, channel string) (string, error) {
	installed, found := i.installedChromeVersion(ctx, channel)
	if !found {
		return "", fmt.Errorf("no working Chrome on PATH after installing %s", target)
	}
	if requested == Latest || installed == target ||
		strings.HasPrefix(target, installed) || strings.HasPrefix(installed, versionPrefix(target)) {
		return installed, nil
	}
	return "", fmt.Errorf("installed Chrome is %s, but %s was requested", installed, target)
}

// versionPrefix drops the last dotted component, so 147.0.7727.55 becomes
// 147.0.7727.
func versionPrefix(version string) string {
	if idx := strings.LastIndex(version, "."); idx > 0 {
		return version[:idx]
	}
	return version
}
