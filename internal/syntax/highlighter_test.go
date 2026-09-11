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
}
