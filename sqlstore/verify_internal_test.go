package sqlstore

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kartaladev/sqlkit"
)

// TestSQLKitStillExpectsTheOldMySQLCollation is a tripwire, not a behaviour
// test. The store checks MySQL identifier columns' type itself, in verify,
// only because the vendored sqlkit expects utf8mb4_0900_as_cs of them. Once a
// refreshed copy expects something else, this fails: see whether sqlkit now
// checks MySQL identifier columns as binary strings, and if so hand that back
// to it and delete this test.
func TestSQLKitStillExpectsTheOldMySQLCollation(t *testing.T) {
	t.Parallel()

	require.Equal(t, "utf8mb4_0900_as_cs", sqlkit.MySQL.IdentifierCollation(),
		"the vendored sqlkit changed what it expects of MySQL identifier columns: revisit Store.verify and this test")
}
