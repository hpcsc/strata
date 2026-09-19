package diffview

import (
	"github.com/hpcsc/strata/internal/diff"
	"github.com/hpcsc/strata/internal/syntax"
)

type block struct {
	lines []diff.Line
	// hunk is nil for the lines between two hunks.
	hunk *diff.Hunk
}

func hunkBlocks(p diff.Patch) []block {
	blocks := make([]block, 0, len(p.Hunks))
	for i := range p.Hunks {
		blocks = append(blocks, block{lines: p.Hunks[i].Lines, hunk: &p.Hunks[i]})
	}
	return blocks
}

// wholeBlocks adds the lines that the patch leaves out. It returns false when
// the text after the change does not reach the last hunk.
func wholeBlocks(src Source) ([]block, bool) {
	end := 0
	for _, h := range src.Patch.Hunks {
		end = max(end, h.NewStart+h.NewLines-1)
	}
	if len(src.After) < end {
		return nil, false
	}
	var blocks []block
	oldNumber, newNumber := 1, 1
	for i := range src.Patch.Hunks {
		h := &src.Patch.Hunks[i]
		if gap := unchanged(src.After, oldNumber, newNumber, h.NewStart-newNumber); len(gap) > 0 {
			blocks = append(blocks, block{lines: gap})
		}
		blocks = append(blocks, block{lines: h.Lines, hunk: h})
		oldNumber = max(oldNumber, h.OldStart+h.OldLines)
		newNumber = max(newNumber, h.NewStart+h.NewLines)
	}
	if tail := unchanged(src.After, oldNumber, newNumber, len(src.After)+1-newNumber); len(tail) > 0 {
		blocks = append(blocks, block{lines: tail})
	}
	return blocks, true
}

func unchanged(after [][]syntax.Span, oldNumber, newNumber, count int) []diff.Line {
	var lines []diff.Line
	for i := range count {
		lines = append(lines, diff.Line{
			Kind:      diff.Context,
			Text:      joined(after[newNumber+i-1]),
			OldNumber: oldNumber + i,
			NewNumber: newNumber + i,
		})
	}
	return lines
}
