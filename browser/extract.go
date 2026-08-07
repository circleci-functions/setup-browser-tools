package browser

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxEntrySize caps what a single archive member may expand to, so a malformed
// or hostile archive cannot fill the disk.
const maxEntrySize = 2 << 30

var ErrUnknownArchive = errors.New("unrecognised archive format")

// ErrUnsafePath reports an archive member that would be written outside the
// destination directory.
type ErrUnsafePath struct {
	Entry string
}

func (e *ErrUnsafePath) Error() string {
	return fmt.Sprintf("archive member %q would escape the destination directory", e.Entry)
}

// Extract unpacks archivePath into destDir, dispatching on the file extension.
//
// .tar.xz is handed to the tar binary because the standard library has no xz
// decoder; the alternative is a third-party xz dependency for one format. tar is
// already among the orb's stated requirements.
func Extract(ctx context.Context, r *Runner, archivePath, destDir string) error {
	switch {
	case strings.HasSuffix(archivePath, ".zip"):
		return extractZip(archivePath, destDir)
	case strings.HasSuffix(archivePath, ".tar.gz"), strings.HasSuffix(archivePath, ".tgz"):
		return extractTar(archivePath, destDir, gzipReader)
	case strings.HasSuffix(archivePath, ".tar.bz2"):
		return extractTar(archivePath, destDir, bzip2Reader)
	case strings.HasSuffix(archivePath, ".tar.xz"):
		return extractTarXZ(ctx, r, archivePath, destDir)
	default:
		return fmt.Errorf("%w: %s", ErrUnknownArchive, filepath.Base(archivePath))
	}
}

// safeJoin resolves name under destDir, rejecting absolute paths and any that
// climb out with "..".
func safeJoin(destDir, name string) (string, error) {
	if filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return "", &ErrUnsafePath{Entry: name}
	}

	target := filepath.Join(destDir, filepath.FromSlash(name))
	rel, err := filepath.Rel(destDir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", &ErrUnsafePath{Entry: name}
	}
	return target, nil
}

func extractZip(archivePath, destDir string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(archivePath), err)
	}
	defer zr.Close()

	for _, entry := range zr.File {
		if err := writeZipEntry(entry, destDir); err != nil {
			return err
		}
	}
	return nil
}

func writeZipEntry(entry *zip.File, destDir string) error {
	target, err := safeJoin(destDir, entry.Name)
	if err != nil {
		return err
	}

	if entry.FileInfo().IsDir() {
		return mkdirAll(target)
	}

	rc, err := entry.Open()
	if err != nil {
		return fmt.Errorf("open archive member %s: %w", entry.Name, err)
	}
	defer rc.Close()

	return writeFile(target, rc, entry.Mode())
}

type decompressor func(io.Reader) (io.Reader, error)

func gzipReader(r io.Reader) (io.Reader, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	return zr, nil
}

func bzip2Reader(r io.Reader) (io.Reader, error) { return bzip2.NewReader(r), nil }

func extractTar(archivePath, destDir string, wrap decompressor) error {
	f, err := os.Open(archivePath) //nolint:gosec // a file this process just downloaded
	if err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(archivePath), err)
	}
	defer f.Close()

	decompressed, err := wrap(f)
	if err != nil {
		return err
	}

	tr := tar.NewReader(decompressed)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", filepath.Base(archivePath), err)
		}
		if err := writeTarEntry(tr, header, destDir); err != nil {
			return err
		}
	}
}

func writeTarEntry(tr *tar.Reader, header *tar.Header, destDir string) error {
	target, err := safeJoin(destDir, header.Name)
	if err != nil {
		return err
	}

	switch header.Typeflag {
	case tar.TypeDir:
		return mkdirAll(target)
	case tar.TypeReg:
		return writeFile(target, tr, header.FileInfo().Mode())
	case tar.TypeSymlink:
		return writeSymlink(target, destDir, header)
	default:
		return nil
	}
}

// writeSymlink recreates a symlink, rejecting one whose target would resolve
// outside destDir. Firefox archives contain relative symlinks between shared
// libraries, so they cannot simply be skipped.
func writeSymlink(target, destDir string, header *tar.Header) error {
	if filepath.IsAbs(header.Linkname) {
		return &ErrUnsafePath{Entry: header.Linkname}
	}
	resolved := filepath.Join(filepath.Dir(target), filepath.FromSlash(header.Linkname))
	if _, err := safeJoin(destDir, mustRel(destDir, resolved)); err != nil {
		return err
	}

	if err := mkdirAll(filepath.Dir(target)); err != nil {
		return err
	}
	_ = os.Remove(target)
	if err := os.Symlink(header.Linkname, target); err != nil {
		return fmt.Errorf("create symlink %s: %w", header.Name, err)
	}
	return nil
}

func mustRel(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return ".." // fails safeJoin, which is the intent
	}
	return rel
}

func mkdirAll(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	return nil
}

func writeFile(target string, src io.Reader, mode os.FileMode) error {
	if err := mkdirAll(filepath.Dir(target)); err != nil {
		return err
	}

	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm()) //nolint:gosec // target came from safeJoin
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, io.LimitReader(src, maxEntrySize)); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	return out.Close()
}

func extractTarXZ(ctx context.Context, r *Runner, archivePath, destDir string) error {
	if _, err := r.Run(ctx, Command{
		Name:         "tar",
		Args:         []string{"-xJf", archivePath, "-C", destDir},
		Unprivileged: true,
		Quiet:        true,
	}); err != nil {
		return fmt.Errorf("extract %s: %w", filepath.Base(archivePath), err)
	}
	return nil
}
