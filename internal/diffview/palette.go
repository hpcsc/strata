package diffview

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/colorprofile"
	"github.com/hpcsc/strata/internal/diff"
	"github.com/hpcsc/strata/internal/syntax"
)

type rgb struct {
	r, g, b uint8
	set     bool
}

type style struct {
	fg     rgb
	bg     rgb
	bold   bool
	italic bool
	dim    bool
}

type palette struct {
	text           rgb
	number         rgb
	separator      rgb
	hunk           rgb
	hunkBackground rgb
	addedMarker    rgb
	deletedMarker  rgb
	addedLine      rgb
	addedWord      rgb
	deletedLine    rgb
	deletedWord    rgb
	note           rgb
	emptySide      rgb
}

var (
	foundText       = rgb{46, 52, 64, true}
	foundBackground = rgb{235, 203, 139, true}
	defaultInserted = rgb{46, 160, 67, true}
	defaultDeleted  = rgb{248, 81, 73, true}
	white           = rgb{255, 255, 255, true}
	black           = rgb{0, 0, 0, true}
	lineTint        = tint{lightness: 0.1, chroma: 0.4}
	wordTint        = tint{lightness: 0.3, chroma: 0.75}
)

func newPalette(t syntax.Theme, profile colorprofile.Profile) palette {
	background := fromSyntax(t.Background).or(white)
	ink := fromSyntax(t.Text).or(black)
	if background.dark() {
		ink = fromSyntax(t.Text).or(white)
	}
	inserted := fromSyntax(t.Inserted).or(defaultInserted)
	deleted := fromSyntax(t.Deleted).or(defaultDeleted)
	number := fromSyntax(t.LineNumber).or(blend(ink, background, 0.5))
	p := palette{
		text:           fromSyntax(t.Text),
		number:         number,
		separator:      blend(number, background, 0.5),
		hunk:           fromSyntax(t.Subheading),
		hunkBackground: blend(ink, background, 0.08),
		addedMarker:    inserted,
		deletedMarker:  deleted,
		addedLine:      lineTint.of(inserted, background),
		addedWord:      wordTint.of(inserted, background),
		deletedLine:    lineTint.of(deleted, background),
		deletedWord:    wordTint.of(deleted, background),
		note:           fromSyntax(t.Comment),
		emptySide:      blend(ink, background, 0.04),
	}
	if profile == colorprofile.ANSI256 {
		p.addedLine, p.deletedLine = sameHueIn256(p.addedLine), sameHueIn256(p.deletedLine)
		p.addedWord, p.deletedWord = sameHueIn256(p.addedWord, p.addedLine), sameHueIn256(p.deletedWord, p.deletedLine)
		p.hunkBackground, p.emptySide, p.separator = greyIn256(p.hunkBackground), greyIn256(p.emptySide), greyIn256(p.separator)
	}
	return p
}

const reset = "\x1b[0m"

func (p palette) lineBackground(kind diff.LineKind) rgb {
	switch kind {
	case diff.Addition:
		return p.addedLine
	case diff.Deletion:
		return p.deletedLine
	default:
		return rgb{}
	}
}

func (p palette) wordBackground(kind diff.LineKind) rgb {
	switch kind {
	case diff.Addition:
		return p.addedWord
	case diff.Deletion:
		return p.deletedWord
	default:
		return rgb{}
	}
}

func (p palette) marker(kind diff.LineKind) (string, rgb) {
	switch kind {
	case diff.Addition:
		return "+", p.addedMarker
	case diff.Deletion:
		return "-", p.deletedMarker
	default:
		return " ", rgb{}
	}
}

func fromSyntax(c syntax.Color) rgb {
	return rgb{c.R, c.G, c.B, c.Set}
}

func (c rgb) or(fallback rgb) rgb {
	if c.set {
		return c
	}
	return fallback
}

func sgr(s style) string {
	var b strings.Builder
	b.WriteString("\x1b[0")
	if s.bold {
		b.WriteString(";1")
	}
	if s.dim {
		b.WriteString(";2")
	}
	if s.italic {
		b.WriteString(";3")
	}
	if s.fg.set {
		fmt.Fprintf(&b, ";38;2;%d;%d;%d", s.fg.r, s.fg.g, s.fg.b)
	}
	if s.bg.set {
		fmt.Fprintf(&b, ";48;2;%d;%d;%d", s.bg.r, s.bg.g, s.bg.b)
	}
	b.WriteString("m")
	return b.String()
}

func paintText(text string, s style) string {
	return sgr(s) + text + reset
}
