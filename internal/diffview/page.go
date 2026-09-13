package diffview

import (
	"github.com/hpcsc/strata/internal/diff"
	"github.com/hpcsc/strata/internal/search"
	"github.com/hpcsc/strata/internal/syntax"
)

type Options struct {
	Width int
	Split bool
	// Find marks the text that matches it.
	Find  search.Query
	Theme syntax.Theme
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
	r := newRenderer(src, max(opts.Width, minWidth), opts.Find, opts.Theme)
	if summary := src.Patch.Summary(); summary != "" {
		return Page{Lines: []string{r.note(summary)}}
	}
	split := opts.Split && src.Patch.File.Status != diff.Added && src.Patch.File.Status != diff.Deleted
	var page Page
	for _, h := range src.Patch.Hunks {
		page.Hunks = append(page.Hunks, len(page.Lines))
		page.Lines = append(page.Lines, r.hunkHeader(h))
		var rows []string
		var matches []int
		if split {
			rows, matches = r.split(h)
		} else {
			rows, matches = r.unified(h)
		}
		for _, m := range matches {
			page.Matches = append(page.Matches, len(page.Lines)+m)
		}
		page.Lines = append(page.Lines, rows...)
	}
	return page
}
