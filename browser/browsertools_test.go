package browser

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestBrowserToolsDispatchOrder(t *testing.T) {
	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})

	// The orb dispatches in this order, and it matters: ChromeDriver reads the
	// installed Chrome, and Edge WebDriver reads the installed Edge.
	assert.DeepEqual(t, i.BrowserToolsOrder(), []string{
		"Firefox", "geckodriver", "Chrome for Testing", "Chrome", "ChromeDriver",
		"Microsoft Edge", "Edge WebDriver",
	})
}

// stubComponents replaces the install functions so the policy can be tested
// without touching a network or a package manager.
func stubComponents(i *Installer, results map[string]error, ran *[]string) {
	i.componentsOverride = func(opts BrowserToolsOptions) []component {
		var out []component
		for _, c := range i.defaultComponents(opts) {
			name := c.Name
			flag := c.Flag
			enabled := c.Enabled
			out = append(out, component{
				Name: name, Flag: flag, Enabled: enabled,
				Install: func(context.Context) error {
					*ran = append(*ran, name)
					return results[name]
				},
			})
		}
		return out
	}
}

func TestBrowserToolsUnsupportedPolicy(t *testing.T) {
	unsupported := Unsupported("Firefox", Platform{Distro: DistroAlpine, Arch: ArchAMD64},
		"Mozilla publishes no Alpine build")

	tests := []struct {
		name       string
		explicit   ExplicitFlags
		results    map[string]error
		wantErr    bool
		wantRan    []string
		wantWarned string
	}{
		{
			name:     "unsupported and only on by default: warn and carry on",
			explicit: ExplicitFlags{},
			results:  map[string]error{"Firefox": unsupported},
			wantRan:  []string{"Firefox", "geckodriver", "Chrome", "ChromeDriver"},

			wantWarned: "skipping Firefox",
		},
		{
			name:     "unsupported and explicitly requested: fail",
			explicit: ExplicitFlags{"firefox": true},
			results:  map[string]error{"Firefox": unsupported},
			wantErr:  true,
			wantRan:  []string{"Firefox"},
		},
		{
			name:     "a genuine failure is fatal even when only on by default",
			explicit: ExplicitFlags{},
			results:  map[string]error{"Firefox": errors.New("download failed")},
			wantErr:  true,
			wantRan:  []string{"Firefox"},
		},
		{
			name:     "a genuine failure is fatal when explicit too",
			explicit: ExplicitFlags{"firefox": true},
			results:  map[string]error{"Firefox": errors.New("checksum mismatch")},
			wantErr:  true,
			wantRan:  []string{"Firefox"},
		},
		{
			name:     "everything succeeding runs the whole default set",
			explicit: ExplicitFlags{},
			results:  map[string]error{},
			wantRan:  []string{"Firefox", "geckodriver", "Chrome", "ChromeDriver"},
		},
		{
			name:     "several unsupported components are each skipped",
			explicit: ExplicitFlags{},
			results: map[string]error{
				"Firefox":     unsupported,
				"geckodriver": unsupported,
			},
			wantRan: []string{"Firefox", "geckodriver", "Chrome", "ChromeDriver"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var log bytes.Buffer
			i, _ := testInstaller(t, installerOpts{distro: DistroDebian})
			i.Out = &log

			var ran []string
			stubComponents(i, tc.results, &ran)

			err := i.InstallBrowserTools(context.Background(), BrowserToolsOptions{
				Chrome: true, ChromeDriver: true, Firefox: true, Geckodriver: true,
				Explicit: tc.explicit,
			})

			if tc.wantErr {
				assert.Assert(t, err != nil)
			} else {
				assert.NilError(t, err)
			}
			assert.DeepEqual(t, ran, tc.wantRan)
			if tc.wantWarned != "" {
				assert.Assert(t, strings.Contains(log.String(), tc.wantWarned),
					"log %q should mention %q", log.String(), tc.wantWarned)
			}
		})
	}
}

func TestBrowserToolsDefaultOffComponentsAlwaysFailWhenUnsupported(t *testing.T) {
	// The four default-off booleans are only ever true because the caller said
	// so, so for them the explicit/default distinction collapses.
	unsupported := Unsupported("Microsoft Edge", Platform{Distro: DistroAlpine, Arch: ArchAMD64}, "")

	var log bytes.Buffer
	i, _ := testInstaller(t, installerOpts{distro: DistroAlpine})
	i.Out = &log

	var ran []string
	stubComponents(i, map[string]error{"Microsoft Edge": unsupported}, &ran)

	err := i.InstallBrowserTools(context.Background(), BrowserToolsOptions{
		Edge:     true,
		Explicit: ExplicitFlags{"edge": true},
	})
	assert.Assert(t, err != nil, "an explicitly requested Edge must fail on Alpine")
}

func TestBrowserToolsDisabledComponentsNeverRun(t *testing.T) {
	i, _ := testInstaller(t, installerOpts{distro: DistroDebian})

	var ran []string
	stubComponents(i, map[string]error{}, &ran)

	assert.NilError(t, i.InstallBrowserTools(context.Background(), BrowserToolsOptions{
		Chrome: true, ChromeDriver: true,
		Firefox: false, Geckodriver: false,
		Explicit: ExplicitFlags{"firefox": true, "geckodriver": true},
	}))

	assert.DeepEqual(t, ran, []string{"Chrome", "ChromeDriver"})
}

func TestExplicitFlagsHas(t *testing.T) {
	e := ExplicitFlags{"firefox": true}
	assert.Assert(t, e.Has("firefox"))
	assert.Assert(t, !e.Has("chrome"))

	var nilFlags ExplicitFlags
	assert.Assert(t, !nilFlags.Has("firefox"), "a nil map must not panic")
}
