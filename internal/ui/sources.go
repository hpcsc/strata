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
	Refs(ctx context.Context) (string, error)
	Commits(ctx context.Context, b stack.Branch) ([]stack.Commit, error)
}

type DiffLoader interface {
	Files(ctx context.Context, base, branch string) ([]diff.File, error)
	Patch(ctx context.Context, base, branch string, f diff.File) (diff.Patch, error)
	Contents(ctx context.Context, f diff.File) (before, after string, err error)
}

type Highlighter interface {
	Lines(path, content string) [][]syntax.Span
	Theme() syntax.Theme
}

type ViewedMarks interface {
	Has(f diff.File) bool
	Toggle(f diff.File) error
}

type Syncer interface {
	Available() error
	Pending(ctx context.Context) (restack.Pending, error)
	Plan(ctx context.Context, progress restack.Progress) (restack.Plan, error)
	Replan(ctx context.Context, progress restack.Progress) (restack.Plan, error)
	Move(ctx context.Context, plan restack.Plan) (restack.Result, error)
	Resolve(ctx context.Context, plan restack.Plan, branch string) (restack.Pending, error)
	Finish(ctx context.Context) (restack.Result, error)
}

type Deleter interface {
	Check(ctx context.Context, branches []stack.Branch) ([]stack.Deletion, error)
	Delete(ctx context.Context, deletions []stack.Deletion) error
}

type Sources struct {
	Tree        TreeReader
	Diffs       DiffLoader
	Highlighter Highlighter
	Viewed      ViewedMarks
	Sync        Syncer
	Deleter     Deleter
}
