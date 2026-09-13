//go:build unit

package syntax_test

import (
	"strings"
	"testing"

	"github.com/hpcsc/strata/internal/syntax"
	"github.com/stretchr/testify/require"
)

func TestHighlighter(t *testing.T) {
	text := func(spans []syntax.Span) string {
		var b strings.Builder
		for _, s := range spans {
			b.WriteString(s.Text)
		}
		return b.String()
	}
	spanFor := func(t *testing.T, spans []syntax.Span, word string) syntax.Span {
		t.Helper()
		for _, s := range spans {
			if strings.TrimSpace(s.Text) == word {
				return s
			}
		}
		require.Failf(t, "no span", "no span holds %q in %v", word, spans)
		return syntax.Span{}
	}

	t.Run("lines", func(t *testing.T) {
		t.Run("gives one entry per line, holding that line's text", func(t *testing.T) {
			content := "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"

			lines := syntax.NewHighlighter("nord").Lines("main.go", content)

			require.Len(t, lines, 5)
			require.Equal(t, "package main", text(lines[0]))
			require.Equal(t, "", text(lines[1]))
			require.Equal(t, "\tprintln(\"hi\")", text(lines[3]))
		})

		t.Run("colours a keyword differently from a name", func(t *testing.T) {
			lines := syntax.NewHighlighter("nord").Lines("main.go", "func main() {}\n")

			require.NotEqual(t, spanFor(t, lines[0], "func").Color, spanFor(t, lines[0], "main").Color)
		})

		t.Run("keeps a comment that spans lines coloured on every line", func(t *testing.T) {
			lines := syntax.NewHighlighter("nord").Lines("main.go", "/* one\ntwo */\nvar x = 1\n")

			require.Equal(t, spanFor(t, lines[0], "/* one").Color, spanFor(t, lines[1], "two */").Color)
		})

		t.Run("gives an unknown file type its lines as text", func(t *testing.T) {
			lines := syntax.NewHighlighter("nord").Lines("notes.unknown-extension", "first\r\nsecond\r\n")

			require.Len(t, lines, 2)
			require.Equal(t, "first", text(lines[0]))
			require.Equal(t, "second", text(lines[1]))
		})
	})

	t.Run("theme", func(t *testing.T) {
		rgb := func(r, g, b uint8) syntax.Color {
			return syntax.Color{R: r, G: g, B: b, Set: true}
		}

		t.Run("takes the colours for text, changed lines, hunk headers, line numbers and comments from the style", func(t *testing.T) {
			theme := syntax.NewHighlighter("catppuccin-mocha").Theme()

			require.Equal(t, syntax.Theme{
				Text:       rgb(0xcd, 0xd6, 0xf4),
				Background: rgb(0x1e, 0x1e, 0x2e),
				Inserted:   rgb(0xa6, 0xe3, 0xa1),
				Deleted:    rgb(0xf3, 0x8b, 0xa8),
				Subheading: rgb(0xfa, 0xb3, 0x87),
				LineNumber: rgb(0x7f, 0x84, 0x9c),
				Comment:    rgb(0x6c, 0x70, 0x86),
			}, theme)
		})

		t.Run("takes a changed line's colour from its background when the style colours that instead of the text", func(t *testing.T) {
			theme := syntax.NewHighlighter("gruvbox").Theme()

			require.Equal(t, []syntax.Color{rgb(0xb8, 0xbb, 0x26), rgb(0xfb, 0x49, 0x34)}, []syntax.Color{theme.Inserted, theme.Deleted})
		})

		t.Run("gives no colour for a changed line the style marks in its plain text colour", func(t *testing.T) {
			theme := syntax.NewHighlighter("ashen").Theme()

			require.Equal(t, syntax.Color{}, theme.Inserted)
		})

		t.Run("gives no colours for changed lines when the style has none", func(t *testing.T) {
			theme := syntax.NewHighlighter("vs").Theme()

			require.Equal(t, []syntax.Color{{}, {}}, []syntax.Color{theme.Inserted, theme.Deleted})
		})
	})
}
