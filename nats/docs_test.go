package nats_test

import (
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy/nats"
)

// TestTheOperationsGuideMatchesTheBroadcaster keeps
// docs/realtime-operations.md from drifting away from the NATS
// broadcaster's defaults.
func TestTheOperationsGuideMatchesTheBroadcaster(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../docs/realtime-operations.md")
	require.NoError(t, err)

	document := string(raw)

	for _, stated := range []string{
		"| NATS subject | `" + nats.DefaultSubject + "` |",
		"| Signals per message | " + strconv.Itoa(nats.MaxSignalsPerMessage) + " |",
		"| NATS subscribe timeout | " + nats.DefaultSubscribeTimeout.String() + " |",
		"no queue group",
		"ephemeral signals, not durable events",
	} {
		assert.Containsf(t, document, stated, "the guide states %q", stated)
	}
}
