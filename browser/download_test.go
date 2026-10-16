package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func testFetcher(handler http.Handler) (Fetcher, *httptest.Server) {
	srv := httptest.NewServer(handler)
	return Fetcher{HTTPClient: srv.Client()}, srv
}

func TestFetcherJSON(t *testing.T) {
	f, srv := testFetcher(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/versions.json":
			fmt.Fprint(w, `{"channels":{"Stable":{"version":"147.0.7727.55"}}}`)
		case "/broken.json":
			fmt.Fprint(w, `not json`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	var out struct {
		Channels struct {
			Stable struct {
				Version string `json:"version"`
			} `json:"Stable"`
		} `json:"channels"`
	}

	assert.NilError(t, f.JSON(context.Background(), srv.URL+"/versions.json", &out))
	assert.Equal(t, out.Channels.Stable.Version, "147.0.7727.55")

	err := f.JSON(context.Background(), srv.URL+"/broken.json", &out)
	assert.Assert(t, err != nil)
	assert.Assert(t, strings.Contains(err.Error(), "decode"))

	err = f.JSON(context.Background(), srv.URL+"/missing.json", &out)
	assert.Assert(t, err != nil)
	assert.Assert(t, strings.Contains(err.Error(), "404"))
}

func TestFetcherText(t *testing.T) {
	f, srv := testFetcher(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "  114.0.5735.90\n\n")
	}))
	defer srv.Close()

	got, err := f.Text(context.Background(), srv.URL+"/LATEST_RELEASE_114")
	assert.NilError(t, err)
	assert.Equal(t, got, "114.0.5735.90")
}

func TestFetcherTextDecodesAMarkedBody(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{name: "UTF-16LE, as msedgedriver.microsoft.com serves it", body: []byte{0xFF, 0xFE, '1', 0, '5', 0, '5', 0, '.', 0, '0', 0, '\r', 0, '\n', 0}},
		{name: "UTF-16BE", body: []byte{0xFE, 0xFF, 0, '1', 0, '5', 0, '5', 0, '.', 0, '0', 0, '\n'}},
		{name: "UTF-8 with a BOM", body: []byte("\xEF\xBB\xBF155.0\n")},
		{name: "plain UTF-8", body: []byte("155.0\n")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, srv := testFetcher(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(tc.body)
			}))
			defer srv.Close()

			got, err := f.Text(context.Background(), srv.URL+"/LATEST_RELEASE_155_LINUX")
			assert.NilError(t, err)
			assert.Equal(t, got, "155.0")
		})
	}
}

func TestFetcherExists(t *testing.T) {
	f, srv := testFetcher(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/present.zip":
			w.WriteHeader(http.StatusOK)
		case "/gone.zip":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	tests := []struct {
		name    string
		path    string
		want    bool
		wantErr bool
	}{
		{name: "published version", path: "/present.zip", want: true},
		{name: "unpublished version is not an error", path: "/gone.zip", want: false},
		{name: "server failure is an error", path: "/boom.zip", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := f.Exists(context.Background(), srv.URL+tc.path)
			if tc.wantErr {
				assert.Assert(t, err != nil)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestFetcherFinalURLFollowsRedirects(t *testing.T) {
	f, srv := testFetcher(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases/latest" {
			http.Redirect(w, r, "/releases/tag/v0.36.0", http.StatusFound)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	got, err := f.FinalURL(context.Background(), srv.URL+"/releases/latest")
	assert.NilError(t, err)
	assert.Assert(t, strings.HasSuffix(got, "/releases/tag/v0.36.0"), "got %q", got)

	tag, err := LastPathSegment(got)
	assert.NilError(t, err)
	assert.Equal(t, tag, "v0.36.0")
}

func TestLastPathSegment(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "tag path", in: "https://example.com/releases/tag/v0.36.0", want: "v0.36.0"},
		{name: "trailing slash is ignored", in: "https://example.com/releases/tag/v0.36.0/", want: "v0.36.0"},
		{name: "single segment", in: "https://example.com/v1", want: "v1"},
		{name: "no path", in: "https://example.com", wantErr: true},
		{name: "root path", in: "https://example.com/", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := LastPathSegment(tc.in)
			if tc.wantErr {
				assert.Assert(t, err != nil, "got %q", got)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestFetcherDownload(t *testing.T) {
	const body = "chromedriver-binary-contents"

	var hits int
	f, srv := testFetcher(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/driver.zip":
			hits++
			fmt.Fprint(w, body)
		case "/flaky.zip":
			hits++
			if hits < 3 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			fmt.Fprint(w, body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	t.Run("saves under the requested name", func(t *testing.T) {
		hits = 0
		dir := t.TempDir()
		got, err := f.Download(context.Background(), srv.URL+"/driver.zip", dir, "chromedriver.zip")
		assert.NilError(t, err)
		assert.Equal(t, got, filepath.Join(dir, "chromedriver.zip"))

		contents, err := os.ReadFile(got)
		assert.NilError(t, err)
		assert.Equal(t, string(contents), body)
		assert.Equal(t, hits, 1)
	})

	t.Run("retries a transient failure", func(t *testing.T) {
		hits = 0
		dir := t.TempDir()
		got, err := f.Download(context.Background(), srv.URL+"/flaky.zip", dir, "driver.zip")
		assert.NilError(t, err)
		assert.Equal(t, hits, 3)

		contents, err := os.ReadFile(got)
		assert.NilError(t, err)
		assert.Equal(t, string(contents), body)
	})

	t.Run("leaves no partial file behind when it gives up", func(t *testing.T) {
		hits = 0
		dir := t.TempDir()
		_, err := f.Download(context.Background(), srv.URL+"/missing.zip", dir, "driver.zip")
		assert.Assert(t, err != nil)
		assert.Assert(t, strings.Contains(err.Error(), "3 attempts"))

		_, statErr := os.Stat(filepath.Join(dir, "driver.zip"))
		assert.Assert(t, os.IsNotExist(statErr), "a partial download was left behind")
	})
}

func TestTempDirIsRemovedByCleanup(t *testing.T) {
	dir, cleanup, err := TempDir("chrome")
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(filepath.Base(dir), "chrome"))

	_, err = os.Stat(dir)
	assert.NilError(t, err)

	cleanup()
	_, err = os.Stat(dir)
	assert.Assert(t, os.IsNotExist(err))
}
