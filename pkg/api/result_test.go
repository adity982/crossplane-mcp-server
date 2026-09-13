package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTable(t *testing.T) {
	t.Run("AlignsColumns", func(t *testing.T) {
		got := Table([]string{"NAME", "READY"}, [][]string{
			{"bucket-one", "True"},
			{"a-much-longer-name", "False"},
		})

		assert.Equal(t, ""+
			"NAME                 READY\n"+
			"bucket-one           True\n"+
			"a-much-longer-name   False", got)
	})

	t.Run("WithoutRowsStillShowsColumns", func(t *testing.T) {
		// The model needs to know which columns exist even when a list is
		// empty, so it can say "none" rather than "unknown".
		assert.Equal(t, "NAME   READY", Table([]string{"NAME", "READY"}, nil))
	})
}

func TestSection(t *testing.T) {
	t.Run("SkipsEmptyBody", func(t *testing.T) {
		var b strings.Builder
		Section(&b, "Title:", "   ")
		assert.Empty(t, b.String())
	})

	t.Run("NoLeadingBlankLineOnFirstSection", func(t *testing.T) {
		var b strings.Builder
		Section(&b, "Title:", "body")
		assert.Equal(t, "Title:\nbody", b.String())
	})

	t.Run("SeparatesFromPreviousContent", func(t *testing.T) {
		var b strings.Builder
		b.WriteString("intro")
		Section(&b, "Title:", "body")
		assert.Equal(t, "intro\n\nTitle:\nbody", b.String())
	})
}

func TestWarnings(t *testing.T) {
	t.Run("SilentWhenNone", func(t *testing.T) {
		var b strings.Builder
		Warnings(&b, nil)
		assert.Empty(t, b.String())
	})

	t.Run("CountsAndLists", func(t *testing.T) {
		var b strings.Builder
		Warnings(&b, []string{"buckets: forbidden", "instances: forbidden"})

		assert.Contains(t, b.String(), "Warnings (2):")
		assert.Contains(t, b.String(), "- buckets: forbidden")
		assert.Contains(t, b.String(), "- instances: forbidden")
	})
}

func TestOrDash(t *testing.T) {
	assert.Equal(t, "-", OrDash(""))
	assert.Equal(t, "value", OrDash("value"))
}

func TestPath(t *testing.T) {
	assert.Equal(t, "team-a/db", Path("team-a", "db"))
	assert.Equal(t, "db", Path("", "db"), "cluster scoped resources have no namespace prefix")
}

func TestResultConstructors(t *testing.T) {
	t.Run("JSONRendersPayloadAsText", func(t *testing.T) {
		result := JSON(map[string]any{"count": 1})

		assert.Contains(t, result.Text, `"count": 1`)
		assert.NotNil(t, result.Structured)
		assert.NoError(t, result.Err)
	})

	t.Run("StructuredKeepsBothViews", func(t *testing.T) {
		result := Structured("one bucket", map[string]any{"count": 1})

		assert.Equal(t, "one bucket", result.Text)
		assert.Equal(t, map[string]any{"count": 1}, result.Structured)
	})

	t.Run("ErrorfCarriesTheMessage", func(t *testing.T) {
		result := Errorf("no such cluster %q", "prod")

		assert.EqualError(t, result.Err, `no such cluster "prod"`)
		assert.Empty(t, result.Text)
	})
}
