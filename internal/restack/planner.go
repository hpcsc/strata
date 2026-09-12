package restack

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hpcsc/strata/internal/stack"
)

type runner interface {
	Run(ctx context.Context, args ...string) (string, error)
	Check(ctx context.Context, args ...string) (bool, error)
}

type Fetch func(ctx context.Context, remote string) error

type Planner struct {
	git      runner
	fetch    Fetch
	trunk    string
	patterns []string
}

func NewPlanner(git runner, fetch Fetch, trunk string, patterns []string) *Planner {
	return &Planner{git: git, fetch: fetch, trunk: trunk, patterns: patterns}
}

func (p *Planner) Plan(ctx context.Context) (Plan, error) {
	out, err := p.git.Run(ctx, "rev-parse", p.trunk, "--symbolic-full-name", p.trunk)
	if err != nil {
		return Plan{}, err
	}
	resolved := lines(out)
	if len(resolved) != 2 {
		return Plan{}, fmt.Errorf("find the trunk %s: git rev-parse printed %q", p.trunk, out)
	}
	before := resolved[0]
	if remote := remoteOf(resolved[1]); remote != "" {
		if err := p.fetch(ctx, remote); err != nil {
			return Plan{}, err
		}
	}

	tree, err := stack.NewReader(p.git, p.trunk, p.patterns).Read(ctx)
	if err != nil {
		return Plan{}, err
	}
	newCommits, err := p.newCommits(ctx, before)
	if err != nil {
		return Plan{}, err
	}

	outcomes := map[string]Outcome{}
	for _, b := range tree.Branches {
		if b.Behind > 0 || outcomes[b.Parent].Kind == Moves {
			outcomes[b.Name] = Outcome{Kind: Moves, NewParent: b.Parent}
		} else {
			outcomes[b.Name] = Outcome{Kind: UpToDate, NewParent: b.Parent}
		}
	}
	return Plan{Tree: tree, NewCommits: newCommits, Outcomes: outcomes}, nil
}

func (p *Planner) newCommits(ctx context.Context, before string) (int, error) {
	out, err := p.git.Run(ctx, "rev-list", "--count", before+".."+p.trunk, "--")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}

func remoteOf(ref string) string {
	rest, ok := strings.CutPrefix(ref, "refs/remotes/")
	if !ok {
		return ""
	}
	remote, _, _ := strings.Cut(rest, "/")
	return remote
}

func lines(out string) []string {
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}
