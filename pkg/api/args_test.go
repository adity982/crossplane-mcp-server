package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArgsRequiredString(t *testing.T) {
	cases := map[string]struct {
		raw     map[string]any
		want    string
		wantErr string
	}{
		"Present":   {raw: map[string]any{"name": "example"}, want: "example"},
		"Missing":   {raw: map[string]any{}, wantErr: `missing required argument "name"`},
		"Null":      {raw: map[string]any{"name": nil}, wantErr: `missing required argument "name"`},
		"WrongType": {raw: map[string]any{"name": 42.0}, wantErr: `argument "name" must be a string`},
		"Blank":     {raw: map[string]any{"name": "   "}, wantErr: `argument "name" must not be empty`},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			args := NewArgs(tc.raw)
			got := args.String("name")

			if tc.wantErr != "" {
				require.Error(t, args.Err())
				assert.Contains(t, args.Err().Error(), tc.wantErr)
				return
			}
			require.NoError(t, args.Err())
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestArgsOptionalDefaults(t *testing.T) {
	args := NewArgs(nil)

	assert.Equal(t, "fallback", args.OptionalString("kind", "fallback"))
	assert.True(t, args.OptionalBool("manifest", true))
	assert.Equal(t, 500, args.OptionalInt("limit", 500))
	assert.NoError(t, args.Err(), "omitted optional arguments are not an error")
}

func TestArgsOptionalInt(t *testing.T) {
	// JSON numbers arrive as float64 over the wire, but a Go caller in a test
	// may pass a plain int.
	args := NewArgs(map[string]any{"fromJSON": 25.0, "fromGo": 25})

	assert.Equal(t, 25, args.OptionalInt("fromJSON", 0))
	assert.Equal(t, 25, args.OptionalInt("fromGo", 0))
	assert.NoError(t, args.Err())
}

func TestArgsOptionalEnum(t *testing.T) {
	t.Run("MatchesCaseInsensitively", func(t *testing.T) {
		args := NewArgs(map[string]any{"status": "NOT-READY"})
		assert.Equal(t, "not-ready", args.OptionalEnum("status", "any", "any", "ready", "not-ready"))
		assert.NoError(t, args.Err())
	})

	t.Run("RejectsUnknownValues", func(t *testing.T) {
		args := NewArgs(map[string]any{"status": "banana"})
		args.OptionalEnum("status", "any", "any", "ready")
		require.Error(t, args.Err())
		assert.Contains(t, args.Err().Error(), `argument "status" must be one of any, ready`)
	})
}

func TestArgsAccumulatesEveryError(t *testing.T) {
	// Reporting all the mistakes at once saves the model a round trip per
	// argument.
	args := NewArgs(map[string]any{"kind": 1.0, "limit": "ten"})
	args.String("name")
	args.OptionalString("kind", "")
	args.OptionalInt("limit", 0)

	err := args.Err()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `missing required argument "name"`)
	assert.Contains(t, err.Error(), `argument "kind" must be a string`)
	assert.Contains(t, err.Error(), `argument "limit" must be a number`)
}

func TestTable(t *testing.T) {
	got := Table([]string{"NAME", "READY"}, [][]string{
		{"bucket-one", "True"},
		{"a-much-longer-name", "False"},
	})

	assert.Equal(t, ""+
		"NAME                 READY\n"+
		"bucket-one           True\n"+
		"a-much-longer-name   False", got)
}

func TestTableWithoutRowsStillShowsColumns(t *testing.T) {
	assert.Equal(t, "NAME   READY", Table([]string{"NAME", "READY"}, nil))
}
