package ui

import (
	"fmt"

	"github.com/hpcsc/strata/internal/diffview"
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
}

// show displays the source that key names; the scroll position resets only
// when key changes.
func (p *diffPanel) show(key string, src diffview.Source, split bool) {
	if key != p.key {
		p.offset = 0
	}
	p.key, p.source, p.hasSource, p.notice = key, src, true, ""
	p.render(split)
}

func (p *diffPanel) showNotice(key, notice string) {
	if key != p.key {
		p.offset = 0
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
	pageKey := fmt.Sprintf("%s\x00%d\x00%t", p.key, p.width, split)
	if pageKey == p.pageKey {
		return
	}
	p.page, p.pageKey = diffview.Render(p.source, diffview.Options{Width: p.width, Split: split}), pageKey
	p.scroll(0)
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
