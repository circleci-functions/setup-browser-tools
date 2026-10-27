package browser

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/circleci-functions/toolkit/sdk"
)

const cacheKeyPrefix = "setup-browser-tools"

// ChromeCacheKey includes the release channel, which the orb's key omits — so a
// beta install could restore a stable entry at the same version, and vice versa.
func ChromeCacheKey(h Host, cacheKey, channel, version string) string {
	return fmt.Sprintf("%s-chrome-%s-%s-%s-%s-%s",
		cacheKeyPrefix, cacheKey, channel, version, h.goos(), h.goarch())
}

// FirefoxCacheKey carries a "release" token that the --cache-key value follows.
// Up to v0.4.0 an entry under setup-browser-tools-firefox-<cache-key>-... held
// the archive at a temp path, which no later run looks in. Keys are write-once
// and a restore matches them as a prefix, so reusing that shape would leave an
// upgraded project restoring the old entry and never saving a usable one. The
// token keeps the new key from matching any old one.
func FirefoxCacheKey(h Host, cacheKey, version string) string {
	return fmt.Sprintf("%s-firefox-release-%s-%s-%s-%s",
		cacheKeyPrefix, cacheKey, version, h.goos(), h.goarch())
}

// cacheDir is where the files cached under key are kept: a fixed path in the
// user's cache directory. The job cache restores an entry to the absolute path
// it was saved from, and does so as the job's user, so the path has to be the
// same in every pipeline and writable without root. A temp directory is
// neither, and an entry saved from one is never found again.
func cacheDir(key string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve cache directory: %w", err)
	}
	return filepath.Join(base, cacheKeyPrefix, key), nil
}

// Cache wraps the job cache. Every failure is logged and swallowed, including
// running outside a CircleCI job at all, so a cache problem never stops an
// install that would otherwise succeed.
type Cache struct {
	Client *sdk.Client
	Out    io.Writer
}

func (c Cache) out() io.Writer {
	if c.Out != nil {
		return c.Out
	}
	return os.Stderr
}

// Restore reports whether path actually ended up populated. The check is
// necessary rather than paranoid: RestoreCache reports success on a miss too, so
// its result alone cannot distinguish a hit.
func (c Cache) Restore(ctx context.Context, key, path string) bool {
	if c.Client == nil {
		return false
	}

	_, err := c.Client.RestoreCache(ctx, []string{key})
	sdk.LogCacheErr("restore", err)
	if err != nil {
		return false
	}

	if !hasContent(path) {
		return false
	}
	fmt.Fprintf(c.out(), "%s: restored %s from cache\n", cacheKeyPrefix, path)
	return true
}

func (c Cache) Save(ctx context.Context, key string, paths []string) {
	if c.Client == nil {
		return
	}

	present := make([]string, 0, len(paths))
	for _, p := range paths {
		if hasContent(p) {
			present = append(present, p)
		}
	}
	if len(present) == 0 {
		return
	}

	sdk.LogCacheErr("save", c.Client.SaveCache(ctx, key, present))
}

func hasContent(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if !info.IsDir() {
		return info.Size() > 0
	}

	entries, err := os.ReadDir(path)
	return err == nil && len(entries) > 0
}
