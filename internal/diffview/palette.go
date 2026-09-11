package diffview

import (
	"fmt"
	"strings"

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

var (
	textColor       = rgb{216, 222, 233, true}
	numberColor     = rgb{76, 86, 106, true}
	separatorColor  = rgb{59, 66, 82, true}
	hunkColor       = rgb{129, 161, 193, true}
	hunkBackground  = rgb{43, 48, 59, true}
	addedMarker     = rgb{163, 190, 140, true}
	deletedMarker   = rgb{191, 97, 106, true}
	addedLine       = rgb{33, 53, 40, true}
	addedWord       = rgb{47, 94, 62, true}
	deletedLine     = rgb{58, 34, 39, true}
	deletedWord     = rgb{112, 46, 56, true}
	noteColor       = rgb{136, 192, 208, true}
	emptySideFiller = rgb{35, 39, 47, true}
)

const reset = "\x1b[0m"

func lineBackground(kind diff.LineKind) rgb {
	switch kind {
	case diff.Addition:
		return addedLine
	case diff.Deletion:
		return deletedLine
	default:
		return rgb{}
	}
}

func wordBackground(kind diff.LineKind) rgb {
	switch kind {
	case diff.Addition:
		return addedWord
	case diff.Deletion:
		return deletedWord
	default:
		return rgb{}
	}
}

func marker(kind diff.LineKind) (string, rgb) {
	switch kind {
	case diff.Addition:
		return "+", addedMarker
	case diff.Deletion:
		return "-", deletedMarker
	default:
		return " ", rgb{}
	}
}

func fromSyntax(c syntax.Color) rgb {
	return rgb{c.R, c.G, c.B, c.Set}
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
