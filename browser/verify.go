package browser

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	sha256HexLen = sha256.Size * 2
	sha512HexLen = sha512.Size * 2
)

type ErrNotInManifest struct {
	// Entry is the manifest key that was looked for.
	Entry    string
	Manifest string
}

func (e *ErrNotInManifest) Error() string {
	return fmt.Sprintf("%s has no entry for %s", e.Manifest, e.Entry)
}

// VerifySums checks path against the digest recorded for entry in a SHA256SUMS or
// SHA512SUMS manifest. entry must be the name exactly as the manifest lists it,
// which for Mozilla means the full "linux-x86_64/en-US/firefox-153.0.3.tar.xz"
// and not just the base name — one manifest lists that same base name for every
// architecture and every locale, so a base-name match picks an arbitrary one.
//
// The algorithm comes from the length of the recorded digest, so one function
// handles either manifest without being told which it was given.
func VerifySums(manifestPath, entry, path string) error {
	want, err := digestFromManifest(manifestPath, entry)
	if err != nil {
		return err
	}

	var h hash.Hash
	switch len(want) {
	case sha256HexLen:
		h = sha256.New()
	case sha512HexLen:
		h = sha512.New()
	default:
		return fmt.Errorf("%s: entry for %s has a %d-character digest, expected %d or %d",
			manifestPath, entry, len(want), sha256HexLen, sha512HexLen)
	}

	got, err := hashFile(path, h)
	if err != nil {
		return err
	}

	if got != want {
		return fmt.Errorf("checksum mismatch for %s: manifest %s records %s, file is %s",
			entry, filepath.Base(manifestPath), want, got)
	}
	return nil
}

func hashFile(path string, h hash.Hash) (string, error) {
	f, err := os.Open(path) //nolint:gosec // a file this process just downloaded
	if err != nil {
		return "", fmt.Errorf("open %s for hashing: %w", path, err)
	}
	defer f.Close()

	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("read %s for hashing: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// digestFromManifest returns the digest recorded for exactly entry.
func digestFromManifest(manifestPath, entry string) (string, error) {
	f, err := os.Open(manifestPath) //nolint:gosec // a file this process just downloaded
	if err != nil {
		return "", fmt.Errorf("open %s: %w", manifestPath, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		digest, name, ok := parseSumsLine(scanner.Text())
		if ok && name == entry {
			return digest, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read %s: %w", manifestPath, err)
	}

	return "", &ErrNotInManifest{Entry: entry, Manifest: filepath.Base(manifestPath)}
}

func parseSumsLine(line string) (digest, name string, ok bool) {
	fields := strings.Fields(line)
	if len(fields) != 2 {
		return "", "", false
	}
	// sha256sum marks binary mode with a leading '*' on the filename.
	return fields[0], strings.TrimPrefix(fields[1], "*"), true
}

// VerifyGPG checks a detached signature over signed, importing keyPath first
// when non-empty. It shells out to gpg, as the orb does and as the orb's
// documented requirements already state, rather than taking on an in-process
// OpenPGP implementation.
//
// A missing gpg is an error: skipping the check would leave callers believing a
// download was verified when it was not.
func VerifyGPG(ctx context.Context, r *Runner, h Host, keyPath, sigPath, signed string) error {
	if !h.has("gpg") {
		return fmt.Errorf("gpg is required to verify %s but is not installed", filepath.Base(signed))
	}

	if keyPath != "" {
		if _, err := r.Run(ctx, Command{
			Name:         "gpg",
			Args:         []string{"--import", keyPath},
			Unprivileged: true,
			Quiet:        true,
		}); err != nil {
			return fmt.Errorf("import signing key: %w", err)
		}
	}

	if _, err := r.Run(ctx, Command{
		Name:         "gpg",
		Args:         []string{"--verify", sigPath, signed},
		Unprivileged: true,
	}); err != nil {
		return fmt.Errorf("verify signature on %s: %w", filepath.Base(signed), err)
	}
	return nil
}
