package ui

import (
	"fmt"

	"github.com/hpcsc/strata/internal/diffview"
	"github.com/hpcsc/strata/internal/search"
	"github.com/hpcsc/strata/internal/syntax"
)

type diffPanel struct {
	key       string
	source    diffview.Source
	hasSource bool
	notice    string
	page      diffview.Page
	pageKey   string
	offset    int
	width     int
	height    int
	find      search.Query
	theme     syntax.Theme
	// match is the index in page.Matches of the match that n and N last
	// moved to, or -1.
	match int
}

// show displays the source that key names; the scroll position resets only
// when key changes.
func (p *diffPanel) show(key string, src diffview.Source, split bool) {
	if key != p.key {
		p.offset, p.match = 0, -1
	}
	p.key, p.source, p.hasSource, p.notice = key, src, true, ""
	p.render(split)
}

func (p *diffPanel) showNotice(key, notice string) {
	if key != p.key {
		p.offset, p.match = 0, -1
	}
	p.key, p.hasSource, p.notice, p.page, p.pageKey = key, false, notice, diffview.Page{}, ""
}

func (p *diffPanel) setSize(width, height int, split bool) {
	p.width, p.height = width, height
	p.render(split)
}

func (p *diffPanel) render(split bool) {
	if !p.hasSource || p.width <= 0 {
		return
	}
	pageKey := fmt.Sprintf("%s\x00%d\x00%t\x00%s", p.key, p.width, split, p.find)
	if pageKey == p.pageKey {
		return
	}
	p.page, p.pageKey = diffview.Render(p.source, diffview.Options{Width: p.width, Split: split, Find: p.find, Theme: p.theme}), pageKey
	p.scroll(0)
}

// setFind marks the text that matches find and moves to the first match at
// or below the top of the panel.
func (p *diffPanel) setFind(find search.Query, split bool) {
	p.find, p.match = find, -1
	p.render(split)
	if find.Empty() || len(p.page.Matches) == 0 {
		return
	}
	for i, row := range p.page.Matches {
		if row >= p.offset {
			p.showMatch(i)
			return
		}
	}
	p.showMatch(0)
}

func (p *diffPanel) nextMatch() {
	if len(p.page.Matches) == 0 {
		return
	}
	for i, row := range p.page.Matches {
		if (p.match >= 0 && i > p.match) || (p.match < 0 && row >= p.offset) {
			p.showMatch(i)
			return
		}
	}
	p.showMatch(0)
}

func (p *diffPanel) previousMatch() {
	if len(p.page.Matches) == 0 {
		return
	}
	for i := len(p.page.Matches) - 1; i >= 0; i-- {
		if (p.match >= 0 && i < p.match) || (p.match < 0 && p.page.Matches[i] < p.offset) {
			p.showMatch(i)
			return
		}
	}
	p.showMatch(len(p.page.Matches) - 1)
}

// showMatch scrolls so match i shows with a few rows above it.
func (p *diffPanel) showMatch(i int) {
	p.match = i
	p.offset = max(0, p.page.Matches[i]-3)
	p.scroll(0)
}

func (p diffPanel) findStatus() string {
	switch {
	case len(p.page.Matches) == 0:
		return "no match"
	case p.match < 0 && len(p.page.Matches) == 1:
		return "1 match"
	case p.match < 0:
		return fmt.Sprintf("%d matches", len(p.page.Matches))
	default:
		return fmt.Sprintf("%d/%d", p.match+1, len(p.page.Matches))
	}
}

func (p *diffPanel) scroll(delta int) {
	p.offset = max(0, min(p.offset+delta, len(p.page.Lines)-p.height))
}

func (p *diffPanel) toTop() {
	p.offset = 0
}

func (p *diffPanel) toBottom() {
	p.scroll(len(p.page.Lines))
}

func (p *diffPanel) nextHunk() {
	for _, start := range p.page.Hunks {
		if start > p.offset {
			p.offset = start
			p.scroll(0)
			return
		}
	}
}

func (p *diffPanel) previousHunk() {
	for i := len(p.page.Hunks) - 1; i >= 0; i-- {
		if p.page.Hunks[i] < p.offset {
			p.offset = p.page.Hunks[i]
			p.scroll(0)
			return
		}
	}
}

func (p diffPanel) lines() []string {
	if p.notice != "" {
		return []string{"  " + dimText.Render(p.notice)}
	}
	return window(p.page.Lines, p.offset, p.height)
}

func (p diffPanel) position() string {
	total := len(p.page.Lines)
	switch {
	case total <= p.height:
		return "all"
	case p.offset == 0:
		return "top"
	case p.offset+p.height >= total:
		return "end"
	default:
		return fmt.Sprintf("%d%%", (p.offset+p.height)*100/total)
	}
}
