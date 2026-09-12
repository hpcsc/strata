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
	Try(ctx context.Context, args ...string) (string, bool, error)
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
	out, err = p.git.Run(ctx, "rev-parse", p.trunk, p.trunk+"^{tree}")
	if err != nil {
		return Plan{}, err
	}
	trunk := lines(out)
	if len(trunk) != 2 {
		return Plan{}, fmt.Errorf("find the trunk %s: git rev-parse printed %q", p.trunk, out)
	}
	tip, treeOfTip := trunk[0], trunk[1]
	newCommits, err := p.newCommits(ctx, before, tip)
	if err != nil {
		return Plan{}, err
	}

	outcomes := map[string]Outcome{}
	merged := map[string]bool{}
	for _, b := range tree.Branches {
		trunkMovedOn := b.Parent == tree.Trunk && b.Behind > 0
		if (trunkMovedOn || merged[b.Parent]) && b.Files > 0 {
			has, err := p.trunkHasChanges(ctx, tip, treeOfTip, b.Tip)
			if err != nil {
				return Plan{}, err
			}
			if has {
				merged[b.Name] = true
				outcomes[b.Name] = Outcome{Kind: Merged, NewParent: tree.Trunk, Worktree: b.Worktree}
				continue
			}
		}
		o := Outcome{Kind: UpToDate, NewParent: b.Parent, UpstreamGone: b.UpstreamGone}
		if merged[b.Parent] {
			o.NewParent = tree.Trunk
		}
		if b.Behind > 0 || merged[b.Parent] || outcomes[b.Parent].Kind == Moves {
			o.Kind = Moves
		}
		outcomes[b.Name] = o
	}
	return Plan{Tree: tree, NewCommits: newCommits, Outcomes: outcomes}, nil
}

func (p *Planner) newCommits(ctx context.Context, before, tip string) (int, error) {
	out, err := p.git.Run(ctx, "rev-list", "--count", before+".."+tip, "--")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}

func (p *Planner) trunkHasChanges(ctx context.Context, trunk, treeOfTrunk, commit string) (bool, error) {
	out, clean, err := p.git.Try(ctx, "merge-tree", "--write-tree", trunk, commit)
	if err != nil {
		return false, err
	}
	written := lines(out)
	return clean && len(written) > 0 && written[0] == treeOfTrunk, nil
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
