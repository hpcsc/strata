//go:build unit

package search_test

import (
	"testing"

	"github.com/hpcsc/strata/internal/search"
	"github.com/stretchr/testify/require"
)

func TestQuery(t *testing.T) {
	t.Run("matches", func(t *testing.T) {
		t.Run("a query in lower case ignores case", func(t *testing.T) {
			require.True(t, search.New("store").Matches("type Store interface"))
		})

		t.Run("a query with a capital letter matches case exactly", func(t *testing.T) {
			query := search.New("Store")

			require.True(t, query.Matches("type Store interface"))
			require.False(t, query.Matches("h.store.Load(id)"))
		})

		t.Run("an empty query matches anything", func(t *testing.T) {
			require.True(t, search.New("").Matches("anything"))
		})
	})

	t.Run("ranges", func(t *testing.T) {
		t.Run("finds every match from left to right", func(t *testing.T) {
			line := "store Store STORE"

			ranges := search.New("store").Ranges(line)

			require.Equal(t, []search.Range{{Start: 0, End: 5}, {Start: 6, End: 11}, {Start: 12, End: 17}}, ranges)
		})

		t.Run("gives byte ranges that stay correct after multi-byte characters", func(t *testing.T) {
			line := "café CAFÉ"

			ranges := search.New("café").Ranges(line)

			require.Len(t, ranges, 2)
			require.Equal(t, "CAFÉ", line[ranges[1].Start:ranges[1].End])
		})

		t.Run("an empty query has no ranges", func(t *testing.T) {
			require.Empty(t, search.New("").Ranges("anything"))
		})
	})
}
