package ntfy_test

import (
	"testing"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/ntfy/ntfytest"
)

func TestMemoryStoreConformance(t *testing.T) {
	t.Parallel()

	ntfytest.Run(t, func(*testing.T) ntfy.Store { return ntfy.NewMemoryStore() })
}
