package ntfy_test

import (
	"testing"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/ntfy/ntfytest"
)

func TestMemoryStoreEmailConformance(t *testing.T) {
	t.Parallel()

	ntfytest.RunEmail(t, func(*testing.T) ntfytest.EmailStore { return ntfy.NewMemoryStore() })
	ntfytest.RunEmailDispatch(t, func(*testing.T) ntfytest.EmailStore { return ntfy.NewMemoryStore() })
}
