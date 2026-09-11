package diffview

import (
	"github.com/hpcsc/strata/internal/diff"
	"github.com/hpcsc/strata/internal/syntax"
)

type Options struct {
	Width int
	Split bool
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
}

const minWidth = 24

func Render(src Source, opts Options) Page {
	r := newRenderer(src, max(opts.Width, minWidth))
	if summary := src.Patch.Summary(); summary != "" {
		return Page{Lines: []string{r.note(summary)}}
	}
	split := opts.Split && src.Patch.File.Status != diff.Added && src.Patch.File.Status != diff.Deleted
	var page Page
	for _, h := range src.Patch.Hunks {
		page.Hunks = append(page.Hunks, len(page.Lines))
		page.Lines = append(page.Lines, r.hunkHeader(h))
		if split {
			page.Lines = append(page.Lines, r.split(h)...)
		} else {
			page.Lines = append(page.Lines, r.unified(h)...)
		}
	}
	return page
}
