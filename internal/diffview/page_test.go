//go:build unit

package diffview_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/hpcsc/strata/internal/diff"
	"github.com/hpcsc/strata/internal/diffview"
	"github.com/hpcsc/strata/internal/syntax"
	"github.com/stretchr/testify/require"
)

func TestRender(t *testing.T) {
	replaced := func(before, after string) diff.Patch {
		return diff.Patch{
			File: diff.File{Path: "price.go", Status: diff.Modified},
			Hunks: []diff.Hunk{{
				OldStart: 1, OldLines: 2, NewStart: 1, NewLines: 2, Section: "func total()",
				Lines: []diff.Line{
					{Kind: diff.Context, Text: "func total() int {", OldNumber: 1, NewNumber: 1},
					{Kind: diff.Deletion, Text: before, OldNumber: 2},
					{Kind: diff.Addition, Text: after, NewNumber: 2},
				},
			}},
		}
	}
	plain := func(lines []string) []string {
		out := make([]string, len(lines))
		for i, l := range lines {
			out[i] = strings.TrimRight(ansi.Strip(l), " ")
		}
		return out
	}
	requireFullWidth := func(t *testing.T, page diffview.Page, width int) {
		t.Helper()
		for i, l := range page.Lines {
			require.Equal(t, width, ansi.StringWidth(l), "row %d: %q", i, ansi.Strip(l))
		}
	}
	lastBackground := regexp.MustCompile(`48;2;\d+;\d+;\d+`)
	backgroundOf := func(t *testing.T, row, word string) string {
		t.Helper()
		at := strings.Index(row, word)
		require.NotEqual(t, -1, at, "%q is not in the row", word)
		codes := lastBackground.FindAllString(row[:at], -1)
		require.NotEmpty(t, codes, "%q has no background", word)
		return codes[len(codes)-1]
	}

	t.Run("unified", func(t *testing.T) {
		t.Run("shows each line with its old and new numbers and a marker", func(t *testing.T) {
			page := diffview.Render(diffview.Source{Patch: replaced("\treturn price * qty", "\treturn price * quantity")},
				diffview.Options{Width: 60})

			require.Equal(t, []string{
				"@@ -1,2 +1,2 @@ func total()",
				"1 1 │  func total() int {",
				"2   │-     return price * qty",
				"  2 │+     return price * quantity",
			}, plain(page.Lines))
		})

		t.Run("wraps a long line onto more rows without losing its text", func(t *testing.T) {
			long := strings.Repeat("abcdefghij", 8)
			patch := diff.Patch{Hunks: []diff.Hunk{{Lines: []diff.Line{{Kind: diff.Addition, Text: long, NewNumber: 1}}}}}

			page := diffview.Render(diffview.Source{Patch: patch}, diffview.Options{Width: 40})

			var text strings.Builder
			for _, l := range plain(page.Lines[1:]) {
				text.WriteString(strings.TrimSpace(strings.TrimPrefix(strings.SplitN(l, "│", 2)[1], "+")))
			}
			require.Greater(t, len(page.Lines), 3)
			require.Equal(t, long, text.String())
			requireFullWidth(t, page, 40)
		})

		t.Run("gives the changed word a stronger background than the rest of the line", func(t *testing.T) {
			page := diffview.Render(diffview.Source{Patch: replaced("return price * qty", "return price * quantity")},
				diffview.Options{Width: 60})

			added := page.Lines[3]
			require.NotEqual(t, backgroundOf(t, added, "return"), backgroundOf(t, added, "quantity"))
		})
	})

	t.Run("split", func(t *testing.T) {
		t.Run("puts a deleted line beside the added line that replaced it", func(t *testing.T) {
			page := diffview.Render(diffview.Source{Patch: replaced("return price * qty", "return price * quantity")},
				diffview.Options{Width: 81, Split: true})

			rows := plain(page.Lines)
			require.Len(t, rows, 3)
			left, right, found := strings.Cut(rows[2], "│")
			require.True(t, found)
			require.Equal(t, "2 return price * qty", strings.TrimSpace(left))
			require.Equal(t, "2 return price * quantity", strings.TrimSpace(right))
		})

		t.Run("leaves the other side empty for a line only one side has", func(t *testing.T) {
			patch := diff.Patch{Hunks: []diff.Hunk{{Lines: []diff.Line{
				{Kind: diff.Context, Text: "a", OldNumber: 1, NewNumber: 1},
				{Kind: diff.Addition, Text: "b", NewNumber: 2},
			}}}}

			page := diffview.Render(diffview.Source{Patch: patch}, diffview.Options{Width: 41, Split: true})

			left, right, _ := strings.Cut(plain(page.Lines)[2], "│")
			require.Empty(t, strings.TrimSpace(left))
			require.Equal(t, "2 b", strings.TrimSpace(right))
		})

		t.Run("shows a file added whole in one column", func(t *testing.T) {
			patch := diff.Patch{File: diff.File{Path: "new.go", Status: diff.Added}, Hunks: []diff.Hunk{{
				NewStart: 1, NewLines: 1,
				Lines: []diff.Line{{Kind: diff.Addition, Text: "package fresh", NewNumber: 1}},
			}}}

			page := diffview.Render(diffview.Source{Patch: patch}, diffview.Options{Width: 60, Split: true})

			require.Equal(t, "  1 │+ package fresh", plain(page.Lines)[1])
		})
	})

	t.Run("rows", func(t *testing.T) {
		t.Run("every row fills the width exactly in both layouts", func(t *testing.T) {
			patch := replaced("\tlabel := \"café\" // 価格", "\tlabel := \"thé\" // "+strings.Repeat("価格", 30))
			highlighted := syntax.NewHighlighter("nord").Lines("price.go", "func total() int {\n\tlabel := \"thé\" // "+strings.Repeat("価格", 30)+"\n")

			for _, split := range []bool{false, true} {
				page := diffview.Render(diffview.Source{Patch: patch, After: highlighted}, diffview.Options{Width: 57, Split: split})
				requireFullWidth(t, page, 57)
			}
		})

		t.Run("records the row where each hunk starts", func(t *testing.T) {
			patch := diff.Patch{Hunks: []diff.Hunk{
				{Lines: []diff.Line{{Kind: diff.Addition, Text: "a", NewNumber: 1}, {Kind: diff.Addition, Text: "b", NewNumber: 2}}},
				{Lines: []diff.Line{{Kind: diff.Addition, Text: "c", NewNumber: 9}}},
			}}

			page := diffview.Render(diffview.Source{Patch: patch}, diffview.Options{Width: 40})

			require.Equal(t, []int{0, 3}, page.Hunks)
		})

		t.Run("describes a patch with no lines to show", func(t *testing.T) {
			patch := diff.Patch{File: diff.File{Path: "logo.png"}, Binary: true}

			page := diffview.Render(diffview.Source{Patch: patch}, diffview.Options{Width: 40})

			require.Equal(t, []string{"  binary file changed"}, plain(page.Lines))
		})
	})
}
