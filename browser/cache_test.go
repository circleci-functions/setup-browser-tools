package browser

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"gotest.tools/v3/assert"
)

func linuxHost() Host { return Host{GOOS: "linux", GOARCH: ArchAMD64} }

func TestCacheKeys(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "chrome default",
			got:  ChromeCacheKey(linuxHost(), "v1", "stable", "147.0.7727.55"),
			want: "setup-browser-tools-chrome-v1-stable-147.0.7727.55-linux-amd64",
		},
		{
			name: "chrome beta differs from stable at the same version",
			got:  ChromeCacheKey(linuxHost(), "v1", "beta", "147.0.7727.55"),
			want: "setup-browser-tools-chrome-v1-beta-147.0.7727.55-linux-amd64",
		},
		{
			name: "chrome honours a custom cache key",
			got:  ChromeCacheKey(linuxHost(), "v2", "stable", Latest),
			want: "setup-browser-tools-chrome-v2-stable-latest-linux-amd64",
		},
		{
			name: "chrome varies by arch",
			got:  ChromeCacheKey(Host{GOOS: "darwin", GOARCH: ArchARM64}, "v1", "stable", Latest),
			want: "setup-browser-tools-chrome-v1-stable-latest-darwin-arm64",
		},
		{
			name: "firefox has no channel",
			got:  FirefoxCacheKey(linuxHost(), "v1", "149.0.2"),
			want: "setup-browser-tools-firefox-v1-149.0.2-linux-amd64",
		},
		{
			name: "firefox varies by arch",
			got:  FirefoxCacheKey(Host{GOOS: "darwin", GOARCH: ArchARM64}, "v1", "149.0.2"),
			want: "setup-browser-tools-firefox-v1-149.0.2-darwin-arm64",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.got, tc.want)
		})
	}
}

func TestChromeCacheKeySeparatesChannels(t *testing.T) {
	// The orb's key omits the channel, so these two collided on one entry.
	stable := ChromeCacheKey(linuxHost(), "v1", "stable", "147.0.7727.55")
	beta := ChromeCacheKey(linuxHost(), "v1", "beta", "147.0.7727.55")
	assert.Assert(t, stable != beta)
}

func TestCacheRoundTrip(t *testing.T) {
	srv := sdktest.NewServer(t)
	cache := Cache{Client: srv.Client(), Out: io.Discard}
	ctx := context.Background()

	const key = "setup-browser-tools-chrome-v1-stable-147.0.7727.55-linux-amd64"

	installed := filepath.Join(t.TempDir(), "chrome")
	assert.NilError(t, os.MkdirAll(installed, 0o750))
	assert.NilError(t, os.WriteFile(filepath.Join(installed, "google-chrome"), []byte("binary\n"), 0o600))

	cache.Save(ctx, key, []string{installed})

	assert.NilError(t, os.RemoveAll(installed))
	assert.Equal(t, hasContent(installed), false)

	assert.Assert(t, cache.Restore(ctx, key, installed), "restore should report a hit")

	got, err := os.ReadFile(filepath.Join(installed, "google-chrome"))
	assert.NilError(t, err)
	assert.Equal(t, string(got), "binary\n")
}

func TestCacheRestoreMissIsNotAHit(t *testing.T) {
	srv := sdktest.NewServer(t)
	cache := Cache{Client: srv.Client(), Out: io.Discard}

	path := filepath.Join(t.TempDir(), "chrome")
	assert.Equal(t, cache.Restore(context.Background(), "never-saved", path), false)
}

func TestCacheOutsideAJobStillProceeds(t *testing.T) {
	// A nil client is what running outside a CircleCI job looks like. Neither
	// call may panic or fail the install.
	cache := Cache{Client: nil, Out: io.Discard}
	ctx := context.Background()

	assert.Equal(t, cache.Restore(ctx, "key", t.TempDir()), false)
	cache.Save(ctx, "key", []string{t.TempDir()})
}

func TestCacheSaveSkipsAbsentPaths(t *testing.T) {
	srv := sdktest.NewServer(t)
	cache := Cache{Client: srv.Client(), Out: io.Discard}

	// Saving a path that does not exist would otherwise store an empty entry
	// that later masquerades as a hit.
	cache.Save(context.Background(), "key", []string{filepath.Join(t.TempDir(), "absent")})
	assert.Equal(t, cache.Restore(context.Background(), "key", t.TempDir()), false)
}

func TestHasContent(t *testing.T) {
	dir := t.TempDir()

	empty := filepath.Join(dir, "empty")
	assert.NilError(t, os.WriteFile(empty, nil, 0o600))

	full := filepath.Join(dir, "full")
	assert.NilError(t, os.WriteFile(full, []byte("x"), 0o600))

	emptyDir := filepath.Join(dir, "emptydir")
	assert.NilError(t, os.MkdirAll(emptyDir, 0o750))

	fullDir := filepath.Join(dir, "fulldir")
	assert.NilError(t, os.MkdirAll(fullDir, 0o750))
	assert.NilError(t, os.WriteFile(filepath.Join(fullDir, "f"), []byte("x"), 0o600))

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "missing path", path: filepath.Join(dir, "nope"), want: false},
		{name: "empty file", path: empty, want: false},
		{name: "non-empty file", path: full, want: true},
		{name: "empty directory", path: emptyDir, want: false},
		{name: "non-empty directory", path: fullDir, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, hasContent(tc.path), tc.want)
		})
	}
}
