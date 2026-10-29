package browser

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"
	"gotest.tools/v3/assert"
)

func TestExtract(t *testing.T) {
	tests := []struct {
		name    string
		archive string
	}{
		{name: "zip", archive: "archive.zip"},
		{name: "tar.gz", archive: "archive.tar.gz"},
		{name: "tar.bz2", archive: "archive.tar.bz2"},
		{name: "tar.xz", archive: "archive.tar.xz"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dest := t.TempDir()
			err := Extract(filepath.Join("testdata", tc.archive), dest)
			assert.NilError(t, err)

			driver, err := os.ReadFile(filepath.Join(dest, "chromedriver"))
			assert.NilError(t, err)
			assert.Equal(t, string(driver), "chromedriver binary\n")

			notes, err := os.ReadFile(filepath.Join(dest, "nested", "Driver_Notes"))
			assert.NilError(t, err)
			assert.Equal(t, string(notes), "notes\n")
		})
	}
}

// TestExtractTarBuffersTheArchive checks the decompressor reads through a
// buffer. Handed the file itself, xz makes a syscall for every few bytes,
// which is what made extracting Firefox take minutes.
func TestExtractTarBuffersTheArchive(t *testing.T) {
	var got io.Reader
	spy := func(r io.Reader) (io.Reader, error) {
		got = r
		return xzReader(r)
	}

	assert.NilError(t, extractTar(filepath.Join("testdata", "archive.tar.xz"), t.TempDir(), spy))

	buffered, ok := got.(*bufio.Reader)
	assert.Assert(t, ok, "the decompressor was handed a %T", got)
	assert.Equal(t, buffered.Size(), readBufferSize)
}

func TestExtractUnknownFormat(t *testing.T) {
	err := Extract(filepath.Join("testdata", "payload.txt"), t.TempDir())
	assert.Assert(t, errors.Is(err, ErrUnknownArchive))
}

// writeZip builds a zip containing the named entries, for archives that no real
// tool would produce.
func writeZip(t *testing.T, entries map[string]string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "crafted.zip")
	f, err := os.Create(path) //nolint:gosec // path is under t.TempDir()
	assert.NilError(t, err)
	defer f.Close()

	zw := zip.NewWriter(f)
	for name, body := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		assert.NilError(t, err)
		_, err = io.WriteString(w, body)
		assert.NilError(t, err)
	}
	assert.NilError(t, zw.Close())
	return path
}

func writeTarGz(t *testing.T, headers []*tar.Header, bodies []string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "crafted.tar")
	f, err := os.Create(path) //nolint:gosec // path is under t.TempDir()
	assert.NilError(t, err)
	defer f.Close()

	tw := tar.NewWriter(f)
	for i, h := range headers {
		assert.NilError(t, tw.WriteHeader(h))
		if bodies[i] != "" {
			_, err := io.WriteString(tw, bodies[i])
			assert.NilError(t, err)
		}
	}
	assert.NilError(t, tw.Close())

	gzPath := path + ".gz"
	assert.NilError(t, gzipFile(path, gzPath))
	return gzPath
}

func gzipFile(src, dest string) error {
	in, err := os.Open(src) //nolint:gosec // src is under t.TempDir()
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dest) //nolint:gosec // dest is under t.TempDir()
	if err != nil {
		return err
	}
	defer out.Close()

	zw := gzip.NewWriter(out)
	if _, err := io.Copy(zw, in); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return out.Close()
}

func writeTarXZ(t *testing.T, headers []*tar.Header, bodies []string) string {
	t.Helper()

	gzPath := writeTarGz(t, headers, bodies)
	tarPath := strings.TrimSuffix(gzPath, ".gz")

	xzPath := tarPath + ".xz"
	in, err := os.Open(tarPath) //nolint:gosec // under t.TempDir()
	assert.NilError(t, err)
	defer in.Close()

	out, err := os.Create(xzPath) //nolint:gosec // under t.TempDir()
	assert.NilError(t, err)
	defer out.Close()

	xw, err := xz.NewWriter(out)
	assert.NilError(t, err)
	_, err = io.Copy(xw, in)
	assert.NilError(t, err)
	assert.NilError(t, xw.Close())
	assert.NilError(t, out.Close())

	return xzPath
}

func TestExtractRejectsTraversal(t *testing.T) {
	tests := []struct {
		name  string
		build func(t *testing.T) string
	}{
		{
			name: "zip member climbing out with ..",
			build: func(t *testing.T) string {
				return writeZip(t, map[string]string{"../escaped.txt": "pwned"})
			},
		},
		{
			name: "zip member with an absolute path",
			build: func(t *testing.T) string {
				return writeZip(t, map[string]string{"/etc/cron.d/escaped": "pwned"})
			},
		},
		{
			name: "zip member climbing out through a subdirectory",
			build: func(t *testing.T) string {
				return writeZip(t, map[string]string{"a/../../escaped.txt": "pwned"})
			},
		},
		{
			name: "tar member climbing out with ..",
			build: func(t *testing.T) string {
				return writeTarGz(t,
					[]*tar.Header{{Name: "../escaped.txt", Typeflag: tar.TypeReg, Size: 5, Mode: 0o600}},
					[]string{"pwned"})
			},
		},
		{
			name: "tar.xz member climbing out with ..",
			build: func(t *testing.T) string {
				return writeTarXZ(t,
					[]*tar.Header{{Name: "../escaped.txt", Typeflag: tar.TypeReg, Size: 5, Mode: 0o600}},
					[]string{"pwned"})
			},
		},
		{
			name: "tar symlink pointing at an absolute path",
			build: func(t *testing.T) string {
				return writeTarGz(t,
					[]*tar.Header{{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0o777}},
					[]string{""})
			},
		},
		{
			name: "tar symlink resolving outside the destination",
			build: func(t *testing.T) string {
				return writeTarGz(t,
					[]*tar.Header{{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "../../etc/passwd", Mode: 0o777}},
					[]string{""})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dest := t.TempDir()
			err := Extract(tc.build(t), dest)

			var target *ErrUnsafePath
			assert.Assert(t, errors.As(err, &target), "got %v", err)

			escaped := filepath.Join(filepath.Dir(dest), "escaped.txt")
			_, statErr := os.Stat(escaped)
			assert.Assert(t, os.IsNotExist(statErr), "a file escaped to %s", escaped)
		})
	}
}

func TestExtractPreservesRelativeSymlinks(t *testing.T) {
	// Firefox archives symlink between shared libraries, so symlinks cannot
	// simply be dropped.
	archive := writeTarGz(t,
		[]*tar.Header{
			{Name: "libnss3.so", Typeflag: tar.TypeReg, Size: 3, Mode: 0o644},
			{Name: "libnss.so", Typeflag: tar.TypeSymlink, Linkname: "libnss3.so", Mode: 0o777},
		},
		[]string{"lib", ""})

	dest := t.TempDir()
	assert.NilError(t, Extract(archive, dest))

	got, err := os.ReadFile(filepath.Join(dest, "libnss.so"))
	assert.NilError(t, err)
	assert.Equal(t, string(got), "lib")
}

func TestSafeJoin(t *testing.T) {
	tests := []struct {
		name    string
		entry   string
		wantErr bool
	}{
		{name: "plain name", entry: "chromedriver"},
		{name: "nested name", entry: "chromedriver-linux64/chromedriver"},
		{name: "interior dot-dot that stays inside", entry: "a/b/../chromedriver"},
		{name: "leading dot-dot", entry: "../escaped", wantErr: true},
		{name: "dot-dot through a subdirectory", entry: "a/../../escaped", wantErr: true},
		{name: "absolute path", entry: "/etc/passwd", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := safeJoin("/dest", tc.entry)
			if tc.wantErr {
				assert.Assert(t, err != nil, "got %q", got)
				return
			}
			assert.NilError(t, err)
			assert.Assert(t, strings.HasPrefix(got, "/dest"), "got %q", got)
		})
	}
}

func TestExtractCreatesTraversableDirectories(t *testing.T) {
	// An extracted tree is copied into a system directory as root and then run by
	// the job user. A group-only directory mode leaves the binary unreachable,
	// which surfaces as "command not found" — it cost two rounds of the install
	// matrix to find, once for Chrome for Testing and once for Firefox.
	dest := t.TempDir()
	assert.NilError(t, Extract(filepath.Join("testdata", "archive.zip"), dest))

	info, err := os.Stat(filepath.Join(dest, "nested"))
	assert.NilError(t, err)
	assert.Equal(t, info.Mode().Perm()&0o055, os.FileMode(0o055),
		"extracted directories must be readable and traversable by others, got %v", info.Mode().Perm())
}
