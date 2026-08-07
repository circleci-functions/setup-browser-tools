package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	metadataTimeout  = 30 * time.Second
	artefactTimeout  = 10 * time.Minute
	downloadAttempts = 3
)

// Fetcher reads version metadata and downloads release artefacts. The zero
// value uses a default HTTP client; tests set HTTPClient to one pointed at an
// httptest server.
type Fetcher struct {
	HTTPClient *http.Client
}

func (f Fetcher) httpClient() *http.Client {
	if f.HTTPClient != nil {
		return f.HTTPClient
	}
	return &http.Client{Timeout: metadataTimeout}
}

func (f Fetcher) JSON(ctx context.Context, rawURL string, out any) error {
	body, err := f.get(ctx, rawURL)
	if err != nil {
		return err
	}
	defer body.Close()

	if err := json.NewDecoder(body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", rawURL, err)
	}
	return nil
}

// Text fetches rawURL and trims surrounding whitespace. Some version endpoints
// serve a bare version string rather than JSON.
func (f Fetcher) Text(ctx context.Context, rawURL string) (string, error) {
	body, err := f.get(ctx, rawURL)
	if err != nil {
		return "", err
	}
	defer body.Close()

	b, err := io.ReadAll(body)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", rawURL, err)
	}
	return strings.TrimSpace(string(b)), nil
}

// Exists distinguishes a version that was never published from a transport
// failure: a 404 gives (false, nil).
func (f Fetcher) Exists(ctx context.Context, rawURL string) (bool, error) {
	resp, err := f.do(ctx, http.MethodHead, rawURL)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if !isSuccess(resp.StatusCode) {
		return false, fmt.Errorf("head %s: unexpected status %s", rawURL, resp.Status)
	}
	return true, nil
}

// FinalURL follows redirects and returns the URL finally served. Resolving
// "latest" on GitHub relies on this: releases/latest redirects to a concrete tag.
func (f Fetcher) FinalURL(ctx context.Context, rawURL string) (string, error) {
	resp, err := f.do(ctx, http.MethodGet, rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))

	if !isSuccess(resp.StatusCode) {
		return "", fmt.Errorf("get %s: unexpected status %s", rawURL, resp.Status)
	}
	if resp.Request == nil || resp.Request.URL == nil {
		return "", fmt.Errorf("get %s: no final URL in response", rawURL)
	}
	return resp.Request.URL.String(), nil
}

func LastPathSegment(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", rawURL, err)
	}
	seg := path.Base(strings.TrimSuffix(u.Path, "/"))
	if seg == "" || seg == "." || seg == "/" {
		return "", fmt.Errorf("no path segment in %s", rawURL)
	}
	return seg, nil
}

// Download saves rawURL into dir as name, retrying a failed transfer. A large
// archive from a busy mirror is the likeliest thing in an install to fail
// transiently, which is why the orb passes curl --retry 3.
func (f Fetcher) Download(ctx context.Context, rawURL, dir, name string) (string, error) {
	dest := filepath.Join(dir, name)

	var err error
	for range downloadAttempts {
		if err = f.downloadOnce(ctx, rawURL, dest); err == nil {
			return dest, nil
		}
		// A partial file would otherwise be taken for a complete download and
		// only fail checksum verification much later.
		_ = os.Remove(dest)
	}

	return "", fmt.Errorf("download %s after %d attempts: %w", rawURL, downloadAttempts, err)
}

func (f Fetcher) downloadOnce(ctx context.Context, rawURL, dest string) error {
	client := f.httpClient()
	if f.HTTPClient == nil {
		// Metadata reads and multi-hundred-megabyte archives cannot share one
		// timeout.
		client = &http.Client{Timeout: artefactTimeout}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("build request for %s: %w", rawURL, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("get %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if !isSuccess(resp.StatusCode) {
		return fmt.Errorf("get %s: unexpected status %s", rawURL, resp.Status)
	}

	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // dest is dir plus a caller-chosen name, both ours
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	return out.Close()
}

func (f Fetcher) do(ctx context.Context, method, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", rawURL, err)
	}

	resp, err := f.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", strings.ToLower(method), rawURL, err)
	}
	return resp, nil
}

// get returns the body, which the caller must close.
func (f Fetcher) get(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	resp, err := f.do(ctx, http.MethodGet, rawURL)
	if err != nil {
		return nil, err
	}
	if !isSuccess(resp.StatusCode) {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("get %s: unexpected status %s", rawURL, resp.Status)
	}
	return resp.Body, nil
}

func isSuccess(code int) bool { return code >= 200 && code <= 299 }

// TempDir returns a staging directory and a function to remove it. Staging
// outside the working directory is what keeps stray archive members out of the
// caller's checkout.
func TempDir(purpose string) (dir string, cleanup func(), err error) {
	dir, err = os.MkdirTemp("", "setup-browser-tools-"+purpose+"-")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temp dir for %s: %w", purpose, err)
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}
