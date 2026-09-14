package notify_test

import (
	"testing"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/ntfy/ntfytest"
)

func TestMemoryStoreConformance(t *testing.T) {
	t.Parallel()

	notifytest.Run(t, func(*testing.T) notify.Store { return notify.NewMemoryStore() })
}
