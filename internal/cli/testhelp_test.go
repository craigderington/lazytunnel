package cli

import (
	"testing"

	"github.com/spf13/viper"
)

// withTestServerURL sets the server URL via viper for testing and restores
// the previous value via t.Cleanup. This is shared between export and import tests.
//
// NOTE: This helper has a limitation due to viper's architecture. The viper.Set()
// call writes to an override map that is consulted ahead of pflag bindings. Once
// any test calls viper.Set(), that key is pinned with an override entry for the
// rest of the test binary's process. The cleanup restores the VALUE to its
// previous state, but cannot remove the override entry. Tests that rely on --server
// flag parsing being observable through viper.GetString() will need a fresh
// viper.New() instance instead.
func withTestServerURL(t *testing.T, url string) {
	t.Helper()
	prevServer := viper.GetString("server")
	viper.Set("server", url)
	t.Cleanup(func() { viper.Set("server", prevServer) })
}
