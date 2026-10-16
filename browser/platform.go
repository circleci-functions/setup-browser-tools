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

// Distro identifies a host's package-management family, which is what decides
// how a browser gets installed.
type Distro string

const (
	DistroMacOS  Distro = "macos"
	DistroAlpine Distro = "alpine"
	DistroRHEL   Distro = "rhel"
	DistroDebian Distro = "debian"
)

const (
	ArchAMD64 = "amd64"
	ArchARM64 = "arm64"
)

type Platform struct {
	Distro Distro
	Arch   string
}

func (p Platform) IsMacOS() bool { return p.Distro == DistroMacOS }

// IsLinuxARM64 reports whether p is 64-bit ARM Linux, for which several browser
// vendors publish no build at all.
func (p Platform) IsLinuxARM64() bool {
	return p.Distro != DistroMacOS && p.Arch == ArchARM64
}

// String returns e.g. "debian/amd64".
func (p Platform) String() string { return string(p.Distro) + "/" + p.Arch }

var ErrUnknownPlatform = errors.New("unrecognised platform: not macOS, and no apk, yum or apt-get found")

// UnsupportedError is a distinct type so that a caller installing a set of
// components can tell "no install path exists here" apart from a genuine
// failure, and skip rather than abort.
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

func Unsupported(component string, p Platform, reason string) error {
	return &UnsupportedError{Component: component, Platform: p, Reason: reason}
}

func IsUnsupported(err error) bool {
	var target *UnsupportedError
	return errors.As(err, &target)
}

// Host supplies the environment facts Detect and ResolvePrivilege read. The
// zero value reads the real machine; tests fill the fields to describe a
// synthetic one.
type Host struct {
	GOOS     string
	GOARCH   string
	Root     string
	LookPath func(string) (string, error)
	Geteuid  func() int
	Writable func(dir string) bool
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
	return exec.LookPath(name) //nolint:wrapcheck // callers only test for nil
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

// writable reports whether the current user can create files in dir. It tries
// to, rather than reading permission bits, so a read-only mount or an ACL
// counts as well.
func (h Host) writable(dir string) bool {
	if h.Writable != nil {
		return h.Writable(dir)
	}
	f, err := os.CreateTemp(dir, ".setup-browser-tools-")
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return true
}

// Detect classifies the host, testing families in the order macOS, Alpine,
// RHEL, Debian, so a host carrying both yum and apt-get resolves as RHEL. Where
// the orb falls through to apt-get for any unrecognised Linux, this returns
// ErrUnknownPlatform.
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

func (h Host) isAlpine() bool {
	issue, err := os.ReadFile(filepath.Join(h.root(), "etc", "issue"))
	if err != nil {
		return false
	}
	return strings.Contains(string(issue), "Alpine")
}

type Privilege struct {
	root     bool
	haveSudo bool
}

// ResolvePrivilege decides once how privileged commands are invoked. Unlike the
// orb's per-script `if [[ $EUID == 0 ]]`, sudo is used only when it is both
// needed and actually installed, so a root container without sudo never gets an
// invocation it cannot run.
func ResolvePrivilege(h Host) Privilege {
	if h.geteuid() == 0 {
		return Privilege{root: true, haveSudo: false}
	}
	return Privilege{root: false, haveSudo: h.has("sudo")}
}

// Escalated reports whether privileged commands can actually succeed.
func (p Privilege) Escalated() bool { return p.root || p.haveSudo }

func (p Privilege) NeedsSudo() bool { return !p.root && p.haveSudo }

func (p Privilege) Command(name string, args ...string) (string, []string) {
	if !p.NeedsSudo() {
		return name, args
	}
	return "sudo", append([]string{name}, args...)
}
