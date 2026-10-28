package browser

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestVerifySums(t *testing.T) {
	tests := []struct {
		name            string
		manifest        string
		filename        string
		wantErrContains string
		wantNotFound    bool
	}{
		{
			name:     "sha256 match on the exact entry",
			manifest: "SHA256SUMS",
			filename: "linux-x86_64/en-US/payload.txt",
		},
		{
			name:     "sha512 match",
			manifest: "SHA512SUMS",
			filename: "linux-x86_64/en-US/payload.txt",
		},
		{
			name:     "malformed lines are skipped",
			manifest: "SHA256SUMS.messy",
			filename: "linux-x86_64/en-US/payload.txt",
		},
		{
			// Mozilla lists this same base name for every architecture and every
			// locale, each with its own digest. Matching on the base name picked an
			// arbitrary one, which made Firefox uninstallable.
			name:            "a different architecture with the same base name is not accepted",
			manifest:        "SHA256SUMS",
			filename:        "linux-i686/en-US/payload.txt",
			wantErrContains: "checksum mismatch",
		},
		{
			name:            "a different locale with the same base name is not accepted",
			manifest:        "SHA256SUMS",
			filename:        "linux-x86_64/de/payload.txt",
			wantErrContains: "checksum mismatch",
		},
		{
			name:         "a bare base name no longer matches anything",
			manifest:     "SHA256SUMS",
			filename:     "payload.txt",
			wantNotFound: true,
		},
		{
			name:         "entry absent from the manifest",
			manifest:     "SHA256SUMS",
			filename:     "linux-x86_64/en-US/not-listed.tar.xz",
			wantNotFound: true,
		},
		{
			name:            "digest of an unrecognised length",
			manifest:        "SHA256SUMS.short",
			filename:        "linux-x86_64/en-US/payload.txt",
			wantErrContains: "8-character digest",
		},
	}

	payload := filepath.Join("testdata", "payload.txt")

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifySums(filepath.Join("testdata", tc.manifest), tc.filename, payload)

			switch {
			case tc.wantNotFound:
				var target *ErrNotInManifest
				assert.Assert(t, errors.As(err, &target), "got %v", err)
				assert.Equal(t, target.Entry, tc.filename)
			case tc.wantErrContains != "":
				assert.Assert(t, err != nil)
				assert.Assert(t, strings.Contains(err.Error(), tc.wantErrContains),
					"error %q should contain %q", err.Error(), tc.wantErrContains)
			default:
				assert.NilError(t, err)
			}
		})
	}
}

func TestVerifySumsMismatchNamesExpectedAndActual(t *testing.T) {
	dir := t.TempDir()
	tampered := filepath.Join(dir, "payload.txt")
	assert.NilError(t, os.WriteFile(tampered, []byte("tampered\n"), 0o600))

	err := VerifySums(filepath.Join("testdata", "SHA256SUMS"), "linux-x86_64/en-US/payload.txt", tampered)
	assert.Assert(t, err != nil)
	// Both digests must appear, or the failure cannot be diagnosed from the log.
	assert.Assert(t, strings.Contains(err.Error(), "01179dc294ceea33cd319f79d3860e084f764e10be49aec784d23467b451f610"),
		"error %q should record the expected digest", err.Error())
	assert.Assert(t, strings.Contains(err.Error(), "file is "), "error %q should record the actual digest", err.Error())
}

func TestVerifySumsMissingFiles(t *testing.T) {
	t.Run("missing manifest", func(t *testing.T) {
		err := VerifySums(filepath.Join("testdata", "nope"), "linux-x86_64/en-US/payload.txt", filepath.Join("testdata", "payload.txt"))
		assert.Assert(t, err != nil)
	})

	t.Run("missing subject", func(t *testing.T) {
		err := VerifySums(filepath.Join("testdata", "SHA256SUMS"), "linux-x86_64/en-US/payload.txt", filepath.Join("testdata", "nope"))
		assert.Assert(t, err != nil)
		assert.Assert(t, strings.Contains(err.Error(), "hashing"))
	})
}

func TestParseSumsLine(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantDigest string
		wantName   string
		wantOK     bool
	}{
		{name: "two fields", line: "abc123  a/b/payload.txt", wantDigest: "abc123", wantName: "a/b/payload.txt", wantOK: true},
		{name: "binary mode asterisk is stripped", line: "abc123 *payload.txt", wantDigest: "abc123", wantName: "payload.txt", wantOK: true},
		{name: "blank line", line: "", wantOK: false},
		{name: "one field", line: "abc123", wantOK: false},
		{name: "a name with a space is kept whole", line: "abc123  mac/en-US/Firefox 149.0.2.dmg", wantDigest: "abc123", wantName: "mac/en-US/Firefox 149.0.2.dmg", wantOK: true},
		{name: "no name after the separator", line: "abc123  ", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			digest, name, ok := parseSumsLine(tc.line)
			assert.Equal(t, ok, tc.wantOK)
			if tc.wantOK {
				assert.Equal(t, digest, tc.wantDigest)
				assert.Equal(t, name, tc.wantName)
			}
		})
	}
}

// The keys in testdata/gpg are throwaway ones made for these tests. "pinned"
// has a certify-only primary key and a signing subkey, as Mozilla's release key
// does; "other" is an unrelated key. Each SHA256SUMS.*.asc is a detached
// signature over testdata/SHA256SUMS.
const (
	testPinnedKey    = "2AB70643F3597EDADCD9CC1C769BC9F386C12C96"
	testPinnedSubkey = "BAA16EDF962F27DA4BD9F855C4423E21A536D811"
	testOtherKey     = "0450652AA27E0BBFD0B1CCD873D94300B9DD3BE7"
)

// validSig is gpg's status line for a good signature by a subkey of primary.
func validSig(primary string) string {
	return "[GNUPG:] VALIDSIG " + testPinnedSubkey + " 2026-10-27 1793129278 0 4 0 1 8 00 " + primary + "\n"
}

func TestVerifyGPGRequiresGPG(t *testing.T) {
	r := &Runner{Priv: rootPrivilege(), Out: io.Discard}
	h := Host{LookPath: fakePath()}

	err := VerifyGPG(context.Background(), r, h, "KEY", "SHA256SUMS.asc", "SHA256SUMS", testPinnedKey)
	assert.Assert(t, err != nil)
	assert.Assert(t, strings.Contains(err.Error(), "gpg is required"),
		"a missing gpg must fail rather than skip verification: %v", err)
}

func TestVerifyGPGUsesAKeyringOfItsOwn(t *testing.T) {
	rec := &recorder{stdout: validSig(testPinnedKey)}
	r := &Runner{Priv: sudoPrivilege(), Out: io.Discard, Executor: rec.exec}
	h := Host{LookPath: fakePath("gpg")}

	assert.NilError(t, VerifyGPG(context.Background(), r, h, "KEY", "SHA256SUMS.asc", "SHA256SUMS", testPinnedKey))

	assert.Equal(t, len(rec.calls), 2)
	home := rec.calls[0].args[1]
	isolated := []string{"--homedir", home, "--batch", "--no-tty", "--no-autostart"}
	assert.DeepEqual(t, rec.calls[0].args, append(isolated, "--import", "KEY"))
	assert.DeepEqual(t, rec.calls[1].args,
		append(isolated, "--status-fd", "1", "--verify", "SHA256SUMS.asc", "SHA256SUMS"))
	for _, c := range rec.calls {
		assert.Equal(t, c.name, "gpg", "gpg must not be run through sudo")
	}
	_, err := os.Stat(home)
	assert.Assert(t, errors.Is(err, os.ErrNotExist), "the temporary keyring was left behind: %v", err)
}

func TestVerifyGPGPropagatesFailure(t *testing.T) {
	rec := &recorder{failures: 1}
	r := &Runner{Priv: rootPrivilege(), Out: io.Discard, Executor: rec.exec}
	h := Host{LookPath: fakePath("gpg")}

	err := VerifyGPG(context.Background(), r, h, "KEY", "SHA256SUMS.asc", "SHA256SUMS", testPinnedKey)
	assert.ErrorContains(t, err, "import signing key for SHA256SUMS")
}

// TestVerifyGPGPinsThePrimaryKey runs the real gpg over the fixtures in
// testdata/gpg, since what matters is how gpg itself reports each case.
func TestVerifyGPGPinsThePrimaryKey(t *testing.T) {
	if _, err := exec.LookPath("gpg"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("gpg is not installed")
		}
		t.Skip("gpg is not installed")
	}

	tests := []struct {
		name            string
		keys            []string
		sig             string
		signed          string
		wantErrContains string
	}{
		{
			name:   "a signature by a subkey of the pinned primary key",
			keys:   []string{"pinned.key"},
			sig:    "SHA256SUMS.subkey.asc",
			signed: "SHA256SUMS",
		},
		{
			name:            "a signature by a key the key file does not hold",
			keys:            []string{"pinned.key"},
			sig:             "SHA256SUMS.other.asc",
			signed:          "SHA256SUMS",
			wantErrContains: "does not verify against key " + testPinnedKey,
		},
		{
			// What a poisoned cache entry or a compromised mirror could serve: a
			// key, and a manifest it really did sign.
			name:            "a good signature by a key other than the pinned one",
			keys:            []string{"other.key"},
			sig:             "SHA256SUMS.other.asc",
			signed:          "SHA256SUMS",
			wantErrContains: "is by key " + testOtherKey + ", not by the expected key " + testPinnedKey,
		},
		{
			name:            "another key served alongside the pinned one",
			keys:            []string{"pinned.key", "other.key"},
			sig:             "SHA256SUMS.other.asc",
			signed:          "SHA256SUMS",
			wantErrContains: "not by the expected key " + testPinnedKey,
		},
		{
			name:            "the pinned key's signature over a different file",
			keys:            []string{"pinned.key"},
			sig:             "SHA256SUMS.subkey.asc",
			signed:          "SHA512SUMS",
			wantErrContains: "BADSIG",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// gpg's default home points somewhere observable, to show it is left
			// alone.
			userHome := t.TempDir()
			t.Setenv("GNUPGHOME", userHome)
			t.Setenv("HOME", userHome)

			var keys []byte
			for _, name := range tc.keys {
				b, err := os.ReadFile(filepath.Join("testdata", "gpg", name))
				assert.NilError(t, err)
				keys = append(keys, b...)
			}
			keyPath := filepath.Join(t.TempDir(), "KEY")
			assert.NilError(t, os.WriteFile(keyPath, keys, 0o600)) //nolint:gosec // keyPath is under t.TempDir()

			r := &Runner{Priv: rootPrivilege(), Out: io.Discard}
			h := Host{LookPath: exec.LookPath}
			err := VerifyGPG(context.Background(), r, h, keyPath,
				filepath.Join("testdata", "gpg", tc.sig), filepath.Join("testdata", tc.signed), testPinnedKey)

			if tc.wantErrContains == "" {
				assert.NilError(t, err)
			} else {
				assert.ErrorContains(t, err, tc.wantErrContains)
			}
			left, err := os.ReadDir(userHome)
			assert.NilError(t, err)
			assert.Equal(t, len(left), 0, "gpg wrote to the user's home: %v", left)
		})
	}
}

func TestValidSigners(t *testing.T) {
	status := "[GNUPG:] NEWSIG\n" +
		"[GNUPG:] KEYEXPIRED 1684354261\n" +
		"[GNUPG:] GOODSIG 5ECB6497C1A20256 Mozilla Software Releases <release@mozilla.com>\n" +
		"[GNUPG:] VALIDSIG 09BEED63F3462A2DFFAB3B875ECB6497C1A20256 2026-04-03 1775240024 0 4 0 1 10 00 " +
		"14F26682D0916CDD81E37B6D61B7B526D98F0353\n" +
		// Without the primary key's fingerprint there is nothing to pin against.
		"[GNUPG:] VALIDSIG 09BEED63F3462A2DFFAB3B875ECB6497C1A20256 2026-04-03 1775240024 0 4 0 1 10 00\n" +
		"VALIDSIG 09BEED63F3462A2DFFAB3B875ECB6497C1A20256 is not a status line 1 2 3 4 5 6 7\n"

	assert.DeepEqual(t, validSigners(status), []string{"14F26682D0916CDD81E37B6D61B7B526D98F0353"})
}
