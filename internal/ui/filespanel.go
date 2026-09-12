package ui

import (
	"fmt"
	"path"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/hpcsc/strata/internal/diff"
	"github.com/hpcsc/strata/internal/search"
)

type fileRow struct {
	depth int
	// A folder row has file -1. Its folder is the full path, such as
	// "common/modules/rules/", and its label is the part the row shows.
	label  string
	folder string
	file   int
}

type filesPanel struct {
	viewed ViewedMarks
	listed []diff.File
	// files holds the listed files in the order the rows show them.
	files []diff.File
	all   []fileRow
	// rows holds the rows that show: all of them less the rows that a folded
	// folder or the filter hides.
	rows []fileRow
	// folded holds the folders that o folded or unfolded, by path. Any other
	// folder folds when every file in it is viewed.
	folded map[string]bool
	// filter hides the files whose paths do not match it, and the folders
	// with no such file. While it is set, no folder is folded.
	filter search.Query
	flat   bool
	notice string
	cursor int
	offset int
	height int
}

func newFilesPanel(viewed ViewedMarks) filesPanel {
	return filesPanel{viewed: viewed, folded: map[string]bool{}}
}

func (p filesPanel) selected() (diff.File, bool) {
	if p.cursor < 0 || p.cursor >= len(p.rows) || p.rows[p.cursor].file < 0 {
		return diff.File{}, false
	}
	return p.files[p.rows[p.cursor].file], true
}

func (p filesPanel) selectedFolder() (fileRow, bool) {
	if p.cursor < 0 || p.cursor >= len(p.rows) || p.rows[p.cursor].file >= 0 {
		return fileRow{}, false
	}
	return p.rows[p.cursor], true
}

// position returns the place of the selected file among all the files, from
// 1, and the number of files.
func (p filesPanel) position() (int, int) {
	if p.cursor < 0 || p.cursor >= len(p.rows) {
		return 0, len(p.files)
	}
	return p.rows[p.cursor].file + 1, len(p.files)
}

func (p filesPanel) filesIn(folder string) []diff.File {
	var out []diff.File
	for _, f := range p.files {
		if strings.HasPrefix(f.Path, folder) {
			out = append(out, f)
		}
	}
	return out
}

// setFiles shows files with the cursor on want. It unfolds the folders that
// hide want, and falls back to the first file when want is not among them.
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
		p.files, p.all = flatLayout(p.listed)
	} else {
		p.files, p.all = treeLayout(p.listed)
	}
	p.cursor, p.offset = 0, 0
	p.showRows()
	for i, f := range p.files {
		if f.Path == want {
			p.reveal(i)
			if row := p.rowOf(i); row >= 0 {
				p.cursor = row
				p.follow()
				return
			}
		}
	}
	p.toFirstFile()
}

func (p *filesPanel) toFirstFile() {
	p.cursor = 0
	for i, r := range p.rows {
		if r.file >= 0 {
			p.cursor = i
			break
		}
	}
	p.follow()
}

// setFilter shows only the files that match filter. It reports whether the
// selected file changed, which happens when the filter hides it.
func (p *filesPanel) setFilter(filter search.Query) bool {
	before, had := p.selected()
	p.filter = filter
	p.showRows()
	for i, f := range p.files {
		if had && f.Path == before.Path {
			if row := p.rowOf(i); row >= 0 {
				p.cursor = row
				p.follow()
				return false
			}
		}
	}
	p.toFirstFile()
	after, has := p.selected()
	return has != had || after.Path != before.Path
}

func (p filesPanel) matching() int {
	n := 0
	for _, f := range p.files {
		if p.filter.Matches(f.Path) {
			n++
		}
	}
	return n
}

func (p *filesPanel) showNotice(notice string) {
	p.listed, p.files, p.all, p.rows, p.notice, p.cursor, p.offset = nil, nil, nil, nil, notice, 0, 0
}

func (p filesPanel) isFolded(folder string) bool {
	if p.flat || !p.filter.Empty() {
		return false
	}
	if folded, ok := p.folded[folder]; ok {
		return folded
	}
	return p.allViewed(folder)
}

func (p filesPanel) allViewed(folder string) bool {
	files := p.filesIn(folder)
	for _, f := range files {
		if !p.viewed.Has(f) {
			return false
		}
	}
	return len(files) > 0
}

// refresh shows the rows again after a file becomes viewed or not viewed. The
// cursor stays on its row, or moves to the folder that now hides it.
func (p *filesPanel) refresh() {
	if p.cursor < 0 || p.cursor >= len(p.rows) {
		p.showRows()
		return
	}
	kept := p.rows[p.cursor]
	p.showRows()
	p.cursor = p.rowFor(kept)
	p.follow()
}

func (p *filesPanel) showRows() {
	p.rows = nil
	hiddenBelow := -1
	for _, r := range p.all {
		if hiddenBelow >= 0 && r.depth > hiddenBelow {
			continue
		}
		hiddenBelow = -1
		if !p.passesFilter(r) {
			continue
		}
		p.rows = append(p.rows, r)
		if r.file < 0 && p.isFolded(r.folder) {
			hiddenBelow = r.depth
		}
	}
}

func (p filesPanel) passesFilter(r fileRow) bool {
	if p.filter.Empty() {
		return true
	}
	if r.file >= 0 {
		return p.filter.Matches(p.files[r.file].Path)
	}
	for _, f := range p.filesIn(r.folder) {
		if p.filter.Matches(f.Path) {
			return true
		}
	}
	return false
}

// rowFor finds the row that shows target, or the deepest folder row that
// hides it.
func (p filesPanel) rowFor(target fileRow) int {
	name := target.folder
	if target.file >= 0 {
		name = p.files[target.file].Path
	}
	best := 0
	for i, r := range p.rows {
		if r.file == target.file && r.folder == target.folder {
			return i
		}
		if r.file < 0 && r.folder != name && strings.HasPrefix(name, r.folder) {
			best = i
		}
	}
	return best
}

// rowOf returns the row that shows file, or -1 when file does not show.
func (p filesPanel) rowOf(file int) int {
	for i, r := range p.rows {
		if r.file == file {
			return i
		}
	}
	return -1
}

func (p *filesPanel) reveal(file int) {
	changed := false
	for _, r := range p.all {
		if r.file < 0 && strings.HasPrefix(p.files[file].Path, r.folder) && p.isFolded(r.folder) {
			p.folded[r.folder] = false
			changed = true
		}
	}
	if changed {
		p.showRows()
	}
}

// toggleFolder folds or unfolds the selected folder. On a file, it folds the
// folder that holds the file and moves the cursor onto that folder.
func (p *filesPanel) toggleFolder() bool {
	if r, ok := p.selectedFolder(); ok {
		p.folded[r.folder] = !p.isFolded(r.folder)
		p.refresh()
		return true
	}
	f, ok := p.selected()
	if !ok {
		return false
	}
	holder := ""
	for _, r := range p.all {
		if r.file < 0 && strings.HasPrefix(f.Path, r.folder) && len(r.folder) > len(holder) {
			holder = r.folder
		}
	}
	if holder == "" {
		return false
	}
	p.folded[holder] = true
	p.refresh()
	return true
}

func (p *filesPanel) moveTo(i int) bool {
	i = max(0, min(i, len(p.rows)-1))
	if i == p.cursor || len(p.rows) == 0 {
		return false
	}
	p.cursor = i
	p.follow()
	return true
}

// moveToFile moves to the next file that shows, or the previous one when
// step is negative, and passes over folder rows.
func (p *filesPanel) moveToFile(step int) bool {
	for i := p.cursor + step; i >= 0 && i < len(p.rows); i += step {
		if p.rows[i].file >= 0 {
			return p.moveTo(i)
		}
	}
	return false
}

// nextFile returns the first file that shows after the cursor.
func (p filesPanel) nextFile() (diff.File, bool) {
	for i := p.cursor + 1; i < len(p.rows); i++ {
		if p.rows[i].file >= 0 {
			return p.files[p.rows[i].file], true
		}
	}
	return diff.File{}, false
}

// passViewed moves on from file, which the reader just marked viewed. A
// folder that is now fully viewed folds, and the cursor goes to the first file
// that shows after it.
func (p *filesPanel) passViewed(file int) {
	p.refresh()
	for i, r := range p.rows {
		if r.file > file {
			p.cursor = i
			p.follow()
			return
		}
	}
}

func (p *filesPanel) setHeight(height int) {
	p.height = height
	p.follow()
}

// follow scrolls so the cursor's row is visible, with the folder rows
// directly above it when they fit.
func (p *filesPanel) follow() {
	row := max(0, p.cursor)
	top := row
	for top > 0 && top <= len(p.rows) && p.rows[top-1].file < 0 {
		top--
	}
	if top < p.offset {
		p.offset = top
	}
	if p.height > 0 && row >= p.offset+p.height {
		p.offset = row - p.height + 1
	}
}

func (p filesPanel) lines(width int, focused bool) []string {
	if p.notice != "" {
		return []string{"  " + dimText.Render(p.notice)}
	}
	if len(p.files) == 0 {
		return []string{"  " + dimText.Render("no files changed")}
	}
	const columns = "▌ M ✓ "
	rows := make([]string, 0, len(p.rows))
	for i, r := range p.rows {
		gutter := "  "
		nameStyle := lipgloss.NewStyle()
		if i == p.cursor {
			gutter = dimText.Render("▌") + " "
			if focused {
				gutter = selectedText.Render("▌") + " "
				nameStyle = selectedText
			}
		}
		indent := strings.Repeat("  ", r.depth)
		if r.file < 0 {
			rows = append(rows, gutter+p.folderRow(r, indent, nameStyle))
			continue
		}
		f := p.files[r.file]
		mark := "  "
		if p.viewed.Has(f) {
			mark = viewedText.Render("✓") + " "
			if i != p.cursor || !focused {
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

func (p filesPanel) folderRow(r fileRow, indent string, nameStyle lipgloss.Style) string {
	mark := "  "
	if p.allViewed(r.folder) {
		mark = viewedText.Render("✓") + " "
	}
	if !p.isFolded(r.folder) {
		return "  " + mark + indent + directoryText.Render("▾ ") + nameStyle.Inherit(directoryText).Render(r.label)
	}
	return "  " + mark + indent + directoryText.Render("▸ ") + nameStyle.Inherit(directoryText).Render(r.label) +
		dimText.Render(fmt.Sprintf("  %s", plural(len(p.filesIn(r.folder)), "file")))
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
