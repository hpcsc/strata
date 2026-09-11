//go:build unit

package diff_test

import (
	"strings"
	"testing"

	"github.com/hpcsc/strata/internal/diff"
	"github.com/stretchr/testify/require"
)

func TestChangedWords(t *testing.T) {
	marked := func(line string, ranges []diff.Range) []string {
		var out []string
		for _, r := range ranges {
			out = append(out, strings.TrimSpace(line[r.Start:r.End]))
		}
		return out
	}

	t.Run("marks only the word that changed on each side", func(t *testing.T) {
		deleted, added := "total := price * qty", "total := price * quantity"

		before, after := diff.ChangedWords(deleted, added)

		require.Equal(t, []string{"qty"}, marked(deleted, before))
		require.Equal(t, []string{"quantity"}, marked(added, after))
	})

	t.Run("marks an inserted word on the added side only", func(t *testing.T) {
		deleted, added := "return nil", "return nil, err"

		before, after := diff.ChangedWords(deleted, added)

		require.Empty(t, before)
		require.Equal(t, []string{", err"}, marked(added, after))
	})

	t.Run("leaves lines that share little unmarked", func(t *testing.T) {
		before, after := diff.ChangedWords("if err != nil {", "for _, item := range items {")

		require.Empty(t, before)
		require.Empty(t, after)
	})

	t.Run("gives byte ranges that stay correct after multi-byte characters", func(t *testing.T) {
		deleted, added := `label := "café au lait"`, `label := "café au thé"`

		before, after := diff.ChangedWords(deleted, added)

		require.Equal(t, []string{"lait"}, marked(deleted, before))
		require.Equal(t, []string{"thé"}, marked(added, after))
	})
}
