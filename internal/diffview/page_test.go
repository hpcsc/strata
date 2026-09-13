//go:build unit

package diffview_test

import (
	"fmt"
	"image/color"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/hpcsc/strata/internal/diff"
	"github.com/hpcsc/strata/internal/diffview"
	"github.com/hpcsc/strata/internal/search"
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
	lastCode := func(t *testing.T, code *regexp.Regexp, row, word string) string {
		t.Helper()
		at := strings.Index(row, word)
		require.NotEqual(t, -1, at, "%q is not in the row", word)
		codes := code.FindAllString(row[:at], -1)
		require.NotEmpty(t, codes, "%q has no %v code", word, code)
		return codes[len(codes)-1]
	}
	backgroundOf := func(t *testing.T, row, word string) string {
		t.Helper()
		return lastCode(t, regexp.MustCompile(`48;2;\d+;\d+;\d+`), row, word)
	}
	foregroundOf := func(t *testing.T, row, word string) string {
		t.Helper()
		return lastCode(t, regexp.MustCompile(`38;2;\d+;\d+;\d+`), row, word)
	}
	strongestChannel := func(t *testing.T, code string) string {
		t.Helper()
		var r, g, b int
		_, err := fmt.Sscanf(code[len("48;2;"):], "%d;%d;%d", &r, &g, &b)
		require.NoError(t, err)
		switch {
		case r > g && r > b:
			return "red"
		case g > r && g > b:
			return "green"
		case b > r && b > g:
			return "blue"
		default:
			return "none"
		}
	}
	shownIn256 := func(t *testing.T, code string) string {
		t.Helper()
		var r, g, b uint8
		_, err := fmt.Sscanf(code[len("48;2;"):], "%d;%d;%d", &r, &g, &b)
		require.NoError(t, err)
		shownR, shownG, shownB, _ := colorprofile.ANSI256.Convert(color.RGBA{R: r, G: g, B: b, A: 255}).RGBA()
		return fmt.Sprintf("48;2;%d;%d;%d", shownR>>8, shownG>>8, shownB>>8)
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

		t.Run("records the rows that hold a match of the find, in both layouts", func(t *testing.T) {
			patch := diff.Patch{Hunks: []diff.Hunk{{Lines: []diff.Line{
				{Kind: diff.Context, Text: "store := newStore()", OldNumber: 1, NewNumber: 1},
				{Kind: diff.Context, Text: "other", OldNumber: 2, NewNumber: 2},
				{Kind: diff.Addition, Text: "return store.Save()", NewNumber: 3},
			}}}}

			for _, split := range []bool{false, true} {
				page := diffview.Render(diffview.Source{Patch: patch}, diffview.Options{Width: 60, Split: split, Find: search.New("store")})

				require.Equal(t, []int{1, 3}, page.Matches)
			}
		})

		t.Run("gives the found text its own background", func(t *testing.T) {
			patch := diff.Patch{Hunks: []diff.Hunk{{Lines: []diff.Line{
				{Kind: diff.Addition, Text: "return store.Save()", NewNumber: 1},
			}}}}

			page := diffview.Render(diffview.Source{Patch: patch}, diffview.Options{Width: 60, Find: search.New("store")})

			require.NotEqual(t, backgroundOf(t, page.Lines[1], "return"), backgroundOf(t, page.Lines[1], "store"))
		})

		t.Run("describes a patch with no lines to show", func(t *testing.T) {
			patch := diff.Patch{File: diff.File{Path: "logo.png"}, Binary: true}

			page := diffview.Render(diffview.Source{Patch: patch}, diffview.Options{Width: 40})

			require.Equal(t, []string{"  binary file changed"}, plain(page.Lines))
		})
	})

	t.Run("colours", func(t *testing.T) {
		rgb := func(r, g, b uint8) syntax.Color {
			return syntax.Color{R: r, G: g, B: b, Set: true}
		}
		source := diffview.Source{Patch: replaced("return price * qty", "return price * quantity")}

		t.Run("marks deleted and added lines in the theme's colours for them", func(t *testing.T) {
			theme := syntax.Theme{Inserted: rgb(163, 190, 140), Deleted: rgb(191, 97, 106)}

			page := diffview.Render(source, diffview.Options{Width: 60, Theme: theme})

			require.Equal(t, []string{"38;2;191;97;106", "38;2;163;190;140"},
				[]string{foregroundOf(t, page.Lines[2], "-"), foregroundOf(t, page.Lines[3], "+")})
		})

		t.Run("tints an added line with the theme's colour for added lines", func(t *testing.T) {
			theme := syntax.Theme{Background: rgb(0, 0, 0), Inserted: rgb(40, 80, 220)}

			page := diffview.Render(source, diffview.Options{Width: 60, Theme: theme})

			require.Equal(t, "blue", strongestChannel(t, backgroundOf(t, page.Lines[3], "return")))
		})

		t.Run("keeps the hue of a changed line's colour over a background of another hue", func(t *testing.T) {
			theme := syntax.Theme{Background: rgb(46, 52, 64), Inserted: rgb(163, 190, 140), Deleted: rgb(191, 97, 106)}

			page := diffview.Render(source, diffview.Options{Width: 60, Theme: theme})

			require.Equal(t, []string{"red", "green"}, []string{
				strongestChannel(t, backgroundOf(t, page.Lines[2], "return")),
				strongestChannel(t, backgroundOf(t, page.Lines[3], "return")),
			})
		})

		t.Run("tints deleted lines red and added lines green when the theme has no colours for them", func(t *testing.T) {
			page := diffview.Render(source, diffview.Options{Width: 60})

			require.Equal(t, []string{"red", "green"}, []string{
				strongestChannel(t, backgroundOf(t, page.Lines[2], "return")),
				strongestChannel(t, backgroundOf(t, page.Lines[3], "return")),
			})
		})

		t.Run("keeps changed lines red and green when a 256-colour terminal rounds them", func(t *testing.T) {
			theme := syntax.Theme{Background: rgb(46, 52, 64), Inserted: rgb(163, 190, 140), Deleted: rgb(191, 97, 106)}

			page := diffview.Render(source, diffview.Options{Width: 60, Theme: theme, ColorProfile: colorprofile.ANSI256})

			require.Equal(t, []string{"red", "green"}, []string{
				strongestChannel(t, shownIn256(t, backgroundOf(t, page.Lines[2], "return"))),
				strongestChannel(t, shownIn256(t, backgroundOf(t, page.Lines[3], "return"))),
			})
		})

		t.Run("keeps a changed word apart from the rest of its line when a 256-colour terminal rounds them", func(t *testing.T) {
			theme := syntax.Theme{Background: rgb(30, 30, 46), Inserted: rgb(166, 227, 161), Deleted: rgb(243, 139, 168)}

			page := diffview.Render(source, diffview.Options{Width: 60, Theme: theme, ColorProfile: colorprofile.ANSI256})

			added := page.Lines[3]
			require.NotEqual(t, shownIn256(t, backgroundOf(t, added, "return")), shownIn256(t, backgroundOf(t, added, "quantity")))
		})

		t.Run("keeps the hunk header and the empty side grey when a 256-colour terminal rounds them", func(t *testing.T) {
			theme := syntax.Theme{Text: rgb(205, 214, 244), Background: rgb(30, 30, 46)}
			patch := diff.Patch{File: diff.File{Path: "price.go", Status: diff.Modified}, Hunks: []diff.Hunk{{Lines: []diff.Line{
				{Kind: diff.Context, Text: "a", OldNumber: 1, NewNumber: 1},
				{Kind: diff.Addition, Text: "b", NewNumber: 2},
			}}}}

			page := diffview.Render(diffview.Source{Patch: patch}, diffview.Options{Width: 41, Split: true, Theme: theme, ColorProfile: colorprofile.ANSI256})

			require.Equal(t, []string{"none", "none"}, []string{
				strongestChannel(t, shownIn256(t, backgroundOf(t, page.Lines[0], "@@"))),
				strongestChannel(t, shownIn256(t, backgroundOf(t, page.Lines[2], "│"))),
			})
		})

		t.Run("puts the hunk header in the theme's subheading colour", func(t *testing.T) {
			theme := syntax.Theme{Subheading: rgb(136, 192, 208)}

			page := diffview.Render(source, diffview.Options{Width: 60, Theme: theme})

			require.Equal(t, "38;2;136;192;208", foregroundOf(t, page.Lines[0], "@@"))
		})
	})
}
