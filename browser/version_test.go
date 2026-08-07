package browser

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestExpandVersion(t *testing.T) {
	env := map[string]string{
		"CHROME_VERSION": "147.0.7727.55",
		"FF":             "149.0.2",
		"EMPTY":          "",
		"_LEADING":       "1.2.3",
		"WITH9":          "9.9",
	}
	lookup := func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	}

	tests := []struct {
		name string
		in   string
		want string
		// wantErrContains, when non-empty, is required to appear in the error.
		wantErrContains []string
	}{
		{
			name: "no dollar is returned unchanged",
			in:   "147.0.7727.55",
			want: "147.0.7727.55",
		},
		{
			name: "latest is returned unchanged",
			in:   Latest,
			want: Latest,
		},
		{
			name: "empty string is returned unchanged",
			in:   "",
			want: "",
		},
		{
			name: "bare reference",
			in:   "$CHROME_VERSION",
			want: "147.0.7727.55",
		},
		{
			name: "braced reference",
			in:   "${CHROME_VERSION}",
			want: "147.0.7727.55",
		},
		{
			name: "leading underscore is a valid name",
			in:   "$_LEADING",
			want: "1.2.3",
		},
		{
			name: "digits allowed after the first character",
			in:   "$WITH9",
			want: "9.9",
		},
		{
			name: "reference embedded in surrounding text",
			in:   "v${FF}esr",
			want: "v149.0.2esr",
		},
		{
			name: "two references in one value",
			in:   "$FF-$CHROME_VERSION",
			want: "149.0.2-147.0.7727.55",
		},
		{
			name: "braced reference stops at the closing brace",
			in:   "${FF}0",
			want: "149.0.20",
		},
		{
			name: "bare reference stops at a non-name character",
			in:   "$FF.1",
			want: "149.0.2.1",
		},
		{
			name: "double dollar is literal",
			in:   "$$",
			want: "$$",
		},
		{
			name: "trailing dollar is literal",
			in:   "147$",
			want: "147$",
		},
		{
			name: "dollar before a digit is literal",
			in:   "$1.2",
			want: "$1.2",
		},
		{
			name: "dollar before a dash is literal",
			in:   "$-x",
			want: "$-x",
		},
		{
			name:            "unset variable is an error naming it",
			in:              "$NOPE",
			wantErrContains: []string{"NOPE", "not set", "--chrome-version"},
		},
		{
			name:            "unset braced variable is an error naming it",
			in:              "${NOPE}",
			wantErrContains: []string{"NOPE", "not set"},
		},
		{
			name:            "variable set but empty is an error, not an empty expansion",
			in:              "$EMPTY",
			wantErrContains: []string{"EMPTY", "empty"},
		},
		{
			name:            "unterminated brace is an error",
			in:              "${FF",
			wantErrContains: []string{"unterminated"},
		},
		{
			name:            "invalid name inside braces is an error",
			in:              "${NOT-A-NAME}",
			wantErrContains: []string{"invalid variable name"},
		},
		{
			name:            "an unset variable anywhere fails the whole value",
			in:              "$FF-$NOPE",
			wantErrContains: []string{"NOPE", "not set"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExpandVersion(tc.in, "--chrome-version", lookup)

			if len(tc.wantErrContains) > 0 {
				assert.Assert(t, err != nil, "want an error, got %q", got)
				for _, want := range tc.wantErrContains {
					assert.Assert(t, strings.Contains(err.Error(), want),
						"error %q does not contain %q", err.Error(), want)
				}
				// An error must never yield a usable value alongside it.
				assert.Equal(t, got, "")
				return
			}

			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestExpandVersionNeverYieldsEmptyForAReference(t *testing.T) {
	// The whole point of not using os.Expand: an unknown name must not become
	// "" and flow into a download URL.
	for _, in := range []string{"$UNKNOWN", "${UNKNOWN}", "prefix-$UNKNOWN"} {
		got, err := ExpandVersion(in, "--version", func(string) (string, bool) { return "", false })
		assert.Assert(t, err != nil, "%q expanded to %q instead of erroring", in, got)
	}
}

func TestExpandVersionDefaultsToProcessEnvironment(t *testing.T) {
	t.Setenv("SETUP_BROWSER_TOOLS_TEST_VERSION", "1.2.3")

	got, err := ExpandVersion("$SETUP_BROWSER_TOOLS_TEST_VERSION", "--version", nil)
	assert.NilError(t, err)
	assert.Equal(t, got, "1.2.3")
}
