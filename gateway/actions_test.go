package gateway

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every action of the generated profiles is a standard action, and the set has
// nothing else: a message missing here would be labelled "other" in metrics and
// refused by an action-restricted pool (review N6).
func TestStandardActionsMatchGeneratedProfiles(t *testing.T) {
	re := regexp.MustCompile(`Action: "([A-Za-z0-9]+)"`)
	generated := map[string]struct{}{}
	for _, dir := range []string{"v16", "v201", "v21"} {
		files, err := filepath.Glob(filepath.Join("..", dir, "profiles", "*.go"))
		require.NoError(t, err)
		require.NotEmpty(t, files, dir)
		for _, f := range files {
			src, err := os.ReadFile(f)
			require.NoError(t, err)
			for _, m := range re.FindAllStringSubmatch(string(src), -1) {
				generated[m[1]] = struct{}{}
			}
		}
	}
	require.NotEmpty(t, generated)
	for a := range generated {
		require.Equal(t, a, LabelAction(a), "generated action %q is not in standardActions", a)
	}
	for a := range standardActions {
		_, ok := generated[a]
		require.True(t, ok, "standardActions has %q, which no generated profile defines", a)
	}
}
