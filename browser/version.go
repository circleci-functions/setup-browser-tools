package browser

import (
	"fmt"
	"os"
	"strings"
)

// Latest is the version value meaning "resolve the newest release available".
const Latest = "latest"

// Lookup reports the value of an environment variable and whether it is set,
// with the same contract as os.LookupEnv.
type Lookup func(name string) (string, bool)

// OSLookup reads from the process environment.
func OSLookup(name string) (string, bool) { return os.LookupEnv(name) }

// ExpandVersion substitutes environment-variable references in a version
// string, so a caller can pass a version through a variable rather than
// hard-coding it. flag names the flag the value came from and appears in error
// messages.
//
// $NAME and ${NAME} are expanded, where NAME starts with a letter or underscore
// and continues with letters, digits or underscores. A reference to a variable
// that is unset, or set to the empty string, is an error rather than an empty
// expansion, because an empty version would otherwise be built into a download
// URL and fail much further on. A '$' that begins no valid reference is left
// alone, so "$$" and "147$" pass through unchanged.
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

// parseRef reads the variable reference starting at the '$' at index i. It
// returns the variable name and the index just past the reference. ok is false
// when i begins no valid reference, in which case the '$' is a literal. An
// error is returned only for a '${' that is never closed, which is a typo
// rather than a literal.
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

// isVarName reports whether s is a valid environment-variable name.
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

// isNameByte reports whether c may appear in a variable name, where first
// indicates c is the leading byte and so may not be a digit.
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
