package ui

import (
	"context"

	"github.com/hpcsc/strata/internal/diff"
	"github.com/hpcsc/strata/internal/restack"
	"github.com/hpcsc/strata/internal/stack"
	"github.com/hpcsc/strata/internal/syntax"
)

type TreeReader interface {
	Read(ctx context.Context) (stack.Tree, error)
	Commits(ctx context.Context, b stack.Branch) ([]stack.Commit, error)
}

type DiffLoader interface {
	Files(ctx context.Context, base, branch string) ([]diff.File, error)
	Patch(ctx context.Context, base, branch string, f diff.File) (diff.Patch, error)
	Contents(ctx context.Context, f diff.File) (before, after string, err error)
}

type Highlighter interface {
	Lines(path, content string) [][]syntax.Span
}

type ViewedMarks interface {
	Has(f diff.File) bool
	Toggle(f diff.File) error
}

type Syncer interface {
	Available() error
	Plan(ctx context.Context) (restack.Plan, error)
}

type Sources struct {
	Tree        TreeReader
	Diffs       DiffLoader
	Highlighter Highlighter
	Viewed      ViewedMarks
	Sync        Syncer
}
