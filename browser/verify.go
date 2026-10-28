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
	"slices"
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

// parseSumsLine splits a manifest line into its digest and name. The name is
// everything after the digest, since it can contain spaces: Mozilla's macOS
// build is "mac/en-US/Firefox 149.0.2.dmg".
func parseSumsLine(line string) (digest, name string, ok bool) {
	digest, rest, found := strings.Cut(strings.TrimSpace(line), " ")
	if !found {
		return "", "", false
	}
	// sha256sum marks binary mode with a leading '*' on the filename.
	name = strings.TrimPrefix(strings.TrimLeft(rest, " "), "*")
	if digest == "" || name == "" {
		return "", "", false
	}
	return digest, name, true
}

// VerifyGPG checks that sigPath is a good detached signature over signed, made
// by the key whose primary key fingerprint is wantPrimary or by any of its
// subkeys. Only the keys in keyPath are consulted. They are imported into a
// keyring of their own, which is removed afterwards, so the job user's keyring
// neither supplies a key nor gains one.
//
// The pin is what makes the check worth having when keyPath comes from the same
// place as the signature: without it, any key would verify a signature made
// with it. Pinning the primary key rather than the signing key lets the
// publisher rotate signing subkeys without breaking verification.
//
// It shells out to gpg, as the orb does and as the orb's documented
// requirements already state, rather than taking on an in-process OpenPGP
// implementation. A missing gpg is an error: skipping the check would leave
// callers believing a download was verified when it was not.
func VerifyGPG(ctx context.Context, r *Runner, h Host, keyPath, sigPath, signed, wantPrimary string) error {
	name := filepath.Base(signed)
	if !h.has("gpg") {
		return fmt.Errorf("gpg is required to verify %s but is not installed", name)
	}

	home, cleanup, err := TempDir("gnupg")
	if err != nil {
		return err
	}
	defer cleanup()
	// --no-autostart keeps gpg from leaving an agent running for the temporary
	// home; nothing here needs a secret key.
	gpg := func(args ...string) Command {
		return Command{
			Name:         "gpg",
			Args:         append([]string{"--homedir", home, "--batch", "--no-tty", "--no-autostart"}, args...),
			Unprivileged: true,
			Quiet:        true,
		}
	}

	if _, err := r.Run(ctx, gpg("--import", keyPath)); err != nil {
		return fmt.Errorf("import signing key for %s: %w", name, err)
	}

	status, err := r.Run(ctx, gpg("--status-fd", "1", "--verify", sigPath, signed))
	if err != nil {
		return fmt.Errorf("signature on %s does not verify against key %s: %w%s",
			name, wantPrimary, err, gpgFailure(status))
	}

	signers := validSigners(status)
	if slices.ContainsFunc(signers, func(primary string) bool { return strings.EqualFold(primary, wantPrimary) }) {
		return nil
	}
	if len(signers) == 0 {
		return fmt.Errorf("gpg reported no valid signature on %s; expected one by key %s", name, wantPrimary)
	}
	return fmt.Errorf("signature on %s is by key %s, not by the expected key %s or one of its subkeys",
		name, strings.Join(signers, ", "), wantPrimary)
}

// validSigners returns the primary key fingerprint of each good signature in
// gpg's status output. A VALIDSIG line ends with the primary key's fingerprint
// whichever of its subkeys made the signature.
func validSigners(status string) []string {
	var signers []string
	for line := range strings.Lines(status) {
		fields := strings.Fields(line)
		if len(fields) >= 12 && fields[0] == "[GNUPG:]" && fields[1] == "VALIDSIG" {
			signers = append(signers, fields[len(fields)-1])
		}
	}
	return signers
}

// gpgFailureStatus are the status keywords that say why a signature did not
// verify.
var gpgFailureStatus = []string{"BADSIG", "ERRSIG", "NO_PUBKEY", "NODATA", "EXPSIG", "REVKEYSIG"}

// gpgFailure returns, for an error message, the status lines explaining a
// failed verification, since gpg's own explanation is not logged.
func gpgFailure(status string) string {
	var reasons []string
	for line := range strings.Lines(status) {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "[GNUPG:]" && slices.Contains(gpgFailureStatus, fields[1]) {
			reasons = append(reasons, strings.Join(fields[1:], " "))
		}
	}
	if len(reasons) == 0 {
		return ""
	}
	return " (" + strings.Join(reasons, "; ") + ")"
}
