package ui

import (
	"path"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/hpcsc/strata/internal/diff"
)

type fileRow struct {
	depth int
	// label names the directory on a directory row, where file is -1.
	label string
	file  int
}

type filesPanel struct {
	listed []diff.File
	// files holds the listed files in the order the rows show them.
	files  []diff.File
	rows   []fileRow
	flat   bool
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
	p.listed, p.notice = files, ""
	p.layOut(want)
}

func (p *filesPanel) toggleFlat() {
	want := ""
	if f, ok := p.selected(); ok {
		want = f.Path
	}
	p.flat = !p.flat
	p.layOut(want)
}

func (p *filesPanel) layOut(want string) {
	if p.flat {
		p.files, p.rows = flatLayout(p.listed)
	} else {
		p.files, p.rows = treeLayout(p.listed)
	}
	p.cursor, p.offset = 0, 0
	for i, f := range p.files {
		if f.Path == want {
			p.cursor = i
		}
	}
	p.follow()
}

func (p *filesPanel) showNotice(notice string) {
	p.listed, p.files, p.rows, p.notice, p.cursor, p.offset = nil, nil, nil, notice, 0, 0
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

// follow scrolls so the cursor's row is visible, with the directory rows
// directly above it when they fit.
func (p *filesPanel) follow() {
	row := 0
	for i, r := range p.rows {
		if r.file == p.cursor {
			row = i
		}
	}
	top := row
	for top > 0 && p.rows[top-1].file < 0 {
		top--
	}
	if top < p.offset {
		p.offset = top
	}
	if p.height > 0 && row >= p.offset+p.height {
		p.offset = row - p.height + 1
	}
}

func (p filesPanel) lines(width int, focused bool, viewed ViewedMarks) []string {
	if p.notice != "" {
		return []string{"  " + dimText.Render(p.notice)}
	}
	if len(p.files) == 0 {
		return []string{"  " + dimText.Render("no files changed")}
	}
	const columns = "▌ M ✓ "
	rows := make([]string, 0, len(p.rows))
	for _, r := range p.rows {
		indent := strings.Repeat("  ", r.depth)
		if r.file < 0 {
			rows = append(rows, strings.Repeat(" ", lipgloss.Width(columns))+indent+directoryText.Render(r.label))
			continue
		}
		f := p.files[r.file]
		gutter := "  "
		nameStyle := lipgloss.NewStyle()
		if r.file == p.cursor {
			gutter = dimText.Render("▌") + " "
			if focused {
				gutter = selectedText.Render("▌") + " "
				nameStyle = selectedText
			}
		}
		mark := "  "
		if viewed.Has(f) {
			mark = viewedText.Render("✓") + " "
			if r.file != p.cursor || !focused {
				nameStyle = dimText
			}
		}
		name := indent + nameStyle.Render(path.Base(f.Path))
		if p.flat {
			dir, base := shortenPath(f.Path, width-lipgloss.Width(columns))
			name = dimText.Render(dir) + nameStyle.Render(base)
		}
		rows = append(rows, gutter+statusStyle(f.Status).Render(string(f.Status))+" "+mark+name)
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
