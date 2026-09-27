package sqlstore

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/kartaladev/sqlkit"
)

// mysqlIdentifierType is the only type a MySQL identifier column may have.
// VARBINARY compares and sorts by bytes and pads nothing, on every supported
// server. A character-set column compares by collation, and even a byte-exact
// collation such as utf8mb4_0900_bin needs MySQL 8.0.17; BINARY pads with NUL,
// so a stored identifier stops matching the one it was stored as.
const mysqlIdentifierType = "varbinary"

// verify compares the live schema with an expectation, reporting every
// discrepancy in one [*sqlkit.SchemaError].
//
// On MySQL, sqlkit cannot judge identifier columns: it checks their collation,
// which a binary string does not have and which cannot tell VARBINARY from
// BINARY or BLOB, and it still expects utf8mb4_0900_as_cs. So sqlkit checks
// everything else, and the store checks the identifier columns' type itself.
// That split goes once sqlkit checks MySQL identifier columns as binary strings;
// the tripwire TestSQLKitStillExpectsTheOldMySQLCollation says when to look.
func (s *Store) verify(ctx context.Context, expectation sqlkit.SchemaExpectation) error {
	ctx = s.own(ctx)

	if s.dialect.Name() != sqlkit.MySQL.Name() {
		return sqlkit.VerifySchema(ctx, s.querier, s.dialect, s.prefix, expectation)
	}

	err := sqlkit.VerifySchema(ctx, s.querier, s.dialect, s.prefix, withoutIdentifierColumns(expectation))

	var schema *sqlkit.SchemaError
	if err != nil && !errors.As(err, &schema) {
		return err
	}

	typed, typeErr := s.identifierTypeIssues(ctx, expectation)
	if typeErr != nil {
		return typeErr
	}

	if len(typed) == 0 {
		return err
	}

	if schema == nil {
		schema = &sqlkit.SchemaError{Dialect: s.dialect.Name()}
	}

	schema.Issues = append(typed, schema.Issues...)

	return schema
}

// withoutIdentifierColumns returns an expectation that asks sqlkit nothing about
// identifier columns' collation, and everything else unchanged.
func withoutIdentifierColumns(expectation sqlkit.SchemaExpectation) sqlkit.SchemaExpectation {
	out := make(sqlkit.SchemaExpectation, len(expectation))
	for table, expected := range expectation {
		expected.IdentifierColumns = nil
		out[table] = expected
	}

	return out
}

// identifierTypeIssues reports each present MySQL identifier column whose type
// is not VARBINARY. A missing table or column is sqlkit's to report.
func (s *Store) identifierTypeIssues(
	ctx context.Context, expectation sqlkit.SchemaExpectation,
) ([]sqlkit.SchemaIssue, error) {
	names := slices.Sorted(maps.Keys(expectation))

	identifiers := make(map[string][]string, len(names))
	tables := make([]any, 0, len(names))

	for _, name := range names {
		identifiers[s.prefix+name] = expectation[name].IdentifierColumns
		tables = append(tables, s.prefix+name)
	}

	w := sqlkit.NewWriter(s.dialect)
	w.Write("SELECT TABLE_NAME, COLUMN_NAME, DATA_TYPE, COLUMN_TYPE, COALESCE(COLLATION_NAME, '') ")
	w.Write("FROM information_schema.COLUMNS ")
	w.Write("WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME IN (", w.BindAll(tables...), ") ")
	w.Write("ORDER BY TABLE_NAME, ORDINAL_POSITION")
	statement := w.Done()

	rows, err := s.querier.QueryStatement(ctx, statement.SQL, statement.Args...)
	if err != nil {
		return nil, fmt.Errorf("sqlstore: read the live identifier column types: %w", err)
	}

	var (
		issues []sqlkit.SchemaIssue
		raw    = make([]any, 5)
		dest   = make([]any, 5)
		values = make([]string, 5)
	)

	for i := range raw {
		dest[i] = &raw[i]
	}

	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("sqlstore: scan an identifier column type: %w", err)
		}

		for i, value := range raw {
			if values[i], err = sqlkit.DecodeString(value); err != nil {
				return nil, err
			}
		}

		table, column, dataType, columnType, collation := values[0], values[1], values[2], values[3], values[4]
		if dataType == mysqlIdentifierType || !slices.Contains(identifiers[table], column) {
			continue
		}

		declared := fmt.Sprintf("%q", columnType)
		if collation != "" {
			declared += fmt.Sprintf(" collated %q", collation)
		}

		issues = append(issues, sqlkit.SchemaIssue{
			Table: table, Column: column,
			Detail: "type is " + declared + " but must be VARBINARY, or identifiers will not compare byte for byte",
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlstore: read the identifier column types: %w", err)
	}

	return issues, nil
}
