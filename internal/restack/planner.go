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
	RunInput(ctx context.Context, stdin string, args ...string) (string, error)
	RunEnv(ctx context.Context, env []string, args ...string) (string, error)
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
	before, ref, err := p.revParseTrunk(ctx, "--symbolic-full-name", p.trunk)
	if err != nil {
		return Plan{}, err
	}
	if remote := remoteOf(ref); remote != "" {
		if err := p.fetch(ctx, remote); err != nil {
			return Plan{}, err
		}
	}

	tree, err := stack.NewReader(p.git, p.trunk, p.patterns).Read(ctx)
	if err != nil {
		return Plan{}, err
	}
	tip, treeOfTip, err := p.revParseTrunk(ctx, p.trunk+"^{tree}")
	if err != nil {
		return Plan{}, err
	}
	newCommits, err := p.newCommits(ctx, before, tip)
	if err != nil {
		return Plan{}, err
	}

	outcomes := map[string]Outcome{}
	for _, b := range tree.Branches {
		o, err := p.outcome(ctx, tree.Trunk, tip, treeOfTip, b, outcomes)
		if err != nil {
			return Plan{}, err
		}
		outcomes[b.Name] = o
	}
	blockStacks(tree, outcomes)
	check, err := newWorktreeCheck(ctx, p.git)
	if err != nil {
		return Plan{}, err
	}
	for _, b := range tree.Branches {
		if outcomes[b.Name], err = check.outcome(ctx, b, outcomes[b.Name]); err != nil {
			return Plan{}, err
		}
	}
	blockStacks(tree, outcomes)
	return Plan{Tree: tree, NewCommits: newCommits, TrunkTip: tip, Outcomes: outcomes}, nil
}

func (p *Planner) revParseTrunk(ctx context.Context, args ...string) (trunk, ofArgs string, err error) {
	out, err := p.git.Run(ctx, append([]string{"rev-parse", p.trunk}, args...)...)
	if err != nil {
		return "", "", err
	}
	parsed := lines(out)
	if len(parsed) != 2 {
		return "", "", fmt.Errorf("find the trunk %s: git rev-parse printed %q", p.trunk, out)
	}
	return parsed[0], parsed[1], nil
}

func (p *Planner) newCommits(ctx context.Context, before, tip string) (int, error) {
	out, err := p.git.Run(ctx, "rev-list", "--count", before+".."+tip, "--")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}

// The outcome of the parent of b must already be in outcomes.
func (p *Planner) outcome(ctx context.Context, trunk, tip, treeOfTip string, b stack.Branch, outcomes map[string]Outcome) (Outcome, error) {
	parent, parentPlanned := outcomes[b.Parent]
	if b.Parent != trunk && !parentPlanned {
		return Outcome{Kind: Loop}, nil
	}
	parentMerged := parent.Kind == Merged
	trunkMovedOn := b.Parent == trunk && b.Behind > 0
	if (trunkMovedOn || parentMerged) && b.Files > 0 {
		has, err := p.trunkHasChanges(ctx, tip, treeOfTip, b.Tip)
		if err != nil {
			return Outcome{}, err
		}
		if has {
			return Outcome{Kind: Merged, NewParent: trunk, Worktree: b.Worktree}, nil
		}
	}

	o := Outcome{Kind: Moves, NewParent: b.Parent, UpstreamGone: b.UpstreamGone}
	onto := parent.NewTip
	if b.Parent == trunk || parentMerged {
		o.NewParent, onto = trunk, tip
	}
	switch {
	case b.Behind == 0 && (b.Parent == trunk || parent.Kind == UpToDate):
		o.Kind, o.NewTip = UpToDate, b.Tip
	case onto == "":
		// the parent stays, so blockStacks names the branch that blocks this one
	case b.HasMergeCommit:
		o.Kind = MergeCommit
	case b.Commits == 0:
		o.NewTip = onto
	default:
		newTip, files, err := p.replay(ctx, onto, b)
		if err != nil {
			return Outcome{}, err
		}
		if newTip == "" {
			o.Kind, o.Files = Conflict, files
		}
		o.NewTip = newTip
	}
	return o, nil
}

func (p *Planner) trunkHasChanges(ctx context.Context, trunk, treeOfTrunk, commit string) (bool, error) {
	out, clean, err := p.git.Try(ctx, "merge-tree", "--write-tree", trunk, commit)
	if err != nil {
		return false, err
	}
	written := lines(out)
	return clean && len(written) > 0 && written[0] == treeOfTrunk, nil
}

func (p *Planner) replay(ctx context.Context, onto string, b stack.Branch) (newTip string, conflict []string, err error) {
	// newer git replay updates the refs itself unless replay.refAction is print
	out, clean, err := p.git.Try(ctx, "-c", "replay.refAction=print", "replay", "--onto", onto, b.Base+".."+b.Ref)
	if err != nil {
		return "", nil, err
	}
	if !clean {
		files, err := p.conflictFiles(ctx, onto, b)
		return "", files, err
	}
	fields := strings.Fields(out)
	if len(fields) != 4 || fields[0] != "update" || fields[1] != b.Ref {
		return "", nil, fmt.Errorf("replay %s: git replay printed %q", b.Name, out)
	}
	if fields[3] != b.Tip {
		return "", nil, fmt.Errorf("%s moved while strata planned the sync", b.Name)
	}
	return fields[2], nil, nil
}

func (p *Planner) conflictFiles(ctx context.Context, onto string, b stack.Branch) ([]string, error) {
	out, _, err := p.git.Try(ctx, "merge-tree", "--write-tree", "--name-only", "--merge-base="+b.Base, onto, b.Tip)
	if err != nil {
		return nil, err
	}
	written := lines(out)
	if len(written) < 2 {
		return nil, nil
	}
	var files []string
	for _, line := range written[1:] {
		if line == "" {
			break
		}
		files = append(files, line)
	}
	return files, nil
}

func blockStacks(tree stack.Tree, outcomes map[string]Outcome) {
	for _, names := range stacksOf(tree) {
		blocker := ""
		for _, name := range names {
			if outcomes[name].Kind.blocksStack() {
				blocker = name
				break
			}
		}
		if blocker == "" {
			continue
		}
		for _, name := range names {
			if o := outcomes[name]; o.Kind == Moves || o.Kind == Merged {
				o.Blocker = blocker
				outcomes[name] = o
			}
		}
	}
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
