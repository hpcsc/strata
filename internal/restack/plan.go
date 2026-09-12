package restack

import "github.com/hpcsc/strata/internal/stack"

type Kind int

const (
	UpToDate Kind = iota
	Moves
	Merged
)

type Outcome struct {
	Kind         Kind
	NewParent    string
	Worktree     string
	UpstreamGone bool
}

func (o Outcome) Text() string {
	var text string
	switch o.Kind {
	case Moves:
		text = "moves onto " + o.NewParent
	case Merged:
		if o.Worktree != "" {
			return "merged, checked out in " + o.Worktree
		}
		return "merged: strata deletes it"
	default:
		text = "up to date"
	}
	if o.UpstreamGone {
		text += "; remote branch gone, not merged"
	}
	return text
}

type Plan struct {
	Tree       stack.Tree
	NewCommits int
	Outcomes   map[string]Outcome
}
