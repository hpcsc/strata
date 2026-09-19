package diffview

import (
	"github.com/charmbracelet/colorprofile"
	"github.com/hpcsc/strata/internal/diff"
	"github.com/hpcsc/strata/internal/search"
	"github.com/hpcsc/strata/internal/syntax"
)

type Options struct {
	Width int
	Split bool
	// WholeFile shows the lines of the file that the patch leaves out.
	WholeFile bool
	// Find marks the text that matches it.
	Find         search.Query
	Theme        syntax.Theme
	ColorProfile colorprofile.Profile
}

// Source is a patch with the highlighted text of the file before and after
// the change; entry i of Before and After holds line i+1.
type Source struct {
	Patch  diff.Patch
	Before [][]syntax.Span
	After  [][]syntax.Span
}

type Page struct {
	// Lines are terminal rows, each exactly Options.Width cells wide.
	Lines []string
	// Hunks holds the index in Lines where each hunk starts.
	Hunks []int
	// Matches holds the index in Lines of each row that shows a match of
	// Options.Find.
	Matches []int
}

const minWidth = 24

func Render(src Source, opts Options) Page {
	width, palette := max(opts.Width, minWidth), newPalette(opts.Theme, opts.ColorProfile)
	if summary := src.Patch.Summary(); summary != "" {
		return Page{Lines: []string{newRenderer(src, nil, width, opts.Find, palette).note(summary)}}
	}
	blocks, whole, note := hunkBlocks(src.Patch), false, ""
	if opts.WholeFile {
		if built, ok := wholeBlocks(src); ok {
			blocks, whole = built, true
		} else {
			note = "the file is too big to show whole"
		}
	}
	r := newRenderer(src, blocks, width, opts.Find, palette)
	split := opts.Split && src.Patch.File.Status != diff.Added && src.Patch.File.Status != diff.Deleted
	var page Page
	if note != "" {
		page.Lines = append(page.Lines, r.note(note))
	}
	for _, b := range blocks {
		if b.hunk != nil {
			page.Hunks = append(page.Hunks, len(page.Lines))
			if !whole {
				page.Lines = append(page.Lines, r.hunkHeader(*b.hunk))
			}
		}
		rows, matches := r.rows(b.lines, split)
		for _, m := range matches {
			page.Matches = append(page.Matches, len(page.Lines)+m)
		}
		page.Lines = append(page.Lines, rows...)
	}
	return page
}
