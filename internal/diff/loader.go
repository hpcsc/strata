package diff

import (
	"context"
	"fmt"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

type runner interface {
	Run(ctx context.Context, args ...string) (string, error)
}

// contentLimit keeps syntax highlighting away from files too big to read.
const contentLimit = 1 << 20

type Loader struct {
	git runner
}

func NewLoader(git runner) *Loader {
	return &Loader{git: git}
}

func (l *Loader) Files(ctx context.Context, base, branch string) ([]File, error) {
	out, err := l.git.Run(ctx, "diff", "--no-color", "--no-ext-diff", "--raw", "-z", "--no-abbrev", "-M", base, branch)
	if err != nil {
		return nil, err
	}
	return parseRaw(out)
}

func (l *Loader) Patch(ctx context.Context, base, branch string, f File) (Patch, error) {
	args := append([]string{"diff", "--no-color", "--no-ext-diff", "-M", "-U3", base, branch, "--"}, f.Paths()...)
	out, err := l.git.Run(ctx, args...)
	if err != nil {
		return Patch{}, err
	}
	files, _, err := gitdiff.Parse(strings.NewReader(out))
	if err != nil {
		return Patch{}, fmt.Errorf("parse the diff of %s: %w", f.Path, err)
	}
	patch := Patch{File: f}
	if len(files) == 0 {
		return patch, nil
	}
	parsed := files[0]
	patch.Binary = parsed.IsBinary
	if parsed.OldMode != 0 {
		patch.OldMode = fmt.Sprintf("%o", parsed.OldMode)
	}
	if parsed.NewMode != 0 {
		patch.NewMode = fmt.Sprintf("%o", parsed.NewMode)
	}
	for _, fragment := range parsed.TextFragments {
		patch.Hunks = append(patch.Hunks, hunk(fragment))
	}
	return patch, nil
}

// Contents returns the file's text before and after the change, or empty text
// for a side that has no blob, is binary, or is too big to highlight.
func (l *Loader) Contents(ctx context.Context, f File) (before, after string, err error) {
	if before, err = l.blob(ctx, f.OldBlob); err != nil {
		return "", "", err
	}
	if after, err = l.blob(ctx, f.NewBlob); err != nil {
		return "", "", err
	}
	return before, after, nil
}

func (l *Loader) blob(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", nil
	}
	size, err := l.git.Run(ctx, "cat-file", "-s", id)
	if err != nil {
		return "", err
	}
	var n int
	if _, err := fmt.Sscan(size, &n); err != nil || n > contentLimit {
		return "", nil
	}
	text, err := l.git.Run(ctx, "cat-file", "blob", id)
	if err != nil || strings.ContainsRune(text, 0) {
		return "", err
	}
	return text, nil
}

const noBlob = "0000000000000000000000000000000000000000"

func parseRaw(out string) ([]File, error) {
	fields := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	var files []File
	for i := 0; i < len(fields); {
		meta := strings.Fields(strings.TrimPrefix(fields[i], ":"))
		if len(meta) != 5 {
			return nil, fmt.Errorf("unexpected diff --raw entry %q", fields[i])
		}
		f := File{Status: Status(meta[4][:1]), OldBlob: meta[2], NewBlob: meta[3]}
		if f.OldBlob == noBlob {
			f.OldBlob = ""
		}
		if f.NewBlob == noBlob {
			f.NewBlob = ""
		}
		if f.Status == Renamed || f.Status == Copied {
			if i+2 >= len(fields) {
				return nil, fmt.Errorf("diff --raw entry %q has no paths", fields[i])
			}
			f.OldPath, f.Path = fields[i+1], fields[i+2]
			i += 3
		} else {
			if i+1 >= len(fields) {
				return nil, fmt.Errorf("diff --raw entry %q has no path", fields[i])
			}
			f.Path = fields[i+1]
			i += 2
		}
		files = append(files, f)
	}
	return files, nil
}

func hunk(fragment *gitdiff.TextFragment) Hunk {
	h := Hunk{
		OldStart: int(fragment.OldPosition),
		OldLines: int(fragment.OldLines),
		NewStart: int(fragment.NewPosition),
		NewLines: int(fragment.NewLines),
		Section:  strings.TrimSpace(fragment.Comment),
	}
	oldNumber, newNumber := h.OldStart, h.NewStart
	for _, line := range fragment.Lines {
		l := Line{Text: strings.TrimSuffix(strings.TrimSuffix(line.Line, "\n"), "\r")}
		switch line.Op {
		case gitdiff.OpDelete:
			l.Kind, l.OldNumber = Deletion, oldNumber
			oldNumber++
		case gitdiff.OpAdd:
			l.Kind, l.NewNumber = Addition, newNumber
			newNumber++
		default:
			l.Kind, l.OldNumber, l.NewNumber = Context, oldNumber, newNumber
			oldNumber++
			newNumber++
		}
		h.Lines = append(h.Lines, l)
	}
	return h
}
