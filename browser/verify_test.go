package browser

import (
	"context"
	"errors"
	"io"
	"os"
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
			name:     "sha256 match",
			manifest: "SHA256SUMS",
			filename: "payload.txt",
		},
		{
			name:     "sha512 match",
			manifest: "SHA512SUMS",
			filename: "payload.txt",
		},
		{
			name:     "manifest path prefix is ignored, base name matches",
			manifest: "SHA256SUMS",
			filename: "linux-x86_64/en-US/payload.txt",
		},
		{
			name:     "malformed lines are skipped",
			manifest: "SHA256SUMS.messy",
			filename: "payload.txt",
		},
		{
			name:            "mismatch reports both digests",
			manifest:        "SHA256SUMS",
			filename:        "other.txt",
			wantErrContains: "checksum mismatch",
		},
		{
			name:         "filename absent from the manifest",
			manifest:     "SHA256SUMS",
			filename:     "not-listed.tar.xz",
			wantNotFound: true,
		},
		{
			name:            "digest of an unrecognised length",
			manifest:        "SHA256SUMS.short",
			filename:        "payload.txt",
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
				assert.Equal(t, target.Filename, "not-listed.tar.xz")
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

	err := VerifySums(filepath.Join("testdata", "SHA256SUMS"), "payload.txt", tampered)
	assert.Assert(t, err != nil)
	// Both digests must appear, or the failure cannot be diagnosed from the log.
	assert.Assert(t, strings.Contains(err.Error(), "01179dc294ceea33cd319f79d3860e084f764e10be49aec784d23467b451f610"),
		"error %q should record the expected digest", err.Error())
	assert.Assert(t, strings.Contains(err.Error(), "file is "), "error %q should record the actual digest", err.Error())
}

func TestVerifySumsMissingFiles(t *testing.T) {
	t.Run("missing manifest", func(t *testing.T) {
		err := VerifySums(filepath.Join("testdata", "nope"), "payload.txt", filepath.Join("testdata", "payload.txt"))
		assert.Assert(t, err != nil)
	})

	t.Run("missing subject", func(t *testing.T) {
		err := VerifySums(filepath.Join("testdata", "SHA256SUMS"), "payload.txt", filepath.Join("testdata", "nope"))
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
		{name: "two fields", line: "abc123  payload.txt", wantDigest: "abc123", wantName: "payload.txt", wantOK: true},
		{name: "binary mode asterisk is stripped", line: "abc123 *payload.txt", wantDigest: "abc123", wantName: "payload.txt", wantOK: true},
		{name: "blank line", line: "", wantOK: false},
		{name: "one field", line: "abc123", wantOK: false},
		{name: "three fields", line: "a b c", wantOK: false},
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

func TestVerifyGPGRequiresGPG(t *testing.T) {
	r := &Runner{Priv: rootPrivilege(), Out: io.Discard}
	h := Host{LookPath: fakePath()}

	err := VerifyGPG(context.Background(), r, h, "", "SHA256SUMS.asc", "SHA256SUMS")
	assert.Assert(t, err != nil)
	assert.Assert(t, strings.Contains(err.Error(), "gpg is required"),
		"a missing gpg must fail rather than skip verification: %v", err)
}

func TestVerifyGPGInvokesGPG(t *testing.T) {
	rec := &recorder{}
	r := &Runner{Priv: sudoPrivilege(), Out: io.Discard, Executor: rec.exec}
	h := Host{LookPath: fakePath("gpg")}

	assert.NilError(t, VerifyGPG(context.Background(), r, h, "KEY", "SHA256SUMS.asc", "SHA256SUMS"))

	assert.Equal(t, len(rec.calls), 2)
	assert.DeepEqual(t, rec.calls[0].args, []string{"--import", "KEY"})
	assert.DeepEqual(t, rec.calls[1].args, []string{"--verify", "SHA256SUMS.asc", "SHA256SUMS"})
	for _, c := range rec.calls {
		assert.Equal(t, c.name, "gpg", "gpg must not be run through sudo")
	}
}

func TestVerifyGPGSkipsImportWithoutAKey(t *testing.T) {
	rec := &recorder{}
	r := &Runner{Priv: rootPrivilege(), Out: io.Discard, Executor: rec.exec}
	h := Host{LookPath: fakePath("gpg")}

	assert.NilError(t, VerifyGPG(context.Background(), r, h, "", "SHA256SUMS.asc", "SHA256SUMS"))
	assert.Equal(t, len(rec.calls), 1)
	assert.DeepEqual(t, rec.calls[0].args, []string{"--verify", "SHA256SUMS.asc", "SHA256SUMS"})
}

func TestVerifyGPGPropagatesFailure(t *testing.T) {
	rec := &recorder{failures: 1}
	r := &Runner{Priv: rootPrivilege(), Out: io.Discard, Executor: rec.exec}
	h := Host{LookPath: fakePath("gpg")}

	err := VerifyGPG(context.Background(), r, h, "", "SHA256SUMS.asc", "SHA256SUMS")
	assert.Assert(t, err != nil)
	assert.Assert(t, strings.Contains(err.Error(), "verify signature on SHA256SUMS"))
}
