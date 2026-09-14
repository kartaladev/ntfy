package ntfy_test

import (
	"bufio"
	"bytes"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCoreModuleImportsOnlyTheStandardLibrary keeps the root module free of
// third-party dependencies in a non-test build. A host that takes only
// github.com/kartaladev/ntfy gets a complete single-process setup without pulling
// in a database driver, broker client or WebSocket library; those belong to the
// modules beside it.
func TestCoreModuleImportsOnlyTheStandardLibrary(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "go", "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./...")

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	out, err := cmd.Output()
	require.NoErrorf(t, err, "go list -deps failed: %s", stderr.String())

	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		dep := strings.TrimSpace(scanner.Text())
		if dep == "" {
			continue
		}

		assert.Truef(t, dep == "github.com/kartaladev/ntfy" || strings.HasPrefix(dep, "github.com/kartaladev/ntfy/"),
			"the core module must import only the standard library and itself, but depends on %q", dep)
	}

	require.NoError(t, scanner.Err())
}
