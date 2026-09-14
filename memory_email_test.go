package notify_test

import (
	"testing"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/ntfy/ntfytest"
)

func TestMemoryStoreEmailConformance(t *testing.T) {
	t.Parallel()

	notifytest.RunEmail(t, func(*testing.T) notifytest.EmailStore { return notify.NewMemoryStore() })
	notifytest.RunEmailDispatch(t, func(*testing.T) notifytest.EmailStore { return notify.NewMemoryStore() })
}
