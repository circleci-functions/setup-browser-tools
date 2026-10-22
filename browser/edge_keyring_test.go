package browser

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestInstallEdgeDebianNeedsNoGPG(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "-----BEGIN PGP PUBLIC KEY BLOCK-----\n")
	}))
	defer srv.Close()

	i, calls := testInstaller(t, installerOpts{distro: DistroDebian})
	i.Endpoints.MicrosoftPackages = srv.URL
	i.Fetcher = Fetcher{HTTPClient: srv.Client()}

	var list string
	scripted := i.Runner.Executor
	i.Runner.Executor = func(ctx context.Context, name string, args, env []string, out io.Writer) (string, error) {
		if name == "cp" && len(args) == 2 && args[1] == "/etc/apt/sources.list.d/microsoft-edge-stable.list" {
			b, err := os.ReadFile(args[0])
			assert.NilError(t, err)
			list = string(b)
		}
		return scripted(ctx, name, args, env, out)
	}

	assert.NilError(t, i.installEdgeDebian(context.Background(), Latest))

	var copiedKey bool
	for _, c := range *calls {
		assert.Assert(t, c.name != "gpg", "gpg was run: %+v", *calls)
		if c.name == "cp" && len(c.args) == 2 && c.args[1] == "/usr/share/keyrings/microsoft-archive-keyring.asc" {
			copiedKey = strings.HasSuffix(c.args[0], "microsoft.asc")
		}
	}
	assert.Assert(t, copiedKey, "the armored key was not installed as a keyring: %+v", *calls)
	assert.Assert(t, strings.Contains(list, "signed-by=/usr/share/keyrings/microsoft-archive-keyring.asc"), "list was %q", list)
}
