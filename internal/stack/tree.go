package stack

import "strings"

type Branch struct {
	Name string
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
	Behind       int
	Worktree     string
	UpstreamGone bool
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

type Commit struct {
	Hash    string
	Subject string
	Age     string
}
