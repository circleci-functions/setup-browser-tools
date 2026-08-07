package browser

import (
	"fmt"
	"os"
	"strings"
)

const Latest = "latest"

// Lookup has the same contract as os.LookupEnv.
type Lookup func(name string) (string, bool)

func OSLookup(name string) (string, bool) { return os.LookupEnv(name) }

// ExpandVersion substitutes $NAME and ${NAME} in a version string, so a caller
// can pass a version through an environment variable. flag names the flag the
// value came from, for error messages.
//
// A reference to a variable that is unset, or set to empty, is an error rather
// than an empty expansion. This is why os.Expand is unusable here: its mapping
// function cannot report failure, so a mistyped $CHROME_VERSON would become ""
// and get built into a download URL.
//
// A '$' beginning no valid reference is left alone, so "$$" and "147$" pass
// through unchanged.
func ExpandVersion(value, flag string, lookup Lookup) (string, error) {
	if !strings.Contains(value, "$") {
		return value, nil
	}
	if lookup == nil {
		lookup = OSLookup
	}

	var b strings.Builder
	for i := 0; i < len(value); {
		c := value[i]
		if c != '$' {
			b.WriteByte(c)
			i++
			continue
		}

		name, next, ok, err := parseRef(value, i, flag)
		if err != nil {
			return "", err
		}
		if !ok {
			b.WriteByte(c)
			i++
			continue
		}

		v, set := lookup(name)
		if !set {
			return "", fmt.Errorf("%s: $%s is not set", flag, name)
		}
		if v == "" {
			return "", fmt.Errorf("%s: $%s is set but empty", flag, name)
		}
		b.WriteString(v)
		i = next
	}

	return b.String(), nil
}

// parseRef reads the reference starting at the '$' at index i, returning the
// name and the index just past it. ok is false when i begins no valid
// reference, making the '$' a literal. An unclosed "${" errors instead, being a
// typo rather than plausible literal text.
func parseRef(value string, i int, flag string) (name string, next int, ok bool, err error) {
	rest := value[i+1:]

	if strings.HasPrefix(rest, "{") {
		end := strings.IndexByte(rest, '}')
		if end < 0 {
			return "", 0, false, fmt.Errorf("%s: unterminated ${ in %q", flag, value)
		}
		inner := rest[1:end]
		if !isVarName(inner) {
			return "", 0, false, fmt.Errorf("%s: invalid variable name %q in %q", flag, inner, value)
		}
		return inner, i + 1 + end + 1, true, nil
	}

	end := 0
	for end < len(rest) && isNameByte(rest[end], end == 0) {
		end++
	}
	if end == 0 {
		return "", 0, false, nil
	}
	return rest[:end], i + 1 + end, true, nil
}

func isVarName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isNameByte(s[i], i == 0) {
			return false
		}
	}
	return true
}

func isNameByte(c byte, first bool) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c == '_':
		return true
	case c >= '0' && c <= '9':
		return !first
	default:
		return false
	}
}
