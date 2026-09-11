package ui

import (
	"path"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/hpcsc/strata/internal/diff"
)

type filesPanel struct {
	files  []diff.File
	notice string
	cursor int
	offset int
	height int
}

func (p filesPanel) selected() (diff.File, bool) {
	if p.cursor < 0 || p.cursor >= len(p.files) {
		return diff.File{}, false
	}
	return p.files[p.cursor], true
}

// setFiles shows files with the cursor on want, or on the first file when
// want is not among them.
func (p *filesPanel) setFiles(files []diff.File, want string) {
	p.files, p.notice, p.cursor, p.offset = files, "", 0, 0
	for i, f := range files {
		if f.Path == want {
			p.cursor = i
		}
	}
	p.follow()
}

func (p *filesPanel) showNotice(notice string) {
	p.files, p.notice, p.cursor, p.offset = nil, notice, 0, 0
}

func (p *filesPanel) moveTo(i int) bool {
	i = max(0, min(i, len(p.files)-1))
	if i == p.cursor || len(p.files) == 0 {
		return false
	}
	p.cursor = i
	p.follow()
	return true
}

func (p *filesPanel) setHeight(height int) {
	p.height = height
	p.follow()
}

func (p *filesPanel) follow() {
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.height > 0 && p.cursor >= p.offset+p.height {
		p.offset = p.cursor - p.height + 1
	}
}

func (p filesPanel) lines(width int, focused bool, viewed ViewedMarks) []string {
	if p.notice != "" {
		return []string{"  " + dimText.Render(p.notice)}
	}
	if len(p.files) == 0 {
		return []string{"  " + dimText.Render("no files changed")}
	}
	rows := make([]string, 0, len(p.files))
	for i, f := range p.files {
		gutter := "  "
		nameStyle := lipgloss.NewStyle()
		if i == p.cursor {
			gutter = dimText.Render("▌") + " "
			if focused {
				gutter = selectedText.Render("▌") + " "
				nameStyle = selectedText
			}
		}
		mark := "  "
		dir, base := shortenPath(f.Path, width-lipgloss.Width("▌ M ✓ "))
		if viewed.Has(f) {
			mark = viewedText.Render("✓") + " "
			if i != p.cursor || !focused {
				nameStyle = dimText
			}
		}
		rows = append(rows, gutter+statusStyle(f.Status).Render(string(f.Status))+" "+mark+dimText.Render(dir)+nameStyle.Render(base))
	}
	return window(rows, p.offset, p.height)
}

// shortenPath fits a path into width cells. It drops whole directories from
// the left first, so the file name stays visible.
func shortenPath(p string, width int) (dir, base string) {
	dir, base = path.Split(p)
	if lipgloss.Width(dir+base) <= width {
		return dir, base
	}
	segments := strings.Split(strings.TrimSuffix(dir, "/"), "/")
	for len(segments) > 0 {
		segments = segments[1:]
		shortened := "…/" + strings.Join(append(segments, ""), "/")
		if len(segments) == 0 {
			shortened = "…/"
		}
		if lipgloss.Width(shortened+base) <= width {
			return shortened, base
		}
	}
	return "", truncate(base, max(1, width))
}

func statusStyle(s diff.Status) lipgloss.Style {
	switch s {
	case diff.Added:
		return addedText
	case diff.Deleted:
		return deletedText
	case diff.Renamed, diff.Copied:
		return renamedText
	default:
		return modifiedText
	}
}
