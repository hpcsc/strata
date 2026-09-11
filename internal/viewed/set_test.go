//go:build unit

package viewed_test

import (
	"path/filepath"
	"testing"

	"github.com/hpcsc/strata/internal/diff"
	"github.com/hpcsc/strata/internal/viewed"
	"github.com/stretchr/testify/require"
)

func TestSet(t *testing.T) {
	file := diff.File{Path: "orders/order.go", Status: diff.Modified, OldBlob: "aaa", NewBlob: "bbb"}

	t.Run("toggle", func(t *testing.T) {
		t.Run("a file marked viewed stays viewed after the set loads again", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "strata", "viewed")
			set, err := viewed.Load(path)
			require.NoError(t, err)

			require.NoError(t, set.Toggle(file))

			again, err := viewed.Load(path)
			require.NoError(t, err)
			require.True(t, again.Has(file))
		})

		t.Run("toggling a viewed file clears its mark", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "viewed")
			set, err := viewed.Load(path)
			require.NoError(t, err)
			require.NoError(t, set.Toggle(file))

			require.NoError(t, set.Toggle(file))

			again, err := viewed.Load(path)
			require.NoError(t, err)
			require.False(t, again.Has(file))
		})
	})

	t.Run("has", func(t *testing.T) {
		t.Run("a mark stops applying once the file changes again", func(t *testing.T) {
			set, err := viewed.Load(filepath.Join(t.TempDir(), "viewed"))
			require.NoError(t, err)
			require.NoError(t, set.Toggle(file))

			changed := file
			changed.NewBlob = "ccc"

			require.False(t, set.Has(changed))
		})

		t.Run("a set with no file on disk has no marks", func(t *testing.T) {
			set, err := viewed.Load(filepath.Join(t.TempDir(), "missing", "viewed"))

			require.NoError(t, err)
			require.False(t, set.Has(file))
		})
	})
}
