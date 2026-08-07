// Package browser installs browsers and browser-testing tools onto a host.
package browser

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Distro identifies the package-management family of a host, which is what
// determines how a browser is installed.
type Distro string

const (
	DistroMacOS  Distro = "macos"
	DistroAlpine Distro = "alpine"
	DistroRHEL   Distro = "rhel"
	DistroDebian Distro = "debian"
)

// Architecture values, as reported by Platform.Arch.
const (
	ArchAMD64 = "amd64"
	ArchARM64 = "arm64"
)

// Platform describes the host a browser is being installed onto.
type Platform struct {
	Distro Distro
	Arch   string
}

// IsMacOS reports whether p is macOS.
func (p Platform) IsMacOS() bool { return p.Distro == DistroMacOS }

// IsLinuxARM64 reports whether p is 64-bit ARM Linux, which several browser
// vendors publish no build for.
func (p Platform) IsLinuxARM64() bool {
	return p.Distro != DistroMacOS && p.Arch == ArchARM64
}

// String returns the distro and architecture, e.g. "debian/amd64".
func (p Platform) String() string { return string(p.Distro) + "/" + p.Arch }

// ErrUnknownPlatform is returned by Detect when the host matches none of the
// supported package-management families.
var ErrUnknownPlatform = errors.New("unrecognised platform: not macOS, and no apk, yum or apt-get found")

// UnsupportedError reports that a component has no installation path on a
// platform. It is returned rather than a plain error so that callers installing
// a set of components can tell an unsupported platform apart from a genuine
// failure and decide whether to skip or fail.
type UnsupportedError struct {
	Component string
	Platform  Platform
	Reason    string
}

func (e *UnsupportedError) Error() string {
	msg := fmt.Sprintf("%s is not supported on %s", e.Component, e.Platform)
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	return msg
}

// Unsupported returns an UnsupportedError for component on p. reason may be
// empty.
func Unsupported(component string, p Platform, reason string) error {
	return &UnsupportedError{Component: component, Platform: p, Reason: reason}
}

// IsUnsupported reports whether err is or wraps an UnsupportedError.
func IsUnsupported(err error) bool {
	var target *UnsupportedError
	return errors.As(err, &target)
}

// Host supplies the environment facts that Detect and ResolvePrivilege read.
// A zero Host reads the real machine; tests set the fields to describe a
// synthetic one.
type Host struct {
	// GOOS defaults to runtime.GOOS.
	GOOS string
	// GOARCH defaults to runtime.GOARCH.
	GOARCH string
	// Root is the filesystem root under which /etc/issue is read. Defaults
	// to "/".
	Root string
	// LookPath defaults to exec.LookPath.
	LookPath func(string) (string, error)
	// Geteuid defaults to os.Geteuid.
	Geteuid func() int
}

func (h Host) goos() string {
	if h.GOOS != "" {
		return h.GOOS
	}
	return runtime.GOOS
}

func (h Host) goarch() string {
	if h.GOARCH != "" {
		return h.GOARCH
	}
	return runtime.GOARCH
}

func (h Host) root() string {
	if h.Root != "" {
		return h.Root
	}
	return string(filepath.Separator)
}

func (h Host) lookPath(name string) (string, error) {
	if h.LookPath != nil {
		return h.LookPath(name)
	}
	return exec.LookPath(name) //nolint:wrapcheck // returned as-is; callers only test for nil
}

func (h Host) geteuid() int {
	if h.Geteuid != nil {
		return h.Geteuid()
	}
	return os.Geteuid()
}

func (h Host) has(name string) bool {
	_, err := h.lookPath(name)
	return err == nil
}

// Detect classifies the host. Families are tested in the order macOS, Alpine,
// RHEL, Debian, so a host with both yum and apt-get is treated as RHEL, and
// returns ErrUnknownPlatform when none match.
func Detect(h Host) (Platform, error) {
	arch := h.goarch()

	if h.goos() == "darwin" {
		return Platform{Distro: DistroMacOS, Arch: arch}, nil
	}

	switch {
	case h.isAlpine():
		return Platform{Distro: DistroAlpine, Arch: arch}, nil
	case h.has("yum"):
		return Platform{Distro: DistroRHEL, Arch: arch}, nil
	case h.has("apt-get"):
		return Platform{Distro: DistroDebian, Arch: arch}, nil
	}

	return Platform{}, ErrUnknownPlatform
}

// isAlpine reports whether /etc/issue names Alpine, which is how Alpine is
// distinguished from other Linux distributions.
func (h Host) isAlpine() bool {
	issue, err := os.ReadFile(filepath.Join(h.root(), "etc", "issue"))
	if err != nil {
		return false
	}
	return strings.Contains(string(issue), "Alpine")
}

// Privilege knows whether commands that write to system directories need a
// sudo prefix.
type Privilege struct {
	root     bool
	haveSudo bool
}

// ResolvePrivilege decides once how privileged commands must be invoked:
// unprefixed when already root, prefixed with sudo when not root and sudo is
// on PATH. When neither holds, commands run unprefixed and Escalated reports
// false, so callers can explain a permission failure rather than emitting a
// sudo invocation that cannot work.
func ResolvePrivilege(h Host) Privilege {
	if h.geteuid() == 0 {
		return Privilege{root: true, haveSudo: false}
	}
	return Privilege{root: false, haveSudo: h.has("sudo")}
}

// Escalated reports whether privileged commands can actually succeed, i.e. the
// process is root or sudo is available.
func (p Privilege) Escalated() bool { return p.root || p.haveSudo }

// NeedsSudo reports whether commands are prefixed with sudo.
func (p Privilege) NeedsSudo() bool { return !p.root && p.haveSudo }

// Command returns the name and arguments to execute in order to run name with
// args as root. As root, or when sudo is unavailable, it returns them
// unchanged; otherwise it prepends sudo. It never returns a bare "sudo" that
// is not on PATH.
func (p Privilege) Command(name string, args ...string) (string, []string) {
	if !p.NeedsSudo() {
		return name, args
	}
	return "sudo", append([]string{name}, args...)
}
