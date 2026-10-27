package browser

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

// fakePath resolves binaries as a PATH holding present would. A bare name is a
// binary in /usr/bin; an absolute path is that binary, in a directory on PATH.
// Each resolves by name or by its full path, and for a name in more than one
// directory the first wins, as on PATH.
func fakePath(present ...string) func(string) (string, error) {
	byName := make(map[string]string, len(present))
	files := make(map[string]bool, len(present))
	for _, p := range present {
		if !filepath.IsAbs(p) {
			p = filepath.Join("/usr/bin", p)
		}
		files[p] = true
		if _, seen := byName[filepath.Base(p)]; !seen {
			byName[filepath.Base(p)] = p
		}
	}
	return func(name string) (string, error) {
		if filepath.IsAbs(name) && files[name] {
			return name, nil
		}
		if p, ok := byName[name]; ok {
			return p, nil
		}
		return "", fmt.Errorf("%s: not found", name)
	}
}

func issueRoot(t *testing.T, contents string) string {
	t.Helper()
	root := t.TempDir()
	if contents == "" {
		return root
	}
	etc := filepath.Join(root, "etc")
	assert.NilError(t, os.MkdirAll(etc, 0o750))
	assert.NilError(t, os.WriteFile(filepath.Join(etc, "issue"), []byte(contents), 0o600))
	return root
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name       string
		goos       string
		goarch     string
		issue      string
		onPath     []string
		wantDistro Distro
		wantErr    error
	}{
		{
			name:       "macOS arm64 wins before any file or PATH check",
			goos:       "darwin",
			goarch:     ArchARM64,
			issue:      "Alpine Linux",
			onPath:     []string{"yum", "apt-get"},
			wantDistro: DistroMacOS,
		},
		{
			name:       "macOS amd64",
			goos:       "darwin",
			goarch:     ArchAMD64,
			wantDistro: DistroMacOS,
		},
		{
			name:       "Alpine from etc/issue",
			goos:       "linux",
			goarch:     ArchAMD64,
			issue:      "Welcome to Alpine Linux 3.20\\nKernel \\r on an \\m (\\l)\\n",
			onPath:     []string{"apk"},
			wantDistro: DistroAlpine,
		},
		{
			name:       "Alpine is checked before yum and apt-get",
			goos:       "linux",
			goarch:     ArchAMD64,
			issue:      "Alpine Linux",
			onPath:     []string{"yum", "apt-get"},
			wantDistro: DistroAlpine,
		},
		{
			name:       "RHEL from yum",
			goos:       "linux",
			goarch:     ArchAMD64,
			issue:      "\\S\\nKernel \\r on an \\m\\n",
			onPath:     []string{"yum"},
			wantDistro: DistroRHEL,
		},
		{
			name:       "yum wins over apt-get",
			goos:       "linux",
			goarch:     ArchAMD64,
			onPath:     []string{"yum", "apt-get"},
			wantDistro: DistroRHEL,
		},
		{
			name:       "Debian from apt-get",
			goos:       "linux",
			goarch:     ArchAMD64,
			issue:      "Debian GNU/Linux 12 \\n \\l\\n",
			onPath:     []string{"apt-get"},
			wantDistro: DistroDebian,
		},
		{
			name:       "Debian arm64",
			goos:       "linux",
			goarch:     ArchARM64,
			onPath:     []string{"apt-get"},
			wantDistro: DistroDebian,
		},
		{
			name:    "no package manager at all",
			goos:    "linux",
			goarch:  ArchAMD64,
			wantErr: ErrUnknownPlatform,
		},
		{
			name:    "etc/issue naming another distro is not Alpine",
			goos:    "linux",
			goarch:  ArchAMD64,
			issue:   "Ubuntu 24.04 LTS",
			wantErr: ErrUnknownPlatform,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Detect(Host{
				GOOS:     tc.goos,
				GOARCH:   tc.goarch,
				Root:     issueRoot(t, tc.issue),
				LookPath: fakePath(tc.onPath...),
			})

			if tc.wantErr != nil {
				assert.Assert(t, errors.Is(err, tc.wantErr), "got %v, want %v", err, tc.wantErr)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, got.Distro, tc.wantDistro)
			assert.Equal(t, got.Arch, tc.goarch)
		})
	}
}

func TestDetectDefaultsToRealHost(t *testing.T) {
	got, err := Detect(Host{})
	// The test host is macOS or Linux with a package manager, so this must
	// classify; the point is that the zero Host reads the real machine rather
	// than panicking on nil function fields.
	assert.NilError(t, err)
	assert.Assert(t, got.Distro != "")
	assert.Assert(t, got.Arch != "")
}

func TestPlatformPredicates(t *testing.T) {
	tests := []struct {
		name           string
		platform       Platform
		wantMacOS      bool
		wantLinuxARM64 bool
		wantString     string
	}{
		{
			name:           "macOS arm64 is not Linux ARM64",
			platform:       Platform{Distro: DistroMacOS, Arch: ArchARM64},
			wantMacOS:      true,
			wantLinuxARM64: false,
			wantString:     "macos/arm64",
		},
		{
			name:           "Debian arm64",
			platform:       Platform{Distro: DistroDebian, Arch: ArchARM64},
			wantMacOS:      false,
			wantLinuxARM64: true,
			wantString:     "debian/arm64",
		},
		{
			name:           "Debian amd64",
			platform:       Platform{Distro: DistroDebian, Arch: ArchAMD64},
			wantMacOS:      false,
			wantLinuxARM64: false,
			wantString:     "debian/amd64",
		},
		{
			name:           "Alpine arm64",
			platform:       Platform{Distro: DistroAlpine, Arch: ArchARM64},
			wantMacOS:      false,
			wantLinuxARM64: true,
			wantString:     "alpine/arm64",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.platform.IsMacOS(), tc.wantMacOS)
			assert.Equal(t, tc.platform.IsLinuxARM64(), tc.wantLinuxARM64)
			assert.Equal(t, tc.platform.String(), tc.wantString)
		})
	}
}

func TestUnsupportedError(t *testing.T) {
	p := Platform{Distro: DistroAlpine, Arch: ArchAMD64}

	t.Run("message includes component, platform and reason", func(t *testing.T) {
		err := Unsupported("Firefox", p, "Mozilla publishes no Alpine build")
		assert.Error(t, err, "Firefox is not supported on alpine/amd64: Mozilla publishes no Alpine build")
	})

	t.Run("reason is optional", func(t *testing.T) {
		err := Unsupported("Edge", p, "")
		assert.Error(t, err, "Edge is not supported on alpine/amd64")
	})

	t.Run("recognised through IsUnsupported", func(t *testing.T) {
		assert.Assert(t, IsUnsupported(Unsupported("Edge", p, "")))
	})

	t.Run("recognised when wrapped", func(t *testing.T) {
		wrapped := fmt.Errorf("install edge: %w", Unsupported("Edge", p, ""))
		assert.Assert(t, IsUnsupported(wrapped))
	})

	t.Run("an ordinary error is not unsupported", func(t *testing.T) {
		assert.Assert(t, !IsUnsupported(errors.New("download failed")))
		assert.Assert(t, !IsUnsupported(nil))
	})

	t.Run("component and platform are inspectable without string matching", func(t *testing.T) {
		var target *UnsupportedError
		assert.Assert(t, errors.As(Unsupported("Edge", p, ""), &target))
		assert.Equal(t, target.Component, "Edge")
		assert.Equal(t, target.Platform.Distro, DistroAlpine)
	})
}

func TestResolvePrivilege(t *testing.T) {
	tests := []struct {
		name          string
		euid          int
		onPath        []string
		wantEscalated bool
		wantSudo      bool
		wantName      string
		wantArgs      []string
	}{
		{
			name:          "root with sudo present runs unprefixed",
			euid:          0,
			onPath:        []string{"sudo"},
			wantEscalated: true,
			wantSudo:      false,
			wantName:      "apt-get",
			wantArgs:      []string{"update"},
		},
		{
			name:          "root without sudo runs unprefixed",
			euid:          0,
			wantEscalated: true,
			wantSudo:      false,
			wantName:      "apt-get",
			wantArgs:      []string{"update"},
		},
		{
			name:          "non-root with sudo is prefixed",
			euid:          1000,
			onPath:        []string{"sudo"},
			wantEscalated: true,
			wantSudo:      true,
			wantName:      "sudo",
			wantArgs:      []string{"apt-get", "update"},
		},
		{
			name:          "non-root without sudo emits no bare sudo",
			euid:          1000,
			wantEscalated: false,
			wantSudo:      false,
			wantName:      "apt-get",
			wantArgs:      []string{"update"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := ResolvePrivilege(Host{
				Geteuid:  func() int { return tc.euid },
				LookPath: fakePath(tc.onPath...),
			})

			assert.Equal(t, p.Escalated(), tc.wantEscalated)
			assert.Equal(t, p.NeedsSudo(), tc.wantSudo)

			name, args := p.Command("apt-get", "update")
			assert.Equal(t, name, tc.wantName)
			assert.DeepEqual(t, args, tc.wantArgs)
		})
	}
}

func TestPrivilegeCommandDoesNotAliasCallerArgs(t *testing.T) {
	p := ResolvePrivilege(Host{
		Geteuid:  func() int { return 1000 },
		LookPath: fakePath("sudo"),
	})

	// A shared backing array would let one prefixed call corrupt the next.
	args := []string{"install", "-y", "google-chrome-stable"}
	_, first := p.Command("apt-get", args...)
	_, second := p.Command("apt-get", args...)

	assert.DeepEqual(t, first, []string{"apt-get", "install", "-y", "google-chrome-stable"})
	assert.DeepEqual(t, second, []string{"apt-get", "install", "-y", "google-chrome-stable"})
	assert.DeepEqual(t, args, []string{"install", "-y", "google-chrome-stable"})
}
