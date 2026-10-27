package acceptance_test

import (
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/assert"

	"github.com/circleci-functions/toolkit/testing/runner"
)

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	r := runner.New(t, binaryPath, runner.WithUnsafeOSEnvVars())
	_, err := r.Run(args...)
	return r.StdoutString() + r.StderrString(), err
}

// subcommands is every command the descriptor should carry.
var subcommands = []string{
	"install-browser-tools",
	"install-chrome",
	"install-chromedriver",
	"install-firefox",
	"install-geckodriver",
	"install-chrome-for-testing",
	"install-edge",
	"install-edge-driver",
}

func TestHelpListsEverySubcommand(t *testing.T) {
	out, err := run(t, "--help")
	assert.NilError(t, err)

	for _, name := range subcommands {
		assert.Assert(t, strings.Contains(out, name), "--help should list %s:\n%s", name, out)
	}
}

func TestSubcommandHelpSucceeds(t *testing.T) {
	for _, name := range subcommands {
		t.Run(name, func(t *testing.T) {
			out, err := run(t, name, "--help")
			assert.NilError(t, err, out)
			assert.Assert(t, strings.Contains(out, name), out)
		})
	}
}

// descriptorFlags returns the exact flag names each subcommand publishes. Help
// text is unusable for this: kong renders a negatable bool as "--[no-]chrome",
// and a substring check for "--chromedriver" would also match
// "--chromedriver-install-dir".
func descriptorFlags(t *testing.T) map[string][]string {
	t.Helper()

	out, err := run(t, "--describe")
	assert.NilError(t, err, out)

	var d struct {
		Commands map[string]struct {
			Flags []struct {
				Name string `yaml:"name"`
			} `yaml:"flags"`
		} `yaml:"commands"`
	}
	assert.NilError(t, yaml.Unmarshal([]byte(out), &d))

	flags := map[string][]string{}
	for name, cmd := range d.Commands {
		for _, f := range cmd.Flags {
			flags[name] = append(flags[name], f.Name)
		}
		sort.Strings(flags[name])
	}
	return flags
}

func TestSubcommandFlagsExactly(t *testing.T) {
	tests := []struct {
		command string
		want    []string
	}{
		{
			command: "install-chrome",
			want:    []string{"cache-key", "channel", "replace-existing", "use-cache", "version"},
		},
		{
			command: "install-chromedriver",
			want:    []string{"install-dir"},
		},
		{
			command: "install-firefox",
			want:    []string{"cache-key", "install-dir", "use-cache", "version"},
		},
		{
			command: "install-geckodriver",
			want:    []string{"install-dir", "version"},
		},
		{
			command: "install-chrome-for-testing",
			want:    []string{"chromedriver", "install-dir", "version"},
		},
		{
			command: "install-edge",
			want:    []string{"version"},
		},
		{
			command: "install-edge-driver",
			want:    []string{"install-dir"},
		},
		{
			command: "install-browser-tools",
			want: []string{
				"chrome", "chrome-for-testing", "chrome-for-testing-driver",
				"chrome-for-testing-install-dir", "chrome-for-testing-version",
				"chrome-version", "chromedriver", "chromedriver-install-dir",
				"edge", "edge-driver", "edge-driver-install-dir", "edge-version",
				"firefox", "firefox-install-dir", "firefox-version",
				"geckodriver", "geckodriver-install-dir", "geckodriver-version",
				"replace-existing-chrome",
			},
		},
	}

	flags := descriptorFlags(t)

	for _, tc := range tests {
		t.Run(tc.command, func(t *testing.T) {
			got, ok := flags[tc.command]
			assert.Assert(t, ok, "%s missing from the descriptor", tc.command)
			sort.Strings(tc.want)
			assert.DeepEqual(t, got, tc.want)
		})
	}
}

func TestAggregateHasNineteenFlags(t *testing.T) {
	flags := descriptorFlags(t)
	assert.Equal(t, len(flags["install-browser-tools"]), 19)
}

func TestUnknownFlagFails(t *testing.T) {
	out, err := run(t, "install-chrome", "--nonsense")
	assert.Assert(t, err != nil, out)
}

func TestUnknownSubcommandFails(t *testing.T) {
	out, err := run(t, "install-safari")
	assert.Assert(t, err != nil, out)
}

func TestChannelEnumIsEnforced(t *testing.T) {
	out, err := run(t, "install-chrome", "--channel", "bogus")
	assert.Assert(t, err != nil, out)
	assert.Assert(t, strings.Contains(out, "bogus"), out)
}

func TestUnresolvableVersionVariableFails(t *testing.T) {
	// Reaching the version check means no install has been attempted, so this is
	// safe to run anywhere.
	tests := []struct {
		name string
		args []string
	}{
		{name: "install-chrome", args: []string{"install-chrome", "--version", "$SETUP_BROWSER_TOOLS_UNSET"}},
		{name: "install-firefox", args: []string{"install-firefox", "--version", "${SETUP_BROWSER_TOOLS_UNSET}"}},
		{name: "install-geckodriver", args: []string{"install-geckodriver", "--version", "$SETUP_BROWSER_TOOLS_UNSET"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := run(t, tc.args...)
			assert.Assert(t, err != nil, out)
			assert.Assert(t, strings.Contains(out, "SETUP_BROWSER_TOOLS_UNSET"),
				"the error should name the variable:\n%s", out)
		})
	}
}

func TestDescribeIsValid(t *testing.T) {
	out, err := run(t, "--describe")
	assert.NilError(t, err, out)

	var d struct {
		Name     string `yaml:"name"`
		Commands map[string]struct {
			Flags []struct {
				Name      string   `yaml:"name"`
				Type      string   `yaml:"type"`
				Default   string   `yaml:"default"`
				Enum      []string `yaml:"enum"`
				Negatable bool     `yaml:"negatable"`
			} `yaml:"flags"`
		} `yaml:"commands"`
	}
	assert.NilError(t, yaml.Unmarshal([]byte(out), &d))

	assert.Equal(t, d.Name, "setup-browser-tools")
	assert.Equal(t, len(d.Commands), len(subcommands))
	for _, name := range subcommands {
		_, ok := d.Commands[name]
		assert.Assert(t, ok, "%s missing from --describe", name)
	}

	agg := d.Commands["install-browser-tools"]
	byName := map[string]string{}
	negatable := map[string]bool{}
	for _, f := range agg.Flags {
		byName[f.Name] = f.Default
		negatable[f.Name] = f.Negatable
	}
	for _, name := range []string{"chrome", "chromedriver", "firefox", "geckodriver"} {
		assert.Equal(t, byName[name], "true", "%s should advertise default true", name)
		assert.Assert(t, negatable[name], "%s should be negatable", name)
	}

	chrome := d.Commands["install-chrome"]
	for _, f := range chrome.Flags {
		if f.Name == "channel" {
			assert.DeepEqual(t, f.Enum, []string{"stable", "beta"})
		}
	}
}

func TestNoSubcommandAdvertisesTimeout(t *testing.T) {
	out, err := run(t, "--describe")
	assert.NilError(t, err, out)
	assert.Assert(t, !strings.Contains(out, "name: timeout"),
		"timeout configures the run step, not the install:\n%s", out)
}
