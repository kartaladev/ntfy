package ntfy_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kartaladev/ntfy"
)

func TestErrorsMatchTheirSentinels(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		err    error
		assert func(t *testing.T, err error)
	}

	cases := []testCase{
		{
			name: "a configuration error matches ErrConfiguration and says what is wrong",
			err:  &ntfy.ConfigurationError{Detail: "a store is required"},
			assert: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ntfy.ErrConfiguration)
				assert.NotErrorIs(t, err, ntfy.ErrValidation)
				assert.Contains(t, err.Error(), "a store is required")
			},
		},
		{
			name: "a validation error matches ErrValidation and lists every issue",
			err: &ntfy.ValidationError{Subject: "draft", Issues: []ntfy.ValidationIssue{
				{Pointer: "/recipient", Detail: "is required"},
				{Pointer: "/kind", Detail: "is longer than 100 bytes"},
			}},
			assert: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ntfy.ErrValidation)
				assert.NotErrorIs(t, err, ntfy.ErrConfiguration)
				assert.Contains(t, err.Error(), "/recipient: is required")
				assert.Contains(t, err.Error(), "/kind: is longer than 100 bytes")
			},
		},
		{
			name: "a validation error with no issues still reads as invalid",
			err:  &ntfy.ValidationError{Subject: "request"},
			assert: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ntfy.ErrValidation)
				assert.Contains(t, err.Error(), "request is not valid")
			},
		},
		{
			name: "every sentinel is distinct",
			err:  ntfy.ErrNotFound,
			assert: func(t *testing.T, err error) {
				sentinels := []error{
					ntfy.ErrNotFound, ntfy.ErrValidation, ntfy.ErrUnauthorized,
					ntfy.ErrConfiguration, ntfy.ErrTooManyStreams, ntfy.ErrUnavailable,
				}

				for i, a := range sentinels {
					for j, b := range sentinels {
						assert.Equalf(t, i == j, errors.Is(a, b), "%v against %v", a, b)
					}
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.assert(t, tc.err)
		})
	}
}
