package stack

import (
	"fmt"
	"strings"
)

type Branch struct {
	Name string
	Ref  string
	Tip  string
	// Parent is the branch this one sits on, or the trunk.
	Parent string
	// Base is the commit where this branch's own changes start.
	Base       string
	Level      int
	Commits    int
	Files      int
	Insertions int
	Deletions  int
	// Behind counts the commits on Parent that this branch does not have.
	Behind         int
	Worktree       string
	RebaseWorktree string
	UpstreamGone   bool
	HasMergeCommit bool
}

// Tree lists branches depth-first: every parent comes before its children.
type Tree struct {
	Trunk    string
	Current  string
	Branches []Branch
}

// Connectors returns the tree lines to draw before each branch's name, such
// as "│  └─ ", in the order of Branches.
func (t Tree) Connectors() []string {
	last := make([]bool, len(t.Branches))
	for i, b := range t.Branches {
		last[i] = true
		for _, later := range t.Branches[i+1:] {
			if later.Level < b.Level {
				break
			}
			if later.Level == b.Level {
				last[i] = false
				break
			}
		}
	}
	out := make([]string, len(t.Branches))
	var continues []bool
	for i, b := range t.Branches {
		continues = continues[:min(b.Level, len(continues))]
		var line strings.Builder
		for _, c := range continues {
			if c {
				line.WriteString("│  ")
			} else {
				line.WriteString("   ")
			}
		}
		if last[i] {
			line.WriteString("└─ ")
		} else {
			line.WriteString("├─ ")
		}
		out[i] = line.String()
		continues = append(continues, !last[i])
	}
	return out
}

func (t Tree) Index(name string) int {
	for i, b := range t.Branches {
		if b.Name == name {
			return i
		}
	}
	return -1
}

func (t Tree) CheckDelete(names []string) error {
	deleting := map[string]bool{}
	for _, name := range names {
		deleting[name] = true
	}
	for _, b := range t.Branches {
		switch {
		case deleting[b.Name] && b.Worktree != "":
			return checkedOutError(b.Name, b.Worktree)
		// strata finds parents from the commits, so a child that stays then
		// shows the commits of its deleted parent as its own
		case deleting[b.Parent] && !deleting[b.Name]:
			return fmt.Errorf("%s sits on %s: mark %s too", b.Name, b.Parent, b.Name)
		}
	}
	return nil
}

func checkedOutError(branch, worktree string) error {
	return fmt.Errorf("%s is checked out in %s: switch that worktree to another branch first", branch, worktree)
}

type Commit struct {
	Hash    string
	Subject string
	Age     string
}
