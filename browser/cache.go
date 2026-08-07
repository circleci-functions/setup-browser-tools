package browser

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/circleci-functions/toolkit/sdk"
)

const cacheKeyPrefix = "setup-browser-tools"

// ChromeCacheKey includes the release channel, which the orb's key omits — so a
// beta install could restore a stable entry at the same version, and vice versa.
func ChromeCacheKey(h Host, cacheKey, channel, version string) string {
	return fmt.Sprintf("%s-chrome-%s-%s-%s-%s-%s",
		cacheKeyPrefix, cacheKey, channel, version, h.goos(), h.goarch())
}

func FirefoxCacheKey(h Host, cacheKey, version string) string {
	return fmt.Sprintf("%s-firefox-%s-%s-%s-%s",
		cacheKeyPrefix, cacheKey, version, h.goos(), h.goarch())
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
