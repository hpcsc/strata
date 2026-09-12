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
	Rebasing
	WorktreeGone
	Changes
	Stale
)

func (k Kind) blocksStack() bool {
	switch k {
	case Conflict, MergeCommit, Loop, Rebasing, WorktreeGone, Changes, Stale:
		return true
	}
	return false
}

type Outcome struct {
	Kind         Kind
	NewParent    string
	NewTip       string
	Worktree     string
	UpstreamGone bool
	Files        []string
	// Blocker names the branch that makes the stack of this branch stay. Kind
	// then tells what the sync does to this branch once that branch can move.
	Blocker string
	Keep    bool
}

func (o Outcome) Text() string {
	if o.Blocker != "" {
		return "stays: " + o.Blocker + " cannot move"
	}
	var text string
	switch o.Kind {
	case Moves:
		text = "moves onto " + o.NewParent
		if o.Worktree != "" {
			text += ", checked out in " + o.Worktree
		}
	case Merged:
		if o.Worktree != "" {
			return "merged, checked out in " + o.Worktree
		}
		if o.Keep {
			return "merged: strata keeps it"
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
	case Rebasing:
		return "stays: a rebase in " + o.Worktree + " uses it"
	case WorktreeGone:
		return "stays: its worktree " + o.Worktree + " is not there; run git worktree prune if you deleted it"
	case Changes:
		return "stays: changes in " + o.Worktree + ": " + strings.Join(o.Files, ", ")
	case Stale:
		return "stays: it changed in " + o.Worktree
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

func (p Plan) KeepMerged() Plan {
	outcomes := make(map[string]Outcome, len(p.Outcomes))
	for name, o := range p.Outcomes {
		o.Keep = o.Kind == Merged
		outcomes[name] = o
	}
	p.Outcomes = outcomes
	return p
}

func (p Plan) withNewTips(newTips map[string]string) Plan {
	outcomes := make(map[string]Outcome, len(p.Outcomes))
	for name, o := range p.Outcomes {
		if tip, ok := newTips[name]; ok && o.Kind == Moves {
			o.NewTip = tip
		}
		outcomes[name] = o
	}
	p.Outcomes = outcomes
	return p
}

func (p Plan) StackHasConflict(branch string) bool {
	for _, name := range stackOf(p, branch) {
		if p.Outcomes[name].Kind == Conflict {
			return true
		}
	}
	return false
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

func (p Plan) StacksThatMove() int {
	n := 0
	for _, names := range stacksOf(p.Tree) {
		if p.stackMoves(names) {
			n++
		}
	}
	return n
}

func (p Plan) stackMoves(names []string) bool {
	changes := false
	for _, name := range names {
		o := p.Outcomes[name]
		if o.Blocker != "" {
			return false
		}
		switch o.Kind {
		case UpToDate:
		case Moves:
			changes = true
		case Merged:
			changes = changes || !o.Keep && o.Worktree == ""
		default:
			return false
		}
	}
	return changes
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
