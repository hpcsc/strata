package restack

import "github.com/hpcsc/strata/internal/stack"

type Kind int

const (
	UpToDate Kind = iota
	Moves
)

type Outcome struct {
	Kind      Kind
	NewParent string
}

func (o Outcome) Text() string {
	switch o.Kind {
	case Moves:
		return "moves onto " + o.NewParent
	default:
		return "up to date"
	}
}

type Plan struct {
	Tree       stack.Tree
	NewCommits int
	Outcomes   map[string]Outcome
}
