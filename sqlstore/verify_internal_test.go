package sqlstore

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/sqlkit"
)

// TestSQLKitStillExpectsTheOldMySQLCollation is a tripwire, not a behaviour
// test. verifyDialect exists only because the vendored sqlkit expects
// utf8mb4_0900_as_cs of MySQL identifier columns; once a refreshed copy expects
// something else, this fails, and verifyDialect is revisited and deleted.
func TestSQLKitStillExpectsTheOldMySQLCollation(t *testing.T) {
	t.Parallel()

	require.Equal(t, "utf8mb4_0900_as_cs", sqlkit.MySQL.IdentifierCollation(),
		"the vendored sqlkit changed what it expects of MySQL identifier columns: revisit verifyDialect and delete it with this test")
}

func TestVerifyDialect(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name    string
		dialect sqlkit.Dialect
		assert  func(t *testing.T, got sqlkit.Dialect)
	}

	expects := func(name, collation string) func(t *testing.T, got sqlkit.Dialect) {
		return func(t *testing.T, got sqlkit.Dialect) {
			assert.Equal(t, name, got.Name(), "the dialect's name is delegated unchanged")
			assert.Equal(t, collation, got.IdentifierCollation())
		}
	}

	cases := []testCase{
		{name: "MySQL expects binary identifier columns", dialect: sqlkit.MySQL, assert: expects("mysql", "binary")},
		{name: "PostgreSQL is unchanged", dialect: sqlkit.PostgreSQL, assert: expects("postgres", "C")},
		{name: "SQLite is unchanged", dialect: sqlkit.SQLite, assert: expects("sqlite", "BINARY")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.assert(t, verifyDialect{tc.dialect})
		})
	}
}
