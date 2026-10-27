package browser

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// firefoxXZFrom is the major release from which Mozilla ships .tar.xz rather
// than .tar.bz2.
const firefoxXZFrom = 135

// usrLocalBin is where a Firefox symlink is always created, in addition to
// --install-dir, because it is on PATH in every CircleCI image.
const usrLocalBin = "/usr/local/bin"

type FirefoxOptions struct {
	Version    string
	InstallDir string
	UseCache   bool
	CacheKey   string
}

// firefoxRelease describes where one release lives and what it is called, both
// remotely and once downloaded.
type firefoxRelease struct {
	Version string
	// Platform is Mozilla's directory name for the build.
	Platform string
	// Filename is the archive's own name, used for the local copy.
	Filename string
	// ManifestEntry is the path exactly as the checksum manifest lists it. One
	// manifest carries the same base name for every architecture and locale, so
	// the full path is what makes the lookup unambiguous.
	ManifestEntry string
	// URL is the full download URL, percent-encoded.
	URL string
}

func (i *Installer) InstallFirefox(ctx context.Context, opts FirefoxOptions) error {
	if err := i.firefoxSupported(); err != nil {
		return err
	}

	requested, err := ExpandVersion(opts.Version, "--version", nil)
	if err != nil {
		return err
	}

	version, err := i.resolveFirefoxVersion(ctx, requested)
	if err != nil {
		return err
	}

	if installed, found := i.installedFirefoxVersion(ctx); found {
		if installed == version {
			i.logf("Firefox %s is already installed", installed)
			return nil
		}
		i.logf("Firefox %s is installed; replacing it with %s", installed, version)
		_ = i.Runner.Exec(ctx, "rm", "-f", filepath.Join(opts.InstallDir, "firefox"), usrLocalBin+"/firefox")
	}

	release, err := i.firefoxRelease(version)
	if err != nil {
		return err
	}

	if err := i.prepareFirefoxHost(ctx, opts.InstallDir); err != nil {
		return err
	}

	dir, cleanup, err := TempDir("firefox")
	if err != nil {
		return err
	}
	defer cleanup()

	downloads, key := i.firefoxDownloadDir(dir, i.firefoxCacheKey(version, opts))
	archive, err := i.fetchAndVerifyFirefox(ctx, downloads, release, key)
	if err != nil {
		return err
	}

	if err := i.unpackFirefox(ctx, dir, archive, release, opts.InstallDir); err != nil {
		return err
	}

	installed, err := i.verifyFirefox(ctx, version)
	if err != nil {
		return err
	}
	i.logf("installed Firefox %s", installed)
	RecordVersion(i.Out, "Firefox", installed)

	if key != "" {
		i.Cache.Save(ctx, key, []string{downloads})
	}
	return nil
}

func (i *Installer) firefoxSupported() error {
	if i.Platform.Distro == DistroAlpine {
		return Unsupported("Firefox", i.Platform, "Mozilla publishes no Alpine build")
	}
	if i.Platform.IsLinuxARM64() {
		return Unsupported("Firefox", i.Platform, "only linux-x86_64 builds are published")
	}
	return nil
}

// firefoxPrivilege checks what the install will need before anything is
// downloaded. On Linux the dependencies come from the package manager; on macOS
// brew runs as the job user, so only the directories have to be writable.
func (i *Installer) firefoxPrivilege(installDir string) error {
	if i.Platform.IsMacOS() {
		return i.requireWritable("Firefox", "/Applications", installDir, usrLocalBin)
	}
	return i.requirePrivilege("Firefox", "run "+i.packageManager())
}

func (i *Installer) resolveFirefoxVersion(ctx context.Context, requested string) (string, error) {
	if requested != Latest {
		return requested, nil
	}

	var versions struct {
		Latest string `json:"LATEST_FIREFOX_VERSION"`
	}
	if err := i.Fetcher.JSON(ctx, i.Endpoints.MozillaProduct+"/firefox_versions.json", &versions); err != nil {
		return "", err
	}
	if versions.Latest == "" {
		return "", errors.New("Mozilla reported no latest Firefox version") //nolint:staticcheck // proper noun
	}
	return versions.Latest, nil
}

// firefoxRelease works out the platform directory, archive name and URL. The
// archive is saved under its own name rather than the orb's flattened
// "linux-x86_64-en-US-firefox-149.0.tar.xz"; the manifest is then looked up by
// full path, which is unambiguous and needs none of the orb's perl rewriting.
func (i *Installer) firefoxRelease(version string) (firefoxRelease, error) {
	major, err := majorVersion(version)
	if err != nil {
		return firefoxRelease{}, err
	}

	rel := firefoxRelease{Version: version}
	if i.Platform.IsMacOS() {
		rel.Platform = "mac"
		rel.Filename = "Firefox " + version + ".dmg"
	} else {
		rel.Platform = "linux-x86_64"
		ext := ".tar.bz2"
		if major >= firefoxXZFrom {
			ext = ".tar.xz"
		}
		rel.Filename = "firefox-" + version + ext
	}

	rel.ManifestEntry = rel.Platform + "/en-US/" + rel.Filename
	rel.URL = fmt.Sprintf("%s/%s/%s/en-US/%s",
		i.Endpoints.MozillaArchive, version, rel.Platform, url.PathEscape(rel.Filename))
	return rel, nil
}

func (i *Installer) firefoxBaseURL(version string) string {
	return i.Endpoints.MozillaArchive + "/" + version
}

// firefoxCacheKey returns "" when caching does not apply. Caching is skipped for
// "latest" because a key containing that literal would pin the first version it
// ever resolved.
func (i *Installer) firefoxCacheKey(version string, opts FirefoxOptions) string {
	if !opts.UseCache || opts.Version == Latest {
		return ""
	}
	return FirefoxCacheKey(i.Host, opts.CacheKey, version)
}

// prepareFirefoxHost checks the install has the privilege it needs, before
// anything is downloaded, and then installs Firefox's dependencies.
func (i *Installer) prepareFirefoxHost(ctx context.Context, installDir string) error {
	if err := i.firefoxPrivilege(installDir); err != nil {
		return err
	}
	return i.installFirefoxDependencies(ctx)
}

func (i *Installer) installFirefoxDependencies(ctx context.Context) error {
	switch i.Platform.Distro {
	case DistroMacOS:
		// gpg verifies the checksum manifest; coreutils is what the orb pulls in
		// alongside it.
		_, err := i.Runner.Run(ctx, Command{
			Name:         "brew",
			Args:         []string{"install", "gnupg", "coreutils"},
			Env:          []string{"HOMEBREW_NO_AUTO_UPDATE=1"},
			Unprivileged: true,
			Quiet:        true,
		})
		return err
	case DistroRHEL:
		packages := i.withGPG("gnupg2",
			"alsa-lib", "bzip2", "dbus-glib-devel", "gtk2-devel", "gtk3-devel", "libXt-devel", "perl", "which")
		return i.Runner.Exec(ctx, "yum", append([]string{"install", "-y"}, packages...)...)
	case DistroDebian:
		if err := i.Runner.AptUpdate(ctx); err != nil {
			return err
		}
		return i.aptInstall(ctx, i.withGPG("gnupg", "libasound-dev", "libxt6", "libgtk-3-dev", "libdbus-glib-1-2")...)
	default:
		return Unsupported("Firefox", i.Platform, "")
	}
}

// withGPG adds gpgPackage to packages when gpg is not on PATH. The download is
// signature-checked with gpg, which minimal Debian and RHEL images lack.
// Installing it only when missing leaves an image's own gpg package alone.
func (i *Installer) withGPG(gpgPackage string, packages ...string) []string {
	if i.Host.has("gpg") {
		return packages
	}
	return append(packages, gpgPackage)
}

// firefoxKeyFile is the local name of Mozilla's release signing key.
const firefoxKeyFile = "mozilla.key"

// firefoxManifests are the checksum manifests a release may publish, in the
// order they are tried. SHA256SUMS comes first with SHA512SUMS as a fallback,
// matching the orb, since older releases publish only one of them.
var firefoxManifests = []string{"SHA256SUMS", "SHA512SUMS"}

// firefoxDownloadDir picks where the release is downloaded to. With caching on
// that is the cache directory for key, so the entry is saved from, and restored
// to, the same path in every pipeline; otherwise it is tmp, which is removed
// afterwards. A cache directory that cannot be created turns caching off rather
// than failing the install, so the key comes back empty then.
func (i *Installer) firefoxDownloadDir(tmp, key string) (dir, cacheKey string) {
	if key == "" {
		return tmp, ""
	}
	dir, err := cacheDir(key)
	if err == nil {
		err = os.MkdirAll(dir, 0o750)
	}
	if err != nil {
		i.logf("not caching Firefox: %v", err)
		return tmp, ""
	}
	return dir, key
}

// fetchAndVerifyFirefox returns the path of a verified archive in dir.
// Verification is GPG over the checksum manifest, then the manifest's digest
// over the archive.
//
// With a cache key, dir is first restored from the job cache. The entry holds
// the signing key, manifest and signature alongside the archive, so a hit is
// verified exactly as a fresh download is without contacting Mozilla, which is
// what makes --use-cache useful when Mozilla is unreachable. An entry that is
// incomplete or fails verification is discarded and the release downloaded
// again: a bad cache must not fail an install that would otherwise succeed.
func (i *Installer) fetchAndVerifyFirefox(ctx context.Context, dir string, rel firefoxRelease, cacheKey string) (string, error) {
	if cacheKey != "" {
		i.Cache.Restore(ctx, cacheKey, dir)
		archive, err := i.verifyCachedFirefox(ctx, dir, rel)
		if err == nil {
			i.logf("using Firefox %s from the job cache", rel.Version)
			return archive, nil
		}
		if hasContent(dir) {
			i.logf("discarding cached Firefox %s and downloading it again: %v", rel.Version, err)
			if err := emptyDir(dir); err != nil {
				return "", err
			}
		}
	}

	archive, err := i.Fetcher.Download(ctx, rel.URL, dir, rel.Filename)
	if err != nil {
		return "", err
	}
	manifest, err := i.fetchVerifiedSums(ctx, dir, rel.Version)
	if err != nil {
		return "", err
	}
	if err := VerifySums(manifest, rel.ManifestEntry, archive); err != nil {
		return "", err
	}
	return archive, nil
}

// verifyCachedFirefox verifies an archive already in dir against the signing
// key, manifest and signature beside it, fetching nothing. Any of them missing
// is an error, as is a manifest whose signature does not verify.
func (i *Installer) verifyCachedFirefox(ctx context.Context, dir string, rel firefoxRelease) (string, error) {
	archive := filepath.Join(dir, rel.Filename)
	key := filepath.Join(dir, firefoxKeyFile)
	if !hasContent(archive) || !hasContent(key) {
		return "", errors.New("no cached archive and signing key")
	}

	for _, name := range firefoxManifests {
		manifest := filepath.Join(dir, name)
		sig := manifest + ".asc"
		if !hasContent(manifest) || !hasContent(sig) {
			continue
		}
		if err := VerifyGPG(ctx, i.Runner, i.Host, key, sig, manifest); err != nil {
			return "", err
		}
		if err := VerifySums(manifest, rel.ManifestEntry, archive); err != nil {
			return "", err
		}
		return archive, nil
	}
	return "", errors.New("no cached checksum manifest with its signature")
}

// emptyDir removes everything in dir but dir itself, so nothing from a
// discarded cache entry is mistaken for part of the download that replaces it.
func emptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return fmt.Errorf("remove %s: %w", filepath.Join(dir, e.Name()), err)
		}
	}
	return nil
}

// fetchVerifiedSums downloads the checksum manifest and its signature, verifies
// the signature against Mozilla's release key, and returns the manifest path.
// The manifests are tried in firefoxManifests order.
func (i *Installer) fetchVerifiedSums(ctx context.Context, dir, version string) (string, error) {
	base := i.firefoxBaseURL(version)

	key, err := i.Fetcher.Download(ctx, base+"/KEY", dir, firefoxKeyFile)
	if err != nil {
		return "", err
	}

	var lastErr error
	for _, name := range firefoxManifests {
		manifest, err := i.Fetcher.Download(ctx, base+"/"+name, dir, name)
		if err != nil {
			lastErr = err
			continue
		}
		sig, err := i.Fetcher.Download(ctx, base+"/"+name+".asc", dir, name+".asc")
		if err != nil {
			lastErr = err
			continue
		}
		if err := VerifyGPG(ctx, i.Runner, i.Host, key, sig, manifest); err != nil {
			return "", err
		}
		return manifest, nil
	}
	return "", fmt.Errorf("no checksum manifest available for Firefox %s: %w", version, lastErr)
}

func (i *Installer) unpackFirefox(ctx context.Context, dir, archive string, rel firefoxRelease, installDir string) error {
	if i.Platform.IsMacOS() {
		return i.installFirefoxMacOS(ctx, dir, archive, installDir)
	}
	return i.installFirefoxLinux(ctx, dir, archive, rel.Version, installDir)
}

func (i *Installer) installFirefoxLinux(ctx context.Context, dir, archive, version, installDir string) error {
	extracted := filepath.Join(dir, "extracted")
	if err := mkdirAll(extracted); err != nil {
		return err
	}
	if err := Extract(archive, extracted); err != nil {
		return err
	}

	versioned := filepath.Join(installDir, "firefox-"+version)
	_ = i.Runner.Exec(ctx, "rm", "-rf", versioned)
	if err := i.Runner.Exec(ctx, "cp", "-R", filepath.Join(extracted, "firefox"), versioned); err != nil {
		return err
	}
	// The tree is copied in as root and then run by the job user, so it has to be
	// traversable regardless of the modes it arrived with.
	if err := i.Runner.Exec(ctx, "chmod", "-R", "a+rX", versioned); err != nil {
		return err
	}
	if err := i.Runner.Exec(ctx, "chmod", "+x", filepath.Join(versioned, "firefox")); err != nil {
		return err
	}
	return i.linkFirefox(ctx, filepath.Join(versioned, "firefox"), installDir)
}

func (i *Installer) installFirefoxMacOS(ctx context.Context, dir, archive, installDir string) error {
	mount := filepath.Join(dir, "mount")
	if err := mkdirAll(mount); err != nil {
		return err
	}

	if err := i.Runner.Exec(ctx, "hdiutil", "attach", "-nobrowse", "-mountpoint", mount, archive); err != nil {
		return err
	}
	defer func() { _ = i.Runner.Exec(ctx, "hdiutil", "detach", mount) }()

	if err := i.Runner.Exec(ctx, "cp", "-R", filepath.Join(mount, "Firefox.app"), "/Applications/"); err != nil {
		return err
	}

	wrapper := filepath.Join(dir, "firefox")
	script := "#!/usr/bin/env bash\nexec /Applications/Firefox.app/Contents/MacOS/firefox \"$@\"\n"
	if err := writeFile(wrapper, strings.NewReader(script), 0o755); err != nil {
		return err
	}
	return i.linkFirefox(ctx, wrapper, installDir)
}

// linkFirefox puts firefox both in installDir and in /usr/local/bin. The orb
// installed into install_dir but hardcoded the symlink to /usr/local/bin, so a
// custom directory produced a binary that was not reachable from it. Both are
// created; when they are the same directory only one is.
func (i *Installer) linkFirefox(ctx context.Context, source, installDir string) error {
	targets := []string{filepath.Join(installDir, "firefox")}
	if filepath.Clean(installDir) != usrLocalBin {
		targets = append(targets, filepath.Join(usrLocalBin, "firefox"))
	}

	for _, target := range targets {
		if err := i.Runner.Exec(ctx, "ln", "-sf", source, target); err != nil {
			return err
		}
	}
	return nil
}

func (i *Installer) installedFirefoxVersion(ctx context.Context) (string, bool) {
	if !i.Host.has("firefox") {
		return "", false
	}
	banner, err := i.Runner.Output(ctx, "firefox", "--version")
	if err != nil {
		return "", false
	}
	v := ParseBrowserVersion(banner)
	return v, v != ""
}

// verifyFirefox accepts an ESR release whose banner drops the suffix, which is
// what the orb's `echo "$(firefox --version)esr"` comparison was working around.
func (i *Installer) verifyFirefox(ctx context.Context, version string) (string, error) {
	installed, found := i.installedFirefoxVersion(ctx)
	if !found {
		return "", fmt.Errorf("no working firefox on PATH after installing %s", version)
	}
	if installed == version || installed+"esr" == version {
		return installed, nil
	}
	return "", fmt.Errorf("installed Firefox is %s, but %s was requested", installed, version)
}
