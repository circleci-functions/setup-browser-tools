package main

import (
	"sort"
	"testing"

	"github.com/alecthomas/kong"
	"gotest.tools/v3/assert"

	"github.com/circleci-functions/toolkit/functionyaml"
)

func parse(t *testing.T, args ...string) *kong.Context {
	t.Helper()

	var flags cli
	parser, err := kong.New(&flags,
		kong.Name("setup-browser-tools"),
		kong.Exit(func(int) { t.Fatalf("kong exited while parsing %v", args) }),
		functionyaml.Embed(),
	)
	assert.NilError(t, err)

	kCtx, err := parser.Parse(args)
	assert.NilError(t, err)
	return kCtx
}

func TestExplicitFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "no flags at all",
			args: []string{"install-browser-tools"},
			want: nil,
		},
		{
			name: "a boolean turned on explicitly",
			args: []string{"install-browser-tools", "--firefox"},
			want: []string{"firefox"},
		},
		{
			name: "the negated form counts as explicit too",
			args: []string{"install-browser-tools", "--no-firefox"},
			want: []string{"firefox"},
		},
		{
			name: "an explicit false via = syntax",
			args: []string{"install-browser-tools", "--firefox=false"},
			want: []string{"firefox"},
		},
		{
			name: "a default-off boolean",
			args: []string{"install-browser-tools", "--edge"},
			want: []string{"edge"},
		},
		{
			name: "a version flag only",
			args: []string{"install-browser-tools", "--chrome-version", "147.0.7727.55"},
			want: []string{"chrome-version"},
		},
		{
			name: "several at once",
			args: []string{"install-browser-tools", "--no-geckodriver", "--edge", "--edge-driver"},
			want: []string{"edge", "edge-driver", "geckodriver"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := explicitFlags(parse(t, tc.args...))

			var names []string
			for name := range got {
				names = append(names, name)
			}
			sort.Strings(names)
			assert.DeepEqual(t, names, tc.want)
		})
	}
}

func TestExplicitFlagsExcludesDefaults(t *testing.T) {
	// The whole mechanism exists because kong.Value.Set cannot answer this:
	// Value.ApplyDefault sets it, so it is true for defaulted flags too. Firefox
	// defaults to true, so if defaults leaked in it would appear here.
	got := explicitFlags(parse(t, "install-browser-tools", "--edge"))

	assert.Assert(t, got.Has("edge"))
	assert.Assert(t, !got.Has("firefox"), "a defaulted flag must not count as explicit")
	assert.Assert(t, !got.Has("chrome"))
	assert.Assert(t, !got.Has("chromedriver"))
	assert.Assert(t, !got.Has("geckodriver"))
	assert.Equal(t, len(got), 1)
}

func TestDescribeAdvertisesNegatableDefaults(t *testing.T) {
	// The plain-bool choice only holds up if the published descriptor still
	// reports default true for the four negatable booleans.
	var flags cli
	parser, err := kong.New(&flags, kong.Name("setup-browser-tools"), functionyaml.Embed())
	assert.NilError(t, err)

	d := functionyaml.FromKong(parser.Model)
	assert.NilError(t, functionyaml.Validate(d))

	cmd, ok := d.Commands["install-browser-tools"]
	assert.Assert(t, ok, "install-browser-tools missing from the descriptor")

	byName := map[string]functionyaml.Flag{}
	for _, f := range cmd.Flags {
		byName[f.Name] = f
	}

	for _, name := range []string{"chrome", "chromedriver", "firefox", "geckodriver"} {
		f, ok := byName[name]
		assert.Assert(t, ok, "%s missing from the descriptor", name)
		assert.Equal(t, f.Type, "bool", "%s should be a bool", name)
		assert.Equal(t, f.Default, "true", "%s should advertise default true", name)
		assert.Assert(t, f.Negatable, "%s should be negatable", name)
	}

	for _, name := range []string{"chrome-for-testing", "chrome-for-testing-driver", "edge", "edge-driver"} {
		f, ok := byName[name]
		assert.Assert(t, ok, "%s missing from the descriptor", name)
		assert.Equal(t, f.Default, "false", "%s should advertise default false", name)
		assert.Assert(t, !f.Negatable, "%s has nothing to negate", name)
	}
}

func TestDescribeCoversEverySubcommand(t *testing.T) {
	var flags cli
	parser, err := kong.New(&flags, kong.Name("setup-browser-tools"), functionyaml.Embed())
	assert.NilError(t, err)

	d := functionyaml.FromKong(parser.Model)
	for _, name := range []string{
		"install-browser-tools", "install-chrome", "install-chromedriver",
		"install-firefox", "install-geckodriver", "install-chrome-for-testing",
		"install-edge", "install-edge-driver",
	} {
		_, ok := d.Commands[name]
		assert.Assert(t, ok, "%s missing from the descriptor", name)
	}
	assert.Equal(t, len(d.Commands), 8)
}

func TestNoSubcommandCarriesATimeoutFlag(t *testing.T) {
	// timeout configures the run step's no_output_timeout, not the install, so it
	// is deliberately absent everywhere.
	var flags cli
	parser, err := kong.New(&flags, kong.Name("setup-browser-tools"), functionyaml.Embed())
	assert.NilError(t, err)

	d := functionyaml.FromKong(parser.Model)
	for name, cmd := range d.Commands {
		for _, f := range cmd.Flags {
			assert.Assert(t, f.Name != "timeout", "%s should not have a --timeout flag", name)
		}
	}
}
