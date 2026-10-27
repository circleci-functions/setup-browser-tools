//go:build acceptance

package acceptance_test

import (
	"testing"

	"gotest.tools/v3/assert"
)

// TestAcceptanceGeckodriver is the smallest real install, and so the one that
// shows the harness works on an executor: geckodriver is published for every
// platform the suite is built for and needs no browser alongside it.
func TestAcceptanceGeckodriver(t *testing.T) {
	s, run := sandbox(t)

	out, err := run(t, "install-geckodriver")
	assert.NilError(t, err, out)
	assertReportsVersion(t, s, "geckodriver", "")
}
