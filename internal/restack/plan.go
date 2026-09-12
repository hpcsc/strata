package restack

import (
	"strings"

	"github.com/hpcsc/strata/internal/stack"
)

type Kind int

const (
	UpToDate Kind = iota
	Moves
	Merged
	Conflict
	MergeCommit
	Loop
	Blocked
)

func (k Kind) keepsStack() bool {
	return k == Conflict || k == MergeCommit || k == Loop
}

type Outcome struct {
	Kind      Kind
	NewParent string
	// NewTip is empty for a branch that stays or that the sync deletes.
	NewTip       string
	Worktree     string
	UpstreamGone bool
	Files        []string
	Blocker      string
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
	case Conflict:
		if len(o.Files) == 0 {
			return "stays: conflict"
		}
		return "stays: conflict in " + strings.Join(o.Files, ", ")
	case MergeCommit:
		return "stays: merge commit"
	case Loop:
		return "stays: its parents form a loop"
	case Blocked:
		return "stays: " + o.Blocker + " cannot move"
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
	TrunkTip   string
	Outcomes   map[string]Outcome
}

func (p Plan) StacksWithConflict() int {
	n := 0
	for _, names := range stacksOf(p.Tree) {
		for _, name := range names {
			if p.Outcomes[name].Kind == Conflict {
				n++
				break
			}
		}
	}
	return n
}

func stacksOf(tree stack.Tree) [][]string {
	var stacks [][]string
	for _, b := range tree.Branches {
		if b.Level == 0 || len(stacks) == 0 {
			stacks = append(stacks, nil)
		}
		stacks[len(stacks)-1] = append(stacks[len(stacks)-1], b.Name)
	}
	return stacks
}
